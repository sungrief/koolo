
package action

import (
    "fmt"
    "log/slog"
    "math/rand"
    "reflect"
    "sort"

    "github.com/hectorgimenez/d2go/pkg/data"
    "github.com/hectorgimenez/d2go/pkg/data/area"
    "github.com/hectorgimenez/d2go/pkg/data/item"
    "github.com/hectorgimenez/d2go/pkg/data/npc"
    "github.com/hectorgimenez/d2go/pkg/data/object"
    "github.com/hectorgimenez/d2go/pkg/nip"
    "github.com/hectorgimenez/koolo/internal/action/step"
    "github.com/hectorgimenez/koolo/internal/config"
    "github.com/hectorgimenez/koolo/internal/context"
    "github.com/hectorgimenez/koolo/internal/game"
    "github.com/hectorgimenez/koolo/internal/ui"
    "github.com/hectorgimenez/koolo/internal/utils"
)

// Optional: external auto-equip predicate to allow purchases that tiers want
var AutoEquipHook func(data.Item) bool

// Action-level plan (kept separate from config structs)
type ActionShoppingPlan struct {
    Enabled         bool
    RefreshesPerRun int
    MinGoldReserve  int
    Vendors         []npc.ID
    Rules           nip.Rules // optional override; if empty we use Runtime.Rules
    Types           []string  // optional allow-list of item types (string of item.Desc().Type)
}

// Adapter from config.ShoppingConfig using reflection so we don't depend on exact field names
func NewActionShoppingPlanFromConfig(cfg config.ShoppingConfig) ActionShoppingPlan {
    plan := ActionShoppingPlan{
        Enabled:         cfg.Enabled,
        RefreshesPerRun: cfg.RefreshesPerRun,
        MinGoldReserve:  cfg.MinGoldReserve,
        Vendors:         vendorListFromConfig(cfg),
        Rules:           nil, // prefer runtime rules
        Types:           nil,
    }
    return plan
}

// Reflect vendor list from a variety of possible shapes
func vendorListFromConfig(cfg config.ShoppingConfig) []npc.ID {
    out := make([]npc.ID, 0, 10)
    rv := reflect.ValueOf(cfg)

    // 1) Method GetVendorList() []npc.ID
    if m := rv.MethodByName("GetVendorList"); m.IsValid() {
        if m.Type().NumIn() == 0 && m.Type().NumOut() == 1 {
            res := m.Call(nil)
            if len(res) == 1 {
                if v, ok := res[0].Interface().([]npc.ID); ok {
                    return v
                }
            }
        }
    }

    // 2) Field VendorsToShop []npc.ID
    if f := rv.FieldByName("VendorsToShop"); f.IsValid() {
        if slice, ok := f.Interface().([]npc.ID); ok && len(slice) > 0 {
            return slice
        }
    }

    // 3) Individual bool flags (best-effort; ignore if missing)
    addIfTrue := func(field string, id npc.ID) {
        if f := rv.FieldByName(field); f.IsValid() && f.Kind() == reflect.Bool && f.Bool() {
            out = append(out, id)
        }
    }
    addIfTrue("VendorAkara", npc.Akara)
    addIfTrue("VendorCharsi", npc.Charsi)
    addIfTrue("VendorGheed", npc.Gheed)
    addIfTrue("VendorFara", npc.Fara)
    addIfTrue("VendorDrognan", npc.Drognan)
    addIfTrue("VendorElzix", npc.Elzix)
    addIfTrue("VendorOrmus", npc.Ormus)
    addIfTrue("VendorMalah", npc.Malah)
    // Anya uses Drehya ID
    addIfTrue("VendorAnya", npc.Drehya)

    return out
}

// Back-compat entrypoint
func RunShoppingFromConfig(cfg *config.ShoppingConfig) error {
    if cfg == nil {
        return fmt.Errorf("nil shopping config")
    }
    return RunShopping(NewActionShoppingPlanFromConfig(*cfg))
}

