package action

import (
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/area"
	"github.com/hectorgimenez/d2go/pkg/data/item"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/d2go/pkg/nip"
	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/ui"
	"github.com/hectorgimenez/koolo/internal/utils"
	"github.com/lxn/win"
)

// -----------------------------------------------------------------------------
// Small helpers
// -----------------------------------------------------------------------------

// utils.Sleep takes an int (ms)
func randomDelay(base int) { utils.Sleep(base + rand.Intn(150)) }

func openVendorTrade(vendorID npc.ID) {
	ctx := context.Get()
	// Some vendors have different menu index for "Trade"
	if vendorID == npc.Halbu {
		ctx.HID.KeySequence(win.VK_HOME, win.VK_RETURN)
	} else {
		ctx.HID.KeySequence(win.VK_HOME, win.VK_DOWN, win.VK_RETURN)
	}
	randomDelay(250)
	ctx.RefreshGameData()
}

// -----------------------------------------------------------------------------
// Config & rules
// -----------------------------------------------------------------------------

type ShopConfig struct {
	Enabled         bool
	MaxGoldToSpend  int
	MinGoldReserve  int
	RefreshesPerRun int
	VendorsToShop   []npc.ID
	ItemTypesToShop []string
	ShoppingRules   nip.Rules
}

func LoadShoppingRules(characterName string) (nip.Rules, error) {
	ctx := context.Get()
	pickitDir := fmt.Sprintf("config/%s/pickit\\", characterName)
	rules, err := nip.ReadDir(pickitDir)
	if err != nil {
		return nil, fmt.Errorf("error loading shopping rules from %s: %w", pickitDir, err)
	}
	ctx.Logger.Info("Shopping rules loaded successfully", slog.Int("ruleCount", len(rules)))
	return rules, nil
}

// -----------------------------------------------------------------------------
// Main entry: town-batched passes
// -----------------------------------------------------------------------------

func ShopAtVendors(config ShopConfig) error {
	ctx := context.Get()
	ctx.SetLastAction("ShopAtVendors")

	if !config.Enabled {
		ctx.Logger.Debug("Shopping disabled")
		return nil
	}
	if len(config.VendorsToShop) == 0 {
		ctx.Logger.Warn("No vendors configured")
		return nil
	}
	if len(config.ShoppingRules) == 0 {
		ctx.Logger.Warn("No shopping rules loaded")
		return nil
	}

	townOrder, byTown := groupVendorsByTown(config.VendorsToShop)
	if len(townOrder) == 0 {
		ctx.Logger.Warn("No vendor towns resolved from vendor list (check VendorLocationMap)")
		return nil
	}

	totalGoldSpent := 0
	totalItemsPurchased := 0

	for _, townArea := range townOrder {
		vendors := byTown[townArea]
		if len(vendors) == 0 {
			continue
		}

		// Ensure once per town batch
		if err := ensureInTown(townArea); err != nil {
			ctx.Logger.Warn("Could not ensure correct town, skipping town batch",
				slog.String("town", townArea.Area().Name), slog.Any("error", err))
			continue
		}
		utils.Sleep(300)
		ctx.RefreshGameData()

		passes := config.RefreshesPerRun
		if passes < 0 {
			passes = 0
		}

		for pass := 0; pass <= passes; pass++ {
			ctx.Logger.Debug("Shopping pass", slog.String("town", townArea.Area().Name), slog.Int("pass", pass))

			for _, vendorID := range vendors {
				gold, items, err := shopVendorSinglePass(vendorID, config)
				if err != nil {
					ctx.Logger.Warn("Single-pass vendor shopping failed",
						slog.Int("vendorID", int(vendorID)), slog.Any("error", err))
					continue
				}
				totalGoldSpent += gold
				totalItemsPurchased += items

				// Clean state before next vendor in same town
				step.CloseAllMenus()
				utils.Sleep(250)
				ctx.RefreshGameData()
			}

			// After we visited every vendor once, refresh town for the next pass
			if pass < passes {
				if err := refreshTownViaWaypoint(townArea); err != nil {
					ctx.Logger.Warn("Town refresh failed; stopping further passes",
						slog.String("town", townArea.Area().Name), slog.Any("error", err))
					break
				}
				utils.Sleep(300)
				ctx.RefreshGameData()
			}
		}
	}

	ctx.Logger.Info("Shopping routine complete",
		slog.Int("totalGoldSpent", totalGoldSpent),
		slog.Int("totalItemsPurchased", totalItemsPurchased))
	return nil
}

