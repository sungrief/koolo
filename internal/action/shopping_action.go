
package action

import (
    "fmt"
    "log/slog"
    "math/rand"

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
        utils.Sleep(200)
        ctx.RefreshGameData()

        passes := cfg.RefreshesPerRun
        if passes < 0 {
            passes = 0
        }

        for pass := 0; pass <= passes; pass++ {
            ctx.Logger.Info("Shopping pass",
                slog.String("town", townID.Area().Name),
                slog.Int("pass", pass),
            )

            for _, v := range vendors {
                _, _, _ = shopVendorSinglePass(v, cfg)
                step.CloseAllMenus()
                utils.Sleep(120)
                ctx.RefreshGameData()
            }

            if pass < passes {
                // Prefer Anya red-portal refresh if this town batch is only Anya
                onlyAnya := len(vendors) == 1 && vendors[0] == npc.Drehya
                if err := refreshTownPreferAnyaPortal(townID, onlyAnya); err != nil {
                    ctx.Logger.Warn("Town refresh failed; stopping further passes",
                        slog.String("town", townID.Area().Name), slog.Any("err", err))
                    break
                }
            }
        }
    }

    return nil
}

func shopVendorSinglePass(vendorID npc.ID, cfg ShoppingConfig) (goldSpent int, itemsBought int, err error) {
    ctx := context.Get()

    if err = moveToVendor(vendorID); err != nil {
        ctx.Logger.Warn("Failed to move to vendor", slog.Int("vendor", int(vendorID)), slog.Any("err", err))
    }
    utils.Sleep(120)
    ctx.RefreshGameData()

    if err = step.InteractNPC(vendorID); err != nil {
        return 0, 0, fmt.Errorf("interact with vendor %d: %w", int(vendorID), err)
    }
    openVendorTrade(vendorID)

    if !ctx.Data.OpenMenus.NPCShop {
        return 0, 0, fmt.Errorf("vendor trade window did not open for %s", fmt.Sprintf("ID=%d", int(vendorID)))
    }

    itemsBought, goldSpent = scanAndPurchaseItems(vendorID, cfg)
    return goldSpent, itemsBought, nil
}