func RunShopping(plan ActionShoppingPlan) error {
    ctx := context.Get()
    if !plan.Enabled {
        ctx.Logger.Debug("Shopping disabled")
        return nil
    }
    if len(plan.Vendors) == 0 {
        ctx.Logger.Warn("No vendors selected for shopping")
        return nil
    }

    // To avoid fit complexity across codebase variations, we enforce stashing once up-front.
    if !ensureTwoFreeColumns() {
        ctx.Logger.Warn("Not enough adjacent space (two columns) even after stashing; aborting shopping")
        return nil
    }

    townOrder, vendorsByTown := groupVendorsByTown(plan.Vendors)

    for _, townID := range townOrder {
        vendors := vendorsByTown[townID]
        if len(vendors) == 0 {
            continue
        }

        if err := ensureInTown(townID); err != nil {
            ctx.Logger.Warn("Skipping town; cannot reach", slog.String("town", townID.Area().Name), slog.Any("err", err))
            continue
        }
        utils.Sleep(60)
        ctx.RefreshGameData()

        if !ensureTwoFreeColumns() {
            ctx.Logger.Warn("Not enough adjacent space after stashing; skipping town batch", slog.String("town", townID.Area().Name))
            continue
        }

        passes := plan.RefreshesPerRun
        if passes < 0 {
            passes = 0
        }

        for pass := 0; pass <= passes; pass++ {
            ctx.Logger.Info("Shopping pass", slog.String("town", townID.Area().Name), slog.Int("pass", pass))

            for _, v := range vendors {
                if !ensureTwoFreeColumns() {
                    ctx.Logger.Warn("Skipping vendor due to inventory space (need two free columns)", slog.Int("vendor", int(v)))
                    break
                }

                _, _, _ = shopVendorSinglePass(v, plan)

                step.CloseAllMenus()
                utils.Sleep(30)
                ctx.RefreshGameData()
            }

            if pass < passes {
                onlyAnya := len(vendors) == 1 && vendors[0] == npc.Drehya
                if err := refreshTownPreferAnyaPortal(townID, onlyAnya); err != nil {
                    ctx.Logger.Warn("Town refresh failed; stopping further passes", slog.String("town", townID.Area().Name), slog.Any("err", err))
                    break
                }
            }
        }
    }
    return nil
}

func shopVendorSinglePass(vendorID npc.ID, plan ActionShoppingPlan) (goldSpent int, itemsBought int, err error) {
    ctx := context.Get()

    // Movement
    _ = moveToVendor(vendorID)
    utils.Sleep(40)
    ctx.RefreshGameData()

    if err = InteractNPC(vendorID); err != nil {
        return 0, 0, fmt.Errorf("interact with vendor %d: %w", int(vendorID), err)
    }

    openVendorTrade(vendorID)
    if !ctx.Data.OpenMenus.NPCShop {
        return 0, 0, fmt.Errorf("vendor trade window did not open for ID=%d", int(vendorID))
    }

    itemsBought, goldSpent = scanAndPurchaseItems(vendorID, plan)
    return goldSpent, itemsBought, nil
}

// --- Minimal "two free columns" helper: stash and assume success ---

func ensureTwoFreeColumns() bool {
    ctx := context.Get()
    step.CloseAllMenus()
    utils.Sleep(50)
    ctx.RefreshGameData()
    if err := Stash(false); err != nil {
        ctx.Logger.Warn("Stash failed while ensuring two free columns", slog.Any("err", err))
        return false
    }
    utils.Sleep(70)
    ctx.RefreshGameData()
    return true
}

// --- Fast vendor tab switch using stash tab coords (no sleeps) ---
func switchVendorTabFast(tab int) {
    if tab < 1 || tab > 4 {
        return
    }
    ctx := context.Get()
    var x, y int
    if ctx.GameReader.LegacyGraphics() {
        x = ui.SwitchStashTabBtnXClassic + (tab-1)*ui.SwitchStashTabBtnTabSizeClassic + ui.SwitchStashTabBtnTabSizeClassic/2
        y = ui.SwitchStashTabBtnYClassic
    } else {
        x = ui.SwitchStashTabBtnX + (tab-1)*ui.SwitchStashTabBtnTabSize + ui.SwitchStashTabBtnTabSize/2
        y = ui.SwitchStashTabBtnY
    }
    ctx.HID.Click(game.LeftButton, x, y)
    ctx.RefreshGameData()
}