// groupVendorsByTown preserves input order for towns and vendors.
// NOTE: VendorLocationMap is declared in shopping_helpers.go in your fork; we reuse it here.
func groupVendorsByTown(list []npc.ID) (townOrder []area.ID, byTown map[area.ID][]npc.ID) {
	byTown = make(map[area.ID][]npc.ID)
	seenTown := make(map[area.ID]bool)

	for _, v := range list {
		townArea, ok := VendorLocationMap[v]
		if !ok {
			continue
		}
		if !seenTown[townArea] {
			seenTown[townArea] = true
			townOrder = append(townOrder, townArea)
		}
		byTown[townArea] = append(byTown[townArea], v)
	}
	return
}

// -----------------------------------------------------------------------------
// One vendor - one pass (no refresh loop inside)
// -----------------------------------------------------------------------------

func shopVendorSinglePass(vendorID npc.ID, config ShopConfig) (int, int, error) {
	ctx := context.Get()
	ctx.SetLastAction("shopVendorSinglePass")

	goldSpent := 0
	itemsPurchased := 0

	// Move inside town to the vendor
	if err := moveToVendor(vendorID); err != nil {
		ctx.Logger.Warn("Failed to move to vendor", slog.Int("vendorID", int(vendorID)), slog.Any("err", err))
	}
	utils.Sleep(400)
	ctx.RefreshGameData()

	// Interact + open trade
	if err := InteractNPC(vendorID); err != nil {
		return 0, 0, fmt.Errorf("failed to interact with vendor %d: %w", int(vendorID), err)
	}
	openVendorTrade(vendorID)
	if !ctx.Data.OpenMenus.NPCShop {
		return 0, 0, fmt.Errorf("failed to open trade window for vendor %d", int(vendorID))
	}

	switchVendorTab(1)
	utils.Sleep(200)
	ctx.RefreshGameData()

	// Wait for items to load
	loaded := false
	for i := 0; i < 5; i++ {
		if len(ctx.Data.Inventory.ByLocation(item.LocationVendor)) > 0 {
			loaded = true
			break
		}
		time.Sleep(200 * time.Millisecond)
		ctx.RefreshGameData()
	}
	if !loaded {
		ctx.Logger.Warn("Vendor items did not load", slog.Int("vendorID", int(vendorID)))
	}

	// Respect minimum gold reserve
	if ctx.Data.PlayerUnit.TotalPlayerGold() < config.MinGoldReserve {
		ctx.Logger.Info("Not enough gold to shop", slog.Int("currentGold", ctx.Data.PlayerUnit.TotalPlayerGold()))
		return 0, 0, nil
	}

	// Scan & buy once (all tabs)
	purchased, spent := scanAndPurchaseItems(vendorID, config)
	itemsPurchased += purchased
	goldSpent += spent

	// Close out
	ctx.HID.PressKeyBinding(ctx.Data.KeyBindings.Inventory)
	utils.Sleep(200)
	ctx.RefreshGameData()

	return goldSpent, itemsPurchased, nil
}

// -----------------------------------------------------------------------------
// Town refresh via waypoint only (no TP)
// -----------------------------------------------------------------------------

func refreshTownViaWaypoint(town area.ID) error {
	ctx := context.Get()
	ctx.SetLastAction("refreshTownViaWaypoint")

	// Leave town to a nearby area (must have a waypoint)
	refreshWaypoints := map[area.ID]area.ID{
		area.RogueEncampment:        area.ColdPlains,
		area.LutGholein:             area.SewersLevel2Act2,
		area.KurastDocks:            area.SpiderForest,
		area.ThePandemoniumFortress: area.CityOfTheDamned,
		area.Harrogath:              area.BloodyFoothills,
	}
	neighbor, ok := refreshWaypoints[town]
	if !ok {
		return fmt.Errorf("no refresh waypoint for town %s", town.Area().Name)
	}

	// Go out via waypoint
	if err := WayPoint(neighbor); err != nil {
		return fmt.Errorf("failed to leave town to %s via waypoint: %w", neighbor.Area().Name, err)
	}
	utils.Sleep(300)
	ctx.RefreshGameData()

	// Come back to town via waypoint (preferred), fallback to ReturnTown only if needed
	if err := WayPoint(town); err != nil {
		ctx.Logger.Debug("WayPoint back to town failed, falling back to ReturnTown", slog.Any("error", err))
		if err2 := ReturnTown(); err2 != nil {
			return fmt.Errorf("failed to return to town via waypoint and TP: %w", err2)
		}
	}
	utils.Sleep(350)
	ctx.RefreshGameData()

	return nil
}

// -----------------------------------------------------------------------------
// Shared helpers kept from your working logic
// -----------------------------------------------------------------------------

