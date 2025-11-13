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

// ActionShoppingPlan mirrors your previous space_checks struct to minimize diffs.
type ActionShoppingPlan struct {
	Enabled         bool
	RefreshesPerRun int
	MinGoldReserve  int
	Vendors         []npc.ID
	Rules           nip.Rules // optional override; if empty, shouldBePickedUp() is used
	Types           []string  // optional allow-list of item types (string of item.Desc().Type)
}

func NewActionShoppingPlanFromConfig(cfg config.ShoppingConfig) ActionShoppingPlan {
	return ActionShoppingPlan{
		Enabled:         cfg.Enabled,
		RefreshesPerRun: cfg.RefreshesPerRun,
		MinGoldReserve:  cfg.MinGoldReserve,
		Vendors:         vendorListFromConfig(cfg),
		Rules:           nil,
		Types:           nil,
	}
}

func vendorListFromConfig(cfg config.ShoppingConfig) []npc.ID {
	out := make([]npc.ID, 0, 10)
	rv := reflect.ValueOf(cfg)

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
	if f := rv.FieldByName("VendorsToShop"); f.IsValid() {
		if slice, ok := f.Interface().([]npc.ID); ok && len(slice) > 0 {
			return slice
		}
	}

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
	addIfTrue("VendorAnya", npc.Drehya)

	return out
}

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

	// Ensure we start with enough space
	if !ensureTwoFreeColumnsStrict() {
		ctx.Logger.Warn("Not enough adjacent space (two full columns) even after stashing; aborting shopping")
		return nil
	}

	// Group vendors by town once; we will iterate towns inside each pass
	townOrder, vendorsByTown := groupVendorsByTown(plan.Vendors)
	ctx.Logger.Debug("Shopping towns planned", slog.Int("count", len(townOrder)))

	passes := plan.RefreshesPerRun
	if passes < 0 {
		passes = 0
	}

	for pass := 0; pass <= passes; pass++ {
		ctx.Logger.Info("Shopping pass", slog.Int("pass", pass))

		for _, townID := range townOrder {
			vendors := vendorsByTown[townID]
			if len(vendors) == 0 {
				continue
			}

			if err := ensureInTown(townID); err != nil {
				ctx.Logger.Warn("Skipping town; cannot reach", slog.String("town", townID.Area().Name), slog.Any("err", err))
				continue
			}
			utils.Sleep(40)
			ctx.RefreshGameData()

			if !ensureTwoFreeColumnsStrict() {
				ctx.Logger.Warn("Insufficient space after stashing; skipping town batch", slog.String("town", townID.Area().Name))
				continue
			}

			for _, v := range vendors {
				if !ensureTwoFreeColumnsStrict() {
					ctx.Logger.Warn("Skipping vendor due to inventory space (need two free columns)", slog.Int("vendor", int(v)))
					break
				}
				if _, _, err := shopVendorSinglePass(v, plan); err != nil {
					ctx.Logger.Warn("Vendor pass failed", slog.Int("vendor", int(v)), slog.Any("err", err))
				}
				step.CloseAllMenus()
				ctx.RefreshGameData()
			}
		}

		// Between passes: if only one town is selected, perform an explicit refresh.
		// For multi-town configs, switching towns next pass effectively refreshes stock.
		if pass < passes {
			if len(townOrder) == 1 {
				firstTown := townOrder[0]
				vendors := vendorsByTown[firstTown]
				onlyAnya := len(vendors) == 1 && vendors[0] == npc.Drehya
				if err := refreshTownPreferAnyaPortal(firstTown, onlyAnya); err != nil {
					ctx.Logger.Warn("Town refresh failed; stopping further passes", slog.String("town", firstTown.Area().Name), slog.Any("err", err))
					break
				}
			}
		}
	}

	return nil
}