// --- Scan & buy using exact vendor tab (Location.Page) ---
func scanAndPurchaseItems(_ npc.ID, plan ActionShoppingPlan) (itemsPurchased int, goldSpent int) {
    ctx := context.Get()

    // Rules: prefer runtime; fall back to plan.Rules if provided
    rules := ctx.Data.CharacterCfg.Runtime.Rules
    if len(rules) == 0 {
        rules = plan.Rules
    }

    if ctx.Data.PlayerUnit.TotalPlayerGold() < plan.MinGoldReserve {
        ctx.Logger.Info("Not enough gold to shop", slog.Int("currentGold", ctx.Data.PlayerUnit.TotalPlayerGold()))
        return 0, 0
    }

    // Snapshot grouping by vendor tab
    perTab := map[int][]data.UnitID{}
    itemsAll := ctx.Data.Inventory.ByLocation(item.LocationVendor)
    for _, it := range itemsAll {
        if !typeMatch(it, plan.Types) {
            continue
        }
        _, res := rules.EvaluateAll(it)
        accept := (res == nip.RuleResultFullMatch)
        if !accept && AutoEquipHook != nil && AutoEquipHook(it) {
            accept = true
        }
        if !accept {
            continue
        }
        tab := it.Location.Page + 1 // 0-based -> 1-based
        if tab < 1 || tab > 4 {
            continue
        }
        perTab[tab] = append(perTab[tab], it.UnitID)
    }

    // Lazy-load fallback: scan each tab if snapshot empty
    if len(perTab) == 0 {
        for tab := 1; tab <= 4; tab++ {
            switchVendorTabFast(tab)
            itemsTab := ctx.Data.Inventory.ByLocation(item.LocationVendor)
            for _, it := range itemsTab {
                if !typeMatch(it, plan.Types) {
                    continue
                }
                _, res := rules.EvaluateAll(it)
                accept := (res == nip.RuleResultFullMatch)
                if !accept && AutoEquipHook != nil && AutoEquipHook(it) {
                    accept = true
                }
                if !accept {
                    continue
                }
                perTab[tab] = append(perTab[tab], it.UnitID)
            }
        }
        if len(perTab) == 0 {
            return 0, 0
        }
    }

    // Deterministic order
    tabs := make([]int, 0, len(perTab))
    for t := range perTab {
        tabs = append(tabs, t)
    }
    sort.Ints(tabs)

    // Buy on the exact tab and match UnitID + tab
    for _, tab := range tabs {
        switchVendorTabFast(tab)

        for _, want := range perTab[tab] {
            itemsNow := ctx.Data.Inventory.ByLocation(item.LocationVendor)
            var target *data.Item
            for _, it := range itemsNow {
                if it.UnitID == want && (it.Location.Page+1) == tab {
                    target = &it
                    break
                }
            }
            if target == nil {
                continue
            }

            sp := ui.GetScreenCoordsForItem(*target)
            ctx.HID.MovePointer(sp.X, sp.Y)
            utils.Sleep(25 + rand.Intn(35))
            ctx.HID.Click(game.RightButton, sp.X, sp.Y)
            utils.Sleep(90)
            ctx.RefreshGameData()

            itemsPurchased++
        }
    }

    return itemsPurchased, goldSpent
}

func typeMatch(it data.Item, allow []string) bool {
    if len(allow) == 0 {
        return true
    }
    t := string(it.Desc().Type)
    for _, a := range allow {
        if a == t {
            return true
        }
    }
    return false
}

func openVendorTrade(vendorID npc.ID) {
    ctx := context.Get()
    if vendorID == npc.Halbu {
        ctx.HID.KeySequence(0x24 /*HOME*/, 0x0D /*ENTER*/)
    } else {
        ctx.HID.KeySequence(0x24 /*HOME*/, 0x28 /*DOWN*/, 0x0D /*ENTER*/)
    }
    utils.Sleep(40)
    ctx.RefreshGameData()
}

// Movement to vendor using action.MoveTo with provider func
func moveToVendor(vendorID npc.ID) error {
    ctx := context.Get()

    // Harrogath anchors
    switch vendorID {
    case npc.Drehya:
        _ = MoveToCoords(data.Position{X: 5107, Y: 5119})
        utils.Sleep(40); ctx.RefreshGameData()
    case npc.Malah:
        _ = MoveToCoords(data.Position{X: 5082, Y: 5030})
        utils.Sleep(40); ctx.RefreshGameData()
    }

    n, ok := ctx.Data.NPCs.FindOne(vendorID)
    if !ok || len(n.Positions) == 0 {
        if vendorID == npc.Drehya {
            for i := 0; i < 5 && (!ok || len(n.Positions) == 0); i++ {
                utils.Sleep(60); ctx.RefreshGameData()
                if nn, ok2 := ctx.Data.NPCs.FindOne(vendorID); ok2 && len(nn.Positions) > 0 { n, ok = nn, true; break }
            }
        }
        if (!ok || len(n.Positions) == 0) && vendorID == npc.Malah {
            if m, found := ctx.Data.Monsters.FindOne(npc.Malah, data.MonsterTypeNone); found {
                _ = MoveToCoords(m.Position); utils.Sleep(60); ctx.RefreshGameData()
                if nn, ok2 := ctx.Data.NPCs.FindOne(vendorID); ok2 && len(nn.Positions) > 0 { n, ok = nn, true }
            }
        }
        if !ok || len(n.Positions) == 0 {
            return fmt.Errorf("vendor %d not found", int(vendorID))
        }
    }

    cur := ctx.Data.PlayerUnit.Position
    target := n.Positions[0]
    bestd := (target.X-cur.X)*(target.X-cur.X) + (target.Y-cur.Y)*(target.Y-cur.Y)
    for _, p := range n.Positions[1:] {
        d := (p.X-cur.X)*(p.X-cur.X) + (p.Y-cur.Y)*(p.Y-cur.Y)
        if d < bestd { target, bestd = p, d }
    }

    // Provide position via function as expected by your MoveTo signature
    return MoveTo(func() (data.Position, bool) {
        return target, true
    })
}