// ensureInTown ensures we are in the target town. Prefer Waypoint over Town Portal.
func ensureInTown(target area.ID) error {
	ctx := context.Get()
	if ctx.Data.PlayerUnit.Area == target {
		ctx.Logger.Debug("Already in target town", slog.String("town", target.Area().Name))
		return nil
	}

	ctx.Logger.Debug("Ensuring presence in town (WP-first)", slog.String("town", target.Area().Name))

	// 1) Try going directly to the target town via waypoint
	if err := WayPoint(target); err == nil {
		utils.Sleep(300)
		ctx.RefreshGameData()
		if ctx.Data.PlayerUnit.Area == target {
			return nil
		}
	}

	// 2) Try hopping to a nearby outdoor area with WP and then WP back to town
	nearby := map[area.ID]area.ID{
		area.RogueEncampment:        area.ColdPlains,
		area.LutGholein:             area.SewersLevel2Act2,
		area.KurastDocks:            area.SpiderForest,
		area.ThePandemoniumFortress: area.CityOfTheDamned,
		area.Harrogath:              area.BloodyFoothills,
	}
	if near, ok := nearby[target]; ok {
		if err := WayPoint(near); err == nil {
			utils.Sleep(300)
			ctx.RefreshGameData()
			if err2 := WayPoint(target); err2 == nil {
				utils.Sleep(300)
				ctx.RefreshGameData()
				if ctx.Data.PlayerUnit.Area == target {
					return nil
				}
			}
		}
	}

	// 3) Last resort: use ReturnTown (TP)
	if err := ReturnTown(); err == nil {
		utils.Sleep(400)
		ctx.RefreshGameData()
		if ctx.Data.PlayerUnit.Area == target {
			return nil
		}
	}

	return fmt.Errorf("unable to reach town %s", target.Area().Name)
}

// switchVendorTab clicks the correct vendor tab depending on UI mode.
func switchVendorTab(tab int) {
	ctx := context.Get()
	ctx.SetLastStep("switchVendorTab")

	if tab < 1 || tab > 4 {
		ctx.Logger.Warn("Invalid vendor tab requested", slog.Int("tab", tab))
		return
	}

	if ctx.GameReader.LegacyGraphics() {
		x := ui.VendorTabStartXClassic + ui.VendorTabSizeClassic*tab - ui.VendorTabSizeClassic/2
		y := ui.VendorTabStartYClassic
		ctx.Logger.Debug("Clicking vendor tab (Classic UI)", slog.Int("tab", tab), slog.Int("x", x), slog.Int("y", y))
		ctx.HID.Click(game.LeftButton, x, y)
	} else {
		x := ui.VendorTabStartX + ui.VendorTabSize*tab - ui.VendorTabSize/2
		y := ui.VendorTabStartY
		ctx.Logger.Debug("Clicking vendor tab (Modern UI)", slog.Int("tab", tab), slog.Int("x", x), slog.Int("y", y))
		ctx.HID.Click(game.LeftButton, x, y)
	}

	utils.Sleep(500)
}

func scanAndPurchaseItems(vendorID npc.ID, config ShopConfig) (int, int) {
	ctx := context.Get()

	itemsPurchased := 0
	goldSpent := 0

	for tab := 1; tab <= 3; tab++ {
		switchVendorTab(tab)
		randomDelay(250)
		ctx.RefreshGameData()

		vendorItems := ctx.Data.Inventory.ByLocation(item.LocationVendor)
		itemsOnThisTab := make([]data.Item, 0, len(vendorItems))
		for _, itm := range vendorItems {
			itemsOnThisTab = append(itemsOnThisTab, itm)
		}

		ctx.Logger.Debug("Scanning vendor tab",
			slog.Int("tab", tab),
			slog.Int("totalVendorItems", len(vendorItems)),
			slog.Int("itemsOnThisTab", len(itemsOnThisTab)))

		for _, itm := range itemsOnThisTab {
			if !isItemTypeMatch(itm, config.ItemTypesToShop) {
				continue
			}
			_, result := config.ShoppingRules.EvaluateAll(itm)
			if result == nip.RuleResultFullMatch {
				ctx.Logger.Info("Found matching item",
					slog.String("item", string(itm.Name)),
					slog.Any("quality", itm.Quality),
					slog.Int("tab", tab))

				if err := purchaseAndStashItem(itm, vendorID, config, tab); err != nil {
					ctx.Logger.Warn("Failed to purchase item",
						slog.String("item", string(itm.Name)),
						slog.Any("error", err))
					continue
				}

				itemsPurchased++
				goldSpent += getItemPrice(itm)

				randomDelay(250)
				ctx.RefreshGameData()
			}
		}
	}

	return itemsPurchased, goldSpent
}