func shopVendorSinglePass(vendorID npc.ID, plan ActionShoppingPlan) (goldSpent int, itemsBought int, err error) {
	ctx := context.Get()

	if err := moveToVendor(vendorID); err != nil {
		ctx.Logger.Debug("moveToVendor reported", slog.Int("vendor", int(vendorID)), slog.Any("err", err))
	}
	utils.Sleep(30)
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

// --- Space helpers ---

func ensureTwoFreeColumnsStrict() bool {
	if hasTwoFreeColumns() {
		return true
	}
	ctx := context.Get()
	step.CloseAllMenus()
	utils.Sleep(30)
	ctx.RefreshGameData()
	if err := Stash(false); err != nil {
		ctx.Logger.Warn("Stash failed while ensuring two free columns", slog.Any("err", err))
		return false
	}
	utils.Sleep(50)
	ctx.RefreshGameData()
	return hasTwoFreeColumns()
}

func hasFreeRect(grid [4][10]bool, w, h int) bool {
	H := len(grid)
	if H == 0 {
		return false
	}
	W := len(grid[0])
	if W == 0 {
		return false
	}

	for y := 0; y <= H-h; y++ {
		for x := 0; x <= W-w; x++ {
			free := true
			for dy := 0; dy < h && free; dy++ {
				for dx := 0; dx < w; dx++ {
					if grid[y+dy][x+dx] {
						free = false
						break
					}
				}
			}
			if free {
				return true
			}
		}
	}
	return false
}

func hasTwoFreeColumns() bool {
	ctx := context.Get()
	grid := ctx.Data.Inventory.Matrix()
	h := len(grid)
	if h == 0 {
		return false
	}
	return hasFreeRect(grid, 2, h)
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

// --- Scan & buy with smart stash-and-return ---

func scanAndPurchaseItems(vendorID npc.ID, plan ActionShoppingPlan) (itemsPurchased int, goldSpent int) {
	ctx := context.Get()

	if ctx.Data.PlayerUnit.TotalPlayerGold() < plan.MinGoldReserve {
		ctx.Logger.Info("Not enough gold to shop", slog.Int("currentGold", ctx.Data.PlayerUnit.TotalPlayerGold()))
		return 0, 0
	}

	perTab := map[IntTab][]data.UnitID{}
	itemsAll := ctx.Data.Inventory.ByLocation(item.LocationVendor)
	for _, it := range itemsAll {
		if !typeMatch(it, plan.Types) {
			continue
		}
		if !shouldBePickedUp(it) {
			continue
		}
		tab := it.Location.Page + 1
		if tab < 1 || tab > 4 {
			continue
		}
		perTab[IntTab{Tab: tab}] = append(perTab[IntTab{Tab: tab}], it.UnitID)
	}

	if len(perTab) == 0 {
		for tab := 1; tab <= 4; tab++ {
			switchVendorTabFast(tab)
			itemsTab := ctx.Data.Inventory.ByLocation(item.LocationVendor)
			for _, it := range itemsTab {
				if !typeMatch(it, plan.Types) || !shouldBePickedUp(it) {
					continue
				}
				perTab[IntTab{Tab: tab}] = append(perTab[IntTab{Tab: tab}], it.UnitID)
			}
		}
		if len(perTab) == 0 {
			return 0, 0
		}
	}

	tabs := make([]int, 0, len(perTab))
	for t := range perTab {
		tabs = append(tabs, t.Tab)
	}
	sort.Ints(tabs)

	for _, tab := range tabs {
		switchVendorTabFast(tab)

		for {
			cands := collectTabCandidates(tab, plan)
			if len(cands) == 0 {
				break
			}

			progress := false
			for _, want := range cands {
				var target *data.Item
				itemsNow := ctx.Data.Inventory.ByLocation(item.LocationVendor)
				for _, it := range itemsNow {
					if (it.Location.Page+1) == tab && it.UnitID == want {
						target = &it
						break
					}
				}
				if target == nil {
					continue
				}

				if !itemFitsInventory(*target) {
					if !stashAndReturnToVendor(vendorID, tab) {
						ctx.Logger.Warn("Stash+return failed; aborting tab purchases", slog.Int("tab", tab))
						return itemsPurchased, goldSpent
					}
					progress = true
					break
				}

				sp := ui.GetScreenCoordsForItem(*target)
				ctx.HID.MovePointer(sp.X, sp.Y)
				utils.Sleep(15 + rand.Intn(25))
				ctx.HID.Click(game.RightButton, sp.X, sp.Y)
				utils.Sleep(50)
				ctx.RefreshGameData()
				itemsPurchased++
				progress = true

				if !hasTwoFreeColumns() {
					if !stashAndReturnToVendor(vendorID, tab) {
						ctx.Logger.Warn("Post-purchase stash+return failed", slog.Int("tab", tab))
						return itemsPurchased, goldSpent
					}
					break
				}
			}

			if !progress {
				break
			}
		}
	}

	return itemsPurchased, goldSpent
}

type IntTab struct{ Tab int }

func collectTabCandidates(tab int, plan ActionShoppingPlan) []data.UnitID {
	ctx := context.Get()
	switchVendorTabFast(tab)
	list := make([]data.UnitID, 0, 8)
	itemsNow := ctx.Data.Inventory.ByLocation(item.LocationVendor)
	for _, it := range itemsNow {
		if (it.Location.Page + 1) != tab {
			continue
		}
		if !typeMatch(it, plan.Types) || !shouldBePickedUp(it) {
			continue
		}
		list = append(list, it.UnitID)
	}
	return list
}

func stashAndReturnToVendor(vendorID npc.ID, tab int) bool {
	ctx := context.Get()

	step.CloseAllMenus()
	if err := Stash(false); err != nil {
		ctx.Logger.Warn("Stash failed", slog.Any("err", err))
		return false
	}
	utils.Sleep(60)
	ctx.RefreshGameData()

	if err := moveToVendor(vendorID); err != nil {
		ctx.Logger.Warn("Return to vendor failed", slog.Int("vendor", int(vendorID)), slog.Any("err", err))
		return false
	}
	if err := InteractNPC(vendorID); err != nil {
		ctx.Logger.Warn("Re-interact vendor failed", slog.Int("vendor", int(vendorID)), slog.Any("err", err))
		return false
	}
	openVendorTrade(vendorID)
	if !ctx.Data.OpenMenus.NPCShop {
		ctx.Logger.Warn("Vendor trade window did not open on return", slog.Int("vendor", int(vendorID)))
		return false
	}

	switchVendorTabFast(tab)
	ctx.RefreshGameData()
	return true
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
	utils.Sleep(20)
	ctx.RefreshGameData()
}

// moveToVendor goes to the closest exposed NPC position.
func moveToVendor(vendorID npc.ID) error {
	ctx := context.Get()

	switch vendorID {
	case npc.Drehya:
		_ = MoveToCoords(data.Position{X: 5107, Y: 5119})
		utils.Sleep(30)
		ctx.RefreshGameData()
	case npc.Malah:
		_ = MoveToCoords(data.Position{X: 5082, Y: 5030})
		utils.Sleep(30)
		ctx.RefreshGameData()
	}

	n, ok := ctx.Data.NPCs.FindOne(vendorID)
	if !ok || len(n.Positions) == 0 {
		if vendorID == npc.Drehya {
			for i := 0; i < 5 && (!ok || len(n.Positions) == 0); i++ {
				utils.Sleep(40)
				ctx.RefreshGameData()
				if nn, ok2 := ctx.Data.NPCs.FindOne(vendorID); ok2 && len(nn.Positions) > 0 {
					n, ok = nn, true
					break
				}
			}
		}
		if (!ok || len(n.Positions) == 0) && vendorID == npc.Malah {
			if m, found := ctx.Data.Monsters.FindOne(npc.Malah, data.MonsterTypeNone); found {
				_ = MoveToCoords(m.Position)
				utils.Sleep(40)
				ctx.RefreshGameData()
				if nn, ok2 := ctx.Data.NPCs.FindOne(vendorID); ok2 && len(nn.Positions) > 0 {
					n, ok = nn, true
				}
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
		if d < bestd {
			target, bestd = p, d
		}
	}

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
		utils.Sleep(600)
		ctx.RefreshGameData()

		if redPortal, found := ctx.Data.Objects.FindOne(object.PermanentTownPortal); found {
			if err := InteractObject(redPortal, func() bool {
				return ctx.Data.AreaData.Area == area.NihlathaksTemple && ctx.Data.AreaData.IsInside(ctx.Data.PlayerUnit.Position)
			}); err == nil {
				utils.Sleep(120)
				ctx.RefreshGameData()
				if err2 := returnToTownViaAnyaRedPortalFromTemple(); err2 == nil {
					return nil
				}
				ctx.Logger.Debug("Temple->Town red-portal failed; falling back to waypoint")
			}
		}
	}
	return refreshTownViaWaypoint(town)
}

func refreshTownViaWaypoint(town area.ID) error {
	// prefer a tagged switch (satisfies QF1003), silence exhaustive with nolint
	//nolint:exhaustive
	switch town {
	case area.RogueEncampment:
		return hopOutAndBack(town, []area.ID{
			area.ColdPlains, area.StonyField, area.DarkWood, area.BlackMarsh, area.OuterCloister,
		})
	case area.LutGholein:
		return hopOutAndBack(town, []area.ID{
			area.DryHills, area.FarOasis, area.LostCity, area.CanyonOfTheMagi, area.ArcaneSanctuary,
		})
	case area.KurastDocks:
		return hopOutAndBack(town, []area.ID{
			area.SpiderForest, area.GreatMarsh, area.FlayerJungle, area.LowerKurast,
		})
	case area.ThePandemoniumFortress:
		return hopOutAndBack(town, []area.ID{
			area.CityOfTheDamned, area.RiverOfFlame,
		})
	case area.Harrogath:
		return hopOutAndBack(town, []area.ID{
			area.FrigidHighlands, area.ArreatPlateau, area.CrystallinePassage,
		})
	default:
		return fmt.Errorf("no viable waypoint refresh for %s", town.Area().Name)
	}
}

func hopOutAndBack(town area.ID, candidates []area.ID) error {
	ctx := context.Get()
	for _, a := range candidates {
		if a == town {
			continue
		}
		if err := WayPoint(a); err == nil {
			utils.Sleep(70)
			ctx.RefreshGameData()
			if err := WayPoint(town); err == nil {
				utils.Sleep(70)
				ctx.RefreshGameData()
				return nil
			}
		}
	}
	return fmt.Errorf("no candidate waypoint worked for %s", town.Area().Name)
}

func returnToTownViaAnyaRedPortalFromTemple() error {
	ctx := context.Get()
	if ctx.Data.AreaData.Area != area.NihlathaksTemple {
		return fmt.Errorf("not in Nihlathak's Temple")
	}
	anchor := data.Position{X: 10073, Y: 13311}
	_ = MoveToCoords(anchor)
	utils.Sleep(70)
	ctx.RefreshGameData()

	redPortal, found := ctx.Data.Objects.FindOne(object.PermanentTownPortal)
	if !found {
		probes := []data.Position{
			{X: 2, Y: 0},
			{X: -2, Y: 0},
			{X: 0, Y: 2},
			{X: 0, Y: -2},
		}
		for _, off := range probes {
			_ = MoveToCoords(data.Position{X: anchor.X + off.X, Y: anchor.Y + off.Y})
			utils.Sleep(50)
			ctx.RefreshGameData()
			if rp, ok := ctx.Data.Objects.FindOne(object.PermanentTownPortal); ok {
				redPortal, found = rp, true
				break
			}
		}
	}
	if !found {
		return fmt.Errorf("temple red portal not found")
	}
	utils.Sleep(800) // cooldown
	if err := InteractObject(redPortal, func() bool {
		return ctx.Data.AreaData.Area == area.Harrogath && ctx.Data.AreaData.IsInside(ctx.Data.PlayerUnit.Position)
	}); err != nil {
		return err
	}
	utils.Sleep(80)
	ctx.RefreshGameData()
	return nil
}

func ensureInTown(target area.ID) error {
	ctx := context.Get()
	if ctx.Data.PlayerUnit.Area == target {
		return nil
	}
	if err := WayPoint(target); err == nil {
		utils.Sleep(50)
		ctx.RefreshGameData()
		return nil
	}
	return ReturnTown()
}

func lookupVendorTown(v npc.ID) (area.ID, bool) {
	// Prefer project-defined map if present
	if townID, ok := VendorLocationMap[v]; ok {
		return townID, true
	}
	// Fallback mapping to ensure multi-town selections always work
	switch v {
	case npc.Akara, npc.Charsi, npc.Gheed:
		return area.RogueEncampment, true
	case npc.Fara, npc.Drognan, npc.Elzix:
		return area.LutGholein, true
	case npc.Ormus:
		return area.KurastDocks, true
	case npc.Halbu:
		return area.ThePandemoniumFortress, true
	case npc.Malah, npc.Drehya:
		return area.Harrogath, true
	default:
		return 0, false
	}
}

func groupVendorsByTown(list []npc.ID) (townOrder []area.ID, byTown map[area.ID][]npc.ID) {
	byTown = map[area.ID][]npc.ID{}
	seen := map[area.ID]bool{}

	for _, v := range list {
		townID, ok := lookupVendorTown(v)
		if !ok {
			continue
		}
		if !seen[townID] {
			seen[townID] = true
			townOrder = append(townOrder, townID)
		}
		byTown[townID] = append(byTown[townID], v)
	}
	return
}