// --- Town refresh helpers ---

func refreshTownPreferAnyaPortal(town area.ID, onlyAnya bool) error {
    ctx := context.Get()
    if town == area.Harrogath && onlyAnya {
        ctx.Logger.Debug("Refreshing town via Anya red portal (preferred)")
        _ = MoveToCoords(data.Position{X: 5116, Y: 5121})
        utils.Sleep(800); ctx.RefreshGameData()

        if redPortal, found := ctx.Data.Objects.FindOne(object.PermanentTownPortal); found {
            if err := InteractObject(redPortal, func() bool {
                return ctx.Data.AreaData.Area == area.NihlathaksTemple && ctx.Data.AreaData.IsInside(ctx.Data.PlayerUnit.Position)
            }); err == nil {
                utils.Sleep(140); ctx.RefreshGameData()
                if err2 := returnToTownViaAnyaRedPortalFromTemple(); err2 == nil { return nil }
                ctx.Logger.Debug("Temple->Town red-portal failed; falling back to waypoint")
            }
        }
    }
    return refreshTownViaWaypoint(town)
}

func refreshTownViaWaypoint(town area.ID) error {
    ctx := context.Get()
    candidates := actRefreshCandidates(town)
    for _, a := range candidates {
        if a == town { continue }
        if err := WayPoint(a); err == nil {
            utils.Sleep(90); ctx.RefreshGameData()
            if err := WayPoint(town); err == nil {
                utils.Sleep(90); ctx.RefreshGameData()
                return nil
            }
        }
    }
    return fmt.Errorf("no viable waypoint refresh for %s", town.Area().Name)
}

func actRefreshCandidates(town area.ID) []area.ID {
    switch town {
    case area.RogueEncampment:
        return []area.ID{area.ColdPlains, area.StonyField, area.DarkWood, area.BlackMarsh, area.OuterCloister}
    case area.LutGholein:
        return []area.ID{area.DryHills, area.FarOasis, area.LostCity, area.CanyonOfTheMagi, area.ArcaneSanctuary}
    case area.KurastDocks:
        return []area.ID{area.SpiderForest, area.GreatMarsh, area.FlayerJungle, area.LowerKurast}
    case area.ThePandemoniumFortress:
        return []area.ID{area.CityOfTheDamned, area.RiverOfFlame}
    case area.Harrogath:
        return []area.ID{area.FrigidHighlands, area.ArreatPlateau, area.CrystallinePassage}
    default:
        return []area.ID{}
    }
}

func returnToTownViaAnyaRedPortalFromTemple() error {
    ctx := context.Get()
    if ctx.Data.AreaData.Area != area.NihlathaksTemple { return fmt.Errorf("not in Nihlathak's Temple") }
    anchor := data.Position{X: 10073, Y: 13311}
    _ = MoveToCoords(anchor)
    utils.Sleep(90); ctx.RefreshGameData()

    redPortal, found := ctx.Data.Objects.FindOne(object.PermanentTownPortal)
    if !found {
        for _, off := range []data.Position{{2,0},{-2,0},{0,2},{0,-2}} {
            _ = MoveToCoords(data.Position{X: anchor.X + off.X, Y: anchor.Y + off.Y})
            utils.Sleep(60); ctx.RefreshGameData()
            if rp, ok := ctx.Data.Objects.FindOne(object.PermanentTownPortal); ok { redPortal, found = rp, true; break }
        }
    }
    if !found { return fmt.Errorf("temple red portal not found") }
    utils.Sleep(1000) // cooldown
    if err := InteractObject(redPortal, func() bool {
        return ctx.Data.AreaData.Area == area.Harrogath && ctx.Data.AreaData.IsInside(ctx.Data.PlayerUnit.Position)
    }); err != nil { return err }
    utils.Sleep(100); ctx.RefreshGameData()
    return nil
}

func ensureInTown(target area.ID) error {
    ctx := context.Get()
    if ctx.Data.PlayerUnit.Area == target { return nil }
    if err := WayPoint(target); err == nil {
        utils.Sleep(70); ctx.RefreshGameData(); return nil
    }
    return ReturnTown()
}

func groupVendorsByTown(list []npc.ID) (townOrder []area.ID, byTown map[area.ID][]npc.ID) {
    byTown = map[area.ID][]npc.ID{}
    seen := map[area.ID]bool{}
    for _, v := range list {
        townID, ok := VendorLocationMap[v]
        if !ok { continue }
        if !seen[townID] { seen[townID] = true; townOrder = append(townOrder, townID) }
        byTown[townID] = append(byTown[townID], v)
    }
    return
}
