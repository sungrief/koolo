
package action

import (
    "fmt"
    "log/slog"
    "math/rand"
    "sort"

    "github.com/hectorgimenez/d2go/pkg/data"
    "github.com/hectorgimenez/d2go/pkg/data/area"
    "github.com/hectorgimenez/d2go/pkg/data/item"
    "github.com/hectorgimenez/d2go/pkg/data/npc"
    "github.com/hectorgimenez/d2go/pkg/data/object"
    "github.com/hectorgimenez/d2go/pkg/nip"
    "github.com/hectorgimenez/koolo/internal/action/step"
    "github.com/hectorgimenez/koolo/internal/context"
    "github.com/hectorgimenez/koolo/internal/game"
    "github.com/hectorgimenez/koolo/internal/ui"
    "github.com/hectorgimenez/koolo/internal/utils"
)

type ShoppingConfig struct {
    Enabled         bool
    RefreshesPerRun int
    MinGoldReserve  int
    Vendors         []npc.ID
    Rules           nip.Rules
    Types           []string
}

func RunShopping(cfg ShoppingConfig) error {
    ctx := context.Get()
    if !cfg.Enabled {
        ctx.Logger.Debug("Shopping disabled")
        return nil
    }
    if len(cfg.Vendors) == 0 {
        ctx.Logger.Warn("No vendors selected for shopping")
        return nil
    }
    if len(cfg.Rules) == 0 && len(ctx.Data.CharacterCfg.Runtime.Rules) == 0 {
        ctx.Logger.Warn("No shopping rules loaded")
        return nil
    }

    townOrder, vendorsByTown := groupVendorsByTown(cfg.Vendors)

    for _, townID := range townOrder {
        vendors := vendorsByTown[townID]
        if len(vendors) == 0 {
            continue
        }

        if err := ensureInTown(townID); err != nil {
            ctx.Logger.Warn("Skipping town; cannot reach", slog.String("town", townID.Area().Name), slog.Any("err", err))
            continue
        }
        utils.Sleep(100)
        ctx.RefreshGameData()

        stashIfNeeded(6)

        passes := cfg.RefreshesPerRun
        if passes < 0 { passes = 0 }

        for pass := 0; pass <= passes; pass++ {
            ctx.Logger.Info("Shopping pass", slog.String("town", townID.Area().Name), slog.Int("pass", pass))

            for _, v := range vendors {
                _, _, _ = shopVendorSinglePass(v, cfg)
                step.CloseAllMenus()
                utils.Sleep(60)
                ctx.RefreshGameData()
                stashIfNeeded(6)
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

func shopVendorSinglePass(vendorID npc.ID, cfg ShoppingConfig) (goldSpent int, itemsBought int, err error) {
    ctx := context.Get()

    // Let step.MoveTo handle its own stuck/unstuck internally; no custom nudging here.
    _ = moveToVendor(vendorID)
    utils.Sleep(60)
    ctx.RefreshGameData()

    if err = step.InteractNPC(vendorID); err != nil {
        return 0, 0, fmt.Errorf("interact with vendor %d: %w", int(vendorID), err)
    }

    openVendorTrade(vendorID)
    if !ctx.Data.OpenMenus.NPCShop {
        return 0, 0, fmt.Errorf("vendor trade window did not open for ID=%d", int(vendorID))
    }

    itemsBought, goldSpent = scanAndPurchaseItems(vendorID, cfg)
    return goldSpent, itemsBought, nil
}

// --- Inventory helpers ---

func isInventoryAlmostFull(minFree int) bool {
    ctx := context.Get()
    totalSlots := 40 // 10x4 approximation
    used := len(ctx.Data.Inventory.ByLocation(item.LocationInventory))
    free := totalSlots - used
    return free < minFree
}

func stashIfNeeded(minFree int) {
    ctx := context.Get()
    if !isInventoryAlmostFull(minFree) { return }
    step.CloseAllMenus()
    utils.Sleep(80)
    ctx.RefreshGameData()
    if err := Stash(false); err != nil {
        ctx.Logger.Warn("Stash attempt failed", slog.Any("err", err))
    } else {
        utils.Sleep(100)
        ctx.RefreshGameData()
    }
}

// --- Fast vendor tab switch (reuse stash coords), no sleeps ---
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

// --- Buy logic using Location.Page (vendor tab) ---
func scanAndPurchaseItems(_ npc.ID, cfg ShoppingConfig) (itemsPurchased int, goldSpent int) {
    ctx := context.Get()

    if ctx.Data.PlayerUnit.TotalPlayerGold() < cfg.MinGoldReserve {
        ctx.Logger.Info("Not enough gold to shop", slog.Int("currentGold", ctx.Data.PlayerUnit.TotalPlayerGold()))
        return 0, 0
    }

    rules := ctx.Data.CharacterCfg.Runtime.Rules
    if len(rules) == 0 { rules = cfg.Rules }

    // Primary discovery: group by vendor page from a single snapshot
    perTab := map[int][]data.UnitID{}
    itemsAll := ctx.Data.Inventory.ByLocation(item.LocationVendor)
    for _, it := range itemsAll {
        if !typeMatch(it, cfg.Types) { continue }
        if _, res := rules.EvaluateAll(it); res != nip.RuleResultFullMatch { continue }
        tab := it.Location.Page + 1 // 0-based -> 1-based
        if tab < 1 || tab > 4 { continue }
        perTab[tab] = append(perTab[tab], it.UnitID)
    }

    // Fallback: if snapshot was empty (lazy loading), scan per tab
    if len(perTab) == 0 {
        for tab := 1; tab <= 4; tab++ {
            switchVendorTabFast(tab)
            itemsTab := ctx.Data.Inventory.ByLocation(item.LocationVendor)
            for _, it := range itemsTab {
                if !typeMatch(it, cfg.Types) { continue }
                if _, res := rules.EvaluateAll(it); res != nip.RuleResultFullMatch { continue }
                perTab[tab] = append(perTab[tab], it.UnitID)
            }
        }
        if len(perTab) == 0 {
            return 0, 0
        }
    }

    // Deterministic tab order
    tabs := make([]int, 0, len(perTab))
    for t := range perTab { tabs = append(tabs, t) }
    sort.Ints(tabs)

    // Purchase: switch ONLY to tabs that have candidates; match by UnitID + tab
    for _, tab := range tabs {
        if isInventoryAlmostFull(2) { break }
        switchVendorTabFast(tab)

        for _, want := range perTab[tab] {
            if isInventoryAlmostFull(2) {
                ctx.Logger.Debug("Inventory almost full during vendor buy; stopping on this vendor")
                break
            }
            // Re-read current vendor list and match exactly
            itemsNow := ctx.Data.Inventory.ByLocation(item.LocationVendor)
            var target *data.Item
            for _, it := range itemsNow {
                if it.UnitID == want && (it.Location.Page+1) == tab {
                    target = &it
                    break
                }
            }
            if target == nil { continue }

            sp := ui.GetScreenCoordsForItem(*target)
            ctx.HID.MovePointer(sp.X, sp.Y)
            utils.Sleep(35 + rand.Intn(45))
            ctx.HID.Click(game.RightButton, sp.X, sp.Y)
            utils.Sleep(110)
            ctx.RefreshGameData()

            itemsPurchased++
        }
    }

    return itemsPurchased, goldSpent
}

func typeMatch(it data.Item, allow []string) bool {
    if len(allow) == 0 { return true }
    t := string(it.Desc().Type)
    for _, a := range allow { if a == t { return true } }
    return false
}

func openVendorTrade(vendorID npc.ID) {
    ctx := context.Get()
    if vendorID == npc.Halbu {
        ctx.HID.KeySequence(0x24 /*HOME*/, 0x0D /*ENTER*/)
    } else {
        ctx.HID.KeySequence(0x24 /*HOME*/, 0x28 /*DOWN*/, 0x0D /*ENTER*/)
    }
    utils.Sleep(60)
    ctx.RefreshGameData()
}

// --- Simple movement to vendor; rely on step.MoveTo internal unstuck ---
func moveToVendor(vendorID npc.ID) error {
    ctx := context.Get()

    // Anchors for A5 NPCs (help memory load)
    switch vendorID {
    case npc.Drehya:
        _ = MoveToCoords(data.Position{X: 5107, Y: 5119})
        utils.Sleep(60); ctx.RefreshGameData()
    case npc.Malah:
        _ = MoveToCoords(data.Position{X: 5082, Y: 5030})
        utils.Sleep(60); ctx.RefreshGameData()
    }

    // Resolve positions
    n, ok := ctx.Data.NPCs.FindOne(vendorID)
    if !ok || len(n.Positions) == 0 {
        if vendorID == npc.Drehya {
            // small coax loop to trigger loading
            for i := 0; i < 5 && (!ok || len(n.Positions) == 0); i++ {
                utils.Sleep(80); ctx.RefreshGameData()
                if nn, ok2 := ctx.Data.NPCs.FindOne(vendorID); ok2 && len(nn.Positions) > 0 { n, ok = nn, true; break }
            }
        }
        if (!ok || len(n.Positions) == 0) && vendorID == npc.Malah {
            if m, found := ctx.Data.Monsters.FindOne(npc.Malah, data.MonsterTypeNone); found {
                _ = MoveToCoords(m.Position); utils.Sleep(80); ctx.RefreshGameData()
                if nn, ok2 := ctx.Data.NPCs.FindOne(vendorID); ok2 && len(nn.Positions) > 0 { n, ok = nn, true }
            }
        }
        if !ok || len(n.Positions) == 0 {
            return fmt.Errorf("vendor %d not found", int(vendorID))
        }
    }

    // Choose nearest position
    cur := ctx.Data.PlayerUnit.Position
    target := n.Positions[0]
    bestd := (target.X-cur.X)*(target.X-cur.X) + (target.Y-cur.Y)*(target.Y-cur.Y)
    for _, p := range n.Positions[1:] {
        d := (p.X-cur.X)*(p.X-cur.X) + (p.Y-cur.Y)*(p.Y-cur.Y)
        if d < bestd { target, bestd = p, d }
    }

    // Single MoveTo with gentle options; let step.MoveTo handle repath/unstuck internally
    return step.MoveTo(target,
        step.WithDistanceToFinish(3),
        step.WithIgnoreItems(),
        step.WithIgnoreMonsters(),
    )
}

// --- Refresh helpers ---

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
                utils.Sleep(160); ctx.RefreshGameData()
                if err2 := returnToTownViaAnyaRedPortalFromTemple(); err2 == nil { return nil }
                ctx.Logger.Debug("Temple->Town red-portal failed; falling back to waypoint")
            }
        }
    }
    return refreshTownViaWaypoint(town)
}