func scanAndPurchaseItems(_ npc.ID, cfg ShoppingConfig) (itemsPurchased int, goldSpent int) {
    ctx := context.Get()

    for tab := 1; tab <= 4; tab++ {
        switchVendorTab(tab)
        utils.Sleep(150)
        ctx.RefreshGameData()

        for i := 0; i < 5 && len(ctx.Data.Inventory.ByLocation(item.LocationVendor)) == 0; i++ {
            utils.Sleep(120)
            ctx.RefreshGameData()
        }

        for _, it := range ctx.Data.Inventory.ByLocation(item.LocationVendor) {
            if !typeMatch(it, cfg.Types) {
                continue
            }
            // Always read the latest pickit in case user reloaded config mid-run
            rules := ctx.Data.CharacterCfg.Runtime.Rules
            if len(rules) == 0 {
                rules = cfg.Rules
            }
            _, res := rules.EvaluateAll(it)
            if res != nip.RuleResultFullMatch {
                continue
            }

            sp := ui.GetScreenCoordsForItem(it)
            ctx.HID.MovePointer(sp.X, sp.Y)
            utils.Sleep(60 + rand.Intn(60))
            ctx.HID.Click(game.RightButton, sp.X, sp.Y)
            utils.Sleep(250)
            ctx.RefreshGameData()

            // IMPORTANT: Do NOT stash while vendor window is open (CTRL+LMB would sell items).
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
    utils.Sleep(120)
    ctx.RefreshGameData()
}

func switchVendorTab(tab int) {
    if tab < 1 || tab > 4 {
        return
    }
    ctx := context.Get()
    if ctx.GameReader.LegacyGraphics() {
        x := ui.VendorTabStartXClassic + ui.VendorTabSizeClassic*tab - ui.VendorTabSizeClassic/2
        y := ui.VendorTabStartYClassic
        ctx.HID.Click(game.LeftButton, x, y)
    } else {
        x := ui.VendorTabStartX + ui.VendorTabSize*tab - ui.VendorTabSize/2
        y := ui.VendorTabStartY
        ctx.HID.Click(game.LeftButton, x, y)
    }
    utils.Sleep(120)
}

func moveToVendor(vendorID npc.ID) error {
    ctx := context.Get()

    // Pre-anchor: force-load vendors that sometimes don't show until you're close.
    switch vendorID {
    case npc.Drehya: // Anya
        _ = MoveToCoords(data.Position{X: 5107, Y: 5119})
        utils.Sleep(120)
        ctx.RefreshGameData()
    case npc.Malah: // Malah (hard anchor)
        _ = MoveToCoords(data.Position{X: 5082, Y: 5030})
        utils.Sleep(120)
        ctx.RefreshGameData()
    }

    // First attempt via NPC list.
    n, ok := ctx.Data.NPCs.FindOne(vendorID)
    if !ok || len(n.Positions) == 0 {
        // Malah can be missing until very close: try monster index first,
        // then a slight upward nudge to coax a load, then re-check NPCs.
        if vendorID == npc.Malah {
            if m, found := ctx.Data.Monsters.FindOne(npc.Malah, data.MonsterTypeNone); found {
                _ = MoveToCoords(m.Position)
                utils.Sleep(150)
                ctx.RefreshGameData()
                if nn, ok2 := ctx.Data.NPCs.FindOne(vendorID); ok2 && len(nn.Positions) > 0 {
                    n, ok = nn, true
                }
            } else {
                p := ctx.Data.PlayerUnit.Position
                _ = MoveToCoords(data.Position{X: p.X, Y: p.Y - 6})
                utils.Sleep(120)
                ctx.RefreshGameData()
                if nn, ok2 := ctx.Data.NPCs.FindOne(vendorID); ok2 && len(nn.Positions) > 0 {
                    n, ok = nn, true
                }
            }
        }
        // Still not found?
        if !ok || len(n.Positions) == 0 {
            return fmt.Errorf("vendor %d not found", int(vendorID))
        }
    }

    // Choose nearest known vendor position and move there.
    target := n.Positions[0]
    if len(n.Positions) > 1 {
        cur := ctx.Data.PlayerUnit.Position
        best := target
        bestd := (best.X-cur.X)*(best.X-cur.X) + (best.Y-cur.Y)*(best.Y-cur.Y)
        for _, p := range n.Positions[1:] {
            d := (p.X-cur.X)*(p.X-cur.X) + (p.Y-cur.Y)*(p.Y-cur.Y)
            if d < bestd {
                best, bestd = p, d
            }
        }
        target = best
    }

    if err := step.MoveTo(target); err != nil {
        return err
    }
    return nil
}

// --- Refresh helpers ---

// Uses Anya red portal both ways when only Anya is enabled.
func refreshTownPreferAnyaPortal(town area.ID, onlyAnya bool) error {
    ctx := context.Get()
    // If only Anya is being shopped in A5, try to refresh through her red portal.
    if town == area.Harrogath && onlyAnya {
        ctx.Logger.Debug("Refreshing town via Anya red portal (preferred)")

        // Move near the portal (same anchor as Pindle run)
        _ = MoveToCoords(data.Position{X: 5116, Y: 5121})
        utils.Sleep(150)
        ctx.RefreshGameData()

        if redPortal, found := ctx.Data.Objects.FindOne(object.PermanentTownPortal); found {
            // Cross into Nihlathak's Temple using the same pattern as your Pindle run.
            err := InteractObject(redPortal, func() bool {
                return ctx.Data.AreaData.Area == area.NihlathaksTemple && ctx.Data.AreaData.IsInside(ctx.Data.PlayerUnit.Position)
            })
            if err == nil {
                utils.Sleep(200)
                ctx.RefreshGameData()
                // Now return to town using the temple-side red portal.
                rerr := returnToTownViaAnyaRedPortalFromTemple()
                if rerr == nil {
                    return nil
                } else {
                    ctx.Logger.Debug("Temple->Town red-portal failed; falling back to waypoint", slog.Any("err", rerr))
                }
            } else {
                // Portal interact failed; fall back.
                ctx.Logger.Debug("Red-portal interact failed; falling back to waypoint", slog.Any("err", err))
            }
        } else {
            ctx.Logger.Debug("Red portal not found; falling back to waypoint refresh")
        }
    }
    // Fallback
    return refreshTownViaWaypoint(town)
}

// returnToTownViaAnyaRedPortalFromTemple moves to the temple-side portal and clicks it to get back to Harrogath.
func returnToTownViaAnyaRedPortalFromTemple() error {
    ctx := context.Get()
    if ctx.Data.AreaData.Area != area.NihlathaksTemple {
        return fmt.Errorf("not in Nihlathak's Temple")
    }

    // Anchor near temple portal (user-provided coords)
    anchor := data.Position{X: 10073, Y: 13311}
    _ = MoveToCoords(anchor)
    utils.Sleep(150)
    ctx.RefreshGameData()

    // Try to find a permanent red portal in the area
    redPortal, found := ctx.Data.Objects.FindOne(object.PermanentTownPortal)
    if !found {
        // small nudge pattern to coax load
        offsets := []data.Position{{X: 2, Y: 0}, {X: -2, Y: 0}, {X: 0, Y: 2}, {X: 0, Y: -2}}
        for _, off := range offsets {
            _ = MoveToCoords(data.Position{X: anchor.X + off.X, Y: anchor.Y + off.Y})
            utils.Sleep(120)
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

    // Cooldown before reusing red portal
        utils.Sleep(2000)

        // Interact until we are back in Harrogath
    if err := InteractObject(redPortal, func() bool {
        return ctx.Data.AreaData.Area == area.Harrogath && ctx.Data.AreaData.IsInside(ctx.Data.PlayerUnit.Position)
    }); err != nil {
        return err
    }

    utils.Sleep(200)
    ctx.RefreshGameData()
    return nil
}

func refreshTownViaWaypoint(town area.ID) error {
    ctx := context.Get()

    neighbor := map[area.ID]area.ID{
        area.RogueEncampment:        area.ColdPlains,
        area.LutGholein:             area.SewersLevel2Act2,
        area.KurastDocks:            area.SpiderForest,
        area.ThePandemoniumFortress: area.CityOfTheDamned,
        area.Harrogath:              area.FrigidHighlands, // updated per request
    }[town]
    if neighbor == 0 {
        return fmt.Errorf("no neighbor WP for %s", town.Area().Name)
    }

    if err := WayPoint(neighbor); err != nil {
        return err
    }
    utils.Sleep(200)
    ctx.RefreshGameData()

    if err := WayPoint(town); err != nil {
        return err
    }
    utils.Sleep(200)
    ctx.RefreshGameData()
    return nil
}

func ensureInTown(target area.ID) error {
    ctx := context.Get()
    if ctx.Data.PlayerUnit.Area == target {
        return nil
    }
    if err := WayPoint(target); err == nil {
        utils.Sleep(140)
        ctx.RefreshGameData()
        return nil
    }
    return ReturnTown()
}

func groupVendorsByTown(list []npc.ID) (townOrder []area.ID, byTown map[area.ID][]npc.ID) {
    byTown = map[area.ID][]npc.ID{}
    seen := map[area.ID]bool{}
    for _, v := range list {
        townID, ok := VendorLocationMap[v]
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