func isItemTypeMatch(itm data.Item, itemTypes []string) bool {
	if len(itemTypes) == 0 {
		return true
	}
	itemTypeName := string(itm.Desc().Type)
	for _, t := range itemTypes {
		if itemTypeName == t {
			return true
		}
	}
	return false
}

func purchaseAndStashItem(itm data.Item, vendorID npc.ID, config ShopConfig, currentTab int) error {
	ctx := context.Get()
	ctx.SetLastAction("purchaseAndStashItem")

	ctx.Logger.Info("Purchasing item",
		slog.String("item", string(itm.Name)),
		slog.String("quality", string(itm.Quality)))

	// Click & buy
	screenPos := ui.GetScreenCoordsForItem(itm)
	ctx.HID.MovePointer(screenPos.X, screenPos.Y)
	utils.Sleep(150)
	ctx.HID.Click(game.RightButton, screenPos.X, screenPos.Y)
	utils.Sleep(800)
	ctx.RefreshGameData()

	// Quick verification
	purchased := false
	if len(ctx.Data.Inventory.ByLocation(item.LocationCursor)) > 0 {
		purchased = true
	} else {
		for _, invItem := range ctx.Data.Inventory.ByLocation(item.LocationInventory) {
			if invItem.Name == itm.Name && !invItem.IsPotion() {
				purchased = true
				break
			}
		}
	}
	if !purchased {
		return fmt.Errorf("purchase verification failed")
	}

	// Stash via general stash logic (no cursor hacks)
	if err := stashPurchasedItem(vendorID); err != nil {
		return fmt.Errorf("failed to stash: %w", err)
	}

	// Return to the vendor and tab so we can continue scanning/buying
	if err := returnToVendor(vendorID, currentTab); err != nil {
		return fmt.Errorf("failed to return to vendor: %w", err)
	}

	return nil
}

func stashPurchasedItem(vendorID npc.ID) error {
	ctx := context.Get()

	step.CloseAllMenus()
	utils.Sleep(150)
	ctx.RefreshGameData()

	if err := OpenStash(); err != nil {
		return fmt.Errorf("open stash: %w", err)
	}
	utils.Sleep(250)
	ctx.RefreshGameData()

	stashGold()
	stashInventory(false)
	dropExcessItems()

	step.CloseAllMenus()
	utils.Sleep(150)
	ctx.RefreshGameData()
	return nil
}

func returnToVendor(vendorID npc.ID, returnToTab int) error {
	ctx := context.Get()
	ctx.SetLastAction("returnToVendor")

	step.CloseAllMenus()
	utils.Sleep(250)
	ctx.RefreshGameData()

	if err := InteractNPC(vendorID); err != nil {
		return fmt.Errorf("failed to interact with vendor: %w", err)
	}
	openVendorTrade(vendorID)
	if !ctx.Data.OpenMenus.NPCShop {
		return fmt.Errorf("failed to reopen trade window")
	}

	utils.Sleep(600)
	ctx.RefreshGameData()

	switchVendorTab(returnToTab)
	utils.Sleep(500)
	ctx.RefreshGameData()

	// Light wait for items to appear
	for attempt := 0; attempt < 3; attempt++ {
		if len(ctx.Data.Inventory.ByLocation(item.LocationVendor)) > 0 {
			break
		}
		utils.Sleep(250)
		ctx.RefreshGameData()
	}
	return nil
}

func getItemPrice(itm data.Item) int {
	// Placeholder price heuristic (not used for spending cap in this action)
	base := 1000
	switch itm.Quality {
	case item.QualityMagic:
		base *= 2
	case item.QualityRare:
		base *= 5
	case item.QualityUnique:
		base *= 10
	}
	return base
}

// Move inside town towards the vendor using known NPC positions.
func moveToVendor(vendorID npc.ID) error {
	ctx := context.Get()
	ctx.SetLastAction("moveToVendor")

	n, found := ctx.Data.NPCs.FindOne(vendorID)
	if !found || len(n.Positions) == 0 {
		return fmt.Errorf("vendor %d not found in NPC list for this town", int(vendorID))
	}

	player := ctx.Data.PlayerUnit.Position
	closest := n.Positions[0]
	best := math.MaxFloat64
	for _, p := range n.Positions {
		dx := float64(p.X - player.X)
		dy := float64(p.Y - player.Y)
		d := dx*dx + dy*dy
		if d < best {
			best = d
			closest = p
		}
	}

	if err := step.MoveTo(closest); err != nil {
		utils.Sleep(300)
		if err2 := step.MoveTo(closest); err2 != nil {
			return fmt.Errorf("failed to reach vendor %d after retry: %w", int(vendorID), err2)
		}
	}

	utils.Sleep(200)
	ctx.RefreshGameData()
	return nil
}