func returnToTownViaAnyaRedPortalFromTemple() error {
    ctx := context.Get()
    if ctx.Data.AreaData.Area != area.NihlathaksTemple { return fmt.Errorf("not in Nihlathak's Temple") }
    anchor := data.Position{X: 10073, Y: 13311}
    _ = MoveToCoords(anchor)
    utils.Sleep(120); ctx.RefreshGameData()

    redPortal, found := ctx.Data.Objects.FindOne(object.PermanentTownPortal)
    if !found {
        for _, off := range []data.Position{{2,0},{-2,0},{0,2},{0,-2}} {
            _ = MoveToCoords(data.Position{X: anchor.X + off.X, Y: anchor.Y + off.Y})
            utils.Sleep(80); ctx.RefreshGameData()
            if rp, ok := ctx.Data.Objects.FindOne(object.PermanentTownPortal); ok { redPortal, found = rp, true; break }
        }
    }
    if !found { return fmt.Errorf("temple red portal not found") }
    utils.Sleep(1000) // cooldown before reuse (1s)
    if err := InteractObject(redPortal, func() bool {
        return ctx.Data.AreaData.Area == area.Harrogath && ctx.Data.AreaData.IsInside(ctx.Data.PlayerUnit.Position)
    }); err != nil { return err }
    utils.Sleep(140); ctx.RefreshGameData()
    return nil
}

func refreshTownViaWaypoint(town area.ID) error {
    ctx := context.Get()
    neighbor := map[area.ID]area.ID{
        area.RogueEncampment:        area.ColdPlains,
        area.LutGholein:             area.SewersLevel2Act2,
        area.KurastDocks:            area.SpiderForest,
        area.ThePandemoniumFortress: area.CityOfTheDamned,
        area.Harrogath:              area.FrigidHighlands,
    }[town]
    if neighbor == 0 { return fmt.Errorf("no neighbor WP for %s", town.Area().Name) }
    if err := WayPoint(neighbor); err != nil { return err }
    utils.Sleep(120); ctx.RefreshGameData()
    if err := WayPoint(town); err != nil { return err }
    utils.Sleep(120); ctx.RefreshGameData()
    return nil
}

func ensureInTown(target area.ID) error {
    ctx := context.Get()
    if ctx.Data.PlayerUnit.Area == target { return nil }
    if err := WayPoint(target); err == nil {
        utils.Sleep(100); ctx.RefreshGameData(); return nil
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
