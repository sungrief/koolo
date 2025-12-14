package action

import (
	"fmt"
	"strings"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/area"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/event"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/utils"
)

func InteractNPC(npc npc.ID) error {
	ctx := context.Get()
	ctx.SetLastAction("InteractNPC")

	pos, found := getNPCPosition(npc, ctx.Data)
	if !found {
		return fmt.Errorf("npc with ID %d not found", npc)
	}

	var err error
	for range 5 {
		err = MoveToCoords(pos)
		if err != nil {
			continue
		}

		err = step.InteractNPC(npc)
		if err != nil {
			continue
		}
		break
	}
	if err != nil {
		return err
	}

	event.Send(event.InteractedTo(event.Text(ctx.Name, ""), int(npc), event.InteractionTypeNPC))

	return nil
}

func InteractObject(o data.Object, isCompletedFn func() bool) error {
	ctx := context.Get()
	ctx.SetLastAction("InteractObject")

	// Track starting area to detect transitions (e.g., portals)
	startingArea := ctx.Data.PlayerUnit.Area

	ctx.Logger.Debug("InteractObject called",
		"object", o.Name,
		"startArea", startingArea)

	pos := o.Position
	distFinish := step.DistanceToFinishMoving
	if ctx.Data.PlayerUnit.Area == area.RiverOfFlame && o.IsWaypoint() {
		pos = data.Position{X: 7800, Y: 5919}
		o.ID = 0
		// Special case for seals: we cant teleport directly to center. Interaction range is bigger then DistanceToFinishMoving so we modify it
	} else if strings.Contains(o.Desc().Name, "Seal") {
		distFinish = 10
	}

	var err error
	for range 5 {
		// Ensure collision data is loaded before attempting to move
		// This prevents "path couldn't be calculated" errors when area has changed but collision grid isn't ready
		if ctx.Data.AreaData.Grid == nil ||
			ctx.Data.AreaData.Grid.CollisionGrid == nil ||
			len(ctx.Data.AreaData.Grid.CollisionGrid) == 0 {
			ctx.Logger.Debug("Waiting for collision grid to load before InteractObject movement",
				"object", o.Name,
				"area", ctx.Data.PlayerUnit.Area)
			utils.Sleep(200)
			ctx.RefreshGameData()
			continue
		}

		// For waypoints, check if we should use telekinesis
		if o.IsWaypoint() {
			// Check if telekinesis is enabled and available
			useTelekinesis := false
			switch ctx.CharacterCfg.Character.Class {
			case "sorceress":
				useTelekinesis = ctx.CharacterCfg.Character.BlizzardSorceress.UseTelekinesis
			case "nova":
				useTelekinesis = ctx.CharacterCfg.Character.NovaSorceress.UseTelekinesis
			case "lightsorc":
				useTelekinesis = ctx.CharacterCfg.Character.LightningSorceress.UseTelekinesis
			case "hydraorb":
				useTelekinesis = ctx.CharacterCfg.Character.HydraOrbSorceress.UseTelekinesis
			case "fireballsorc":
				useTelekinesis = ctx.CharacterCfg.Character.FireballSorceress.UseTelekinesis
			case "sorceress_leveling":
				useTelekinesis = ctx.CharacterCfg.Character.SorceressLeveling.UseTelekinesis
			}

			canUseTK := useTelekinesis && ctx.Data.AreaData.Area.IsTown()

			if canUseTK {
				// Only move if distance is greater than 21 (telekinesis max range)
				// Otherwise stay where we are and use telekinesis from current position
				distance := ctx.PathFinder.DistanceFromMe(pos)
				if distance > 21 {
					// Too far, move closer to 15 units (optimal TK range)
					err = step.MoveTo(pos, step.WithDistanceToFinish(15), step.WithIgnoreMonsters())
					if err != nil {
						continue
					}
				}
				// If distance <= 21, don't move at all - use telekinesis from current position
			} else if !ctx.Data.AreaData.Area.IsTown() {
				// Original behavior for non-town waypoints without TK - move directly to waypoint
				err = MoveToCoords(pos)
				if err != nil {
					continue
				}
			} else {
				// Town waypoints without TK - use default distance
				err = step.MoveTo(pos, step.WithDistanceToFinish(distFinish), step.WithIgnoreMonsters())
				if err != nil {
					continue
				}
			}
		} else {
			err = step.MoveTo(pos, step.WithDistanceToFinish(distFinish), step.WithIgnoreMonsters())
			if err != nil {
				continue
			}
		}

		err = step.InteractObject(o, isCompletedFn)
		if err != nil {
			continue
		}
		break
	}

	if err != nil {
		ctx.Logger.Debug("InteractObject step.InteractObject returned error",
			"object", o.Name,
			"error", err)
		return err
	}

	// Refresh game data to get the final area state after interaction
	ctx.RefreshGameData()

	ctx.Logger.Debug("InteractObject step.InteractObject succeeded",
		"object", o.Name,
		"startArea", startingArea,
		"currentArea", ctx.Data.PlayerUnit.Area)

	// If we transitioned to a new area (portal interaction), ensure collision data is loaded
	if ctx.Data.PlayerUnit.Area != startingArea {
		ctx.Logger.Debug("Area transition detected via portal, waiting for collision data",
			"from", startingArea,
			"to", ctx.Data.PlayerUnit.Area)

		// Initial delay to allow server to fully sync area data
		utils.Sleep(500)
		ctx.RefreshGameData()

		// Wait up to 3 seconds for collision grid to load and be valid
		deadline := time.Now().Add(3 * time.Second)
		gridLoaded := false
		for time.Now().Before(deadline) {
			ctx.RefreshGameData()

			// Verify collision grid exists, is not nil, and has valid dimensions
			if ctx.Data.AreaData.Grid != nil &&
				ctx.Data.AreaData.Grid.CollisionGrid != nil &&
				len(ctx.Data.AreaData.Grid.CollisionGrid) > 0 {
				gridLoaded = true
				ctx.Logger.Debug("Collision grid loaded successfully after portal transition",
					"area", ctx.Data.PlayerUnit.Area,
					"gridWidth", ctx.Data.AreaData.Grid.Width,
					"gridHeight", ctx.Data.AreaData.Grid.Height)
				break
			}
			utils.Sleep(100)
		}

		if !gridLoaded {
			ctx.Logger.Warn("Collision grid did not load within timeout",
				"area", ctx.Data.PlayerUnit.Area,
				"timeout", "3s")
		}
	}

	return nil
}

func InteractObjectByID(id data.UnitID, isCompletedFn func() bool) error {
	ctx := context.Get()
	ctx.SetLastAction("InteractObjectByID")

	o, found := ctx.Data.Objects.FindByID(id)
	if !found {
		return fmt.Errorf("object with ID %d not found", id)
	}

	return InteractObject(o, isCompletedFn)
}

func getNPCPosition(npc npc.ID, d *game.Data) (data.Position, bool) {
	monster, found := d.Monsters.FindOne(npc, data.MonsterTypeNone)
	if found {
		return monster.Position, true
	}

	n, found := d.NPCs.FindOne(npc)
	if !found {
		return data.Position{}, false
	}

	return data.Position{X: n.Positions[0].X, Y: n.Positions[0].Y}, true
}
