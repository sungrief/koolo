package run

import (
	"errors"
	"fmt"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/area"
	"github.com/hectorgimenez/d2go/pkg/data/item"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/d2go/pkg/data/object"
	"github.com/hectorgimenez/d2go/pkg/data/quest"
	"github.com/hectorgimenez/koolo/internal/action"
	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/config"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/utils"
)

type RescueCain struct {
	ctx *context.Status
}

func NewRescueCain() *RescueCain {
	return &RescueCain{
		ctx: context.Get(),
	}
}

func (rc RescueCain) Name() string {
	return string(config.RescueCainRun)
}

func (rc RescueCain) CheckConditions(parameters *RunParameters) SequencerResult {
	if IsFarmingRun(parameters) {
		return SequencerError
	}
	if rc.ctx.Data.Quests[quest.Act1TheSearchForCain].Completed() {
		return SequencerSkip
	}

	return SequencerOk
}

func (rc RescueCain) Run(parameters *RunParameters) error {
	rc.ctx.Logger.Info("Starting Rescue Cain Quest...")

	// --- Navigation to the Dark Wood and a safe zone near the Inifuss Tree ---
	err := action.WayPoint(area.RogueEncampment)
	if err != nil {
		return err
	}

	scrollInifussUnitID := 524
	scrollInifussName := "Scroll of Inifuss"

	needToGoToTristram := rc.ctx.Data.Quests[quest.Act1TheSearchForCain].HasStatus(quest.StatusInProgress2) || rc.ctx.Data.Quests[quest.Act1TheSearchForCain].HasStatus(quest.StatusInProgress3)

	infusInInventory := false
	for _, itm := range rc.ctx.Data.Inventory.ByLocation(item.LocationInventory) {
		if itm.ID == scrollInifussUnitID {
			infusInInventory = true
			break
		}
	}

	if !infusInInventory && !needToGoToTristram {
		rc.ctx.CharacterCfg.Character.ClearPathDist = 20
		if err := config.SaveSupervisorConfig(rc.ctx.CharacterCfg.ConfigFolderName, rc.ctx.CharacterCfg); err != nil {
			rc.ctx.Logger.Error("Failed to save character configuration: %s", err.Error())
		}

		err = action.WayPoint(area.DarkWood)
		if err != nil {
			return err
		}

		rc.ctx.CharacterCfg.Character.ClearPathDist = 30
		if err := config.SaveSupervisorConfig(rc.ctx.CharacterCfg.ConfigFolderName, rc.ctx.CharacterCfg); err != nil {
			rc.ctx.Logger.Error("Failed to save character configuration: %s", err.Error())
		}

		// Find the Inifuss Tree position.
		var inifussTreePos data.Position
		var foundTree bool
		for _, o := range rc.ctx.Data.Objects {
			if o.Name == object.InifussTree {
				inifussTreePos = o.Position
				foundTree = true
				break
			}
		}
		if !foundTree {
			rc.ctx.Logger.Error("InifussTree not found, aborting quest.")
			return errors.New("InifussTree not found")
		}

		// Get the player's current position.
		playerPos := rc.ctx.Data.PlayerUnit.Position

		// --- New segmented approach to clear the path to the Inifuss Tree ---
		// Start 55 units away and move closer in 10-unit increments.

		clearRadius := 20
		for distance := 55; distance > 0; distance -= 5 {
			rc.ctx.Logger.Info(fmt.Sprintf("Moving to position %d units away from the Inifuss Tree to clear the area.", distance))

			// Calculate the new position based on the current distance.
			safePos := atDistance(inifussTreePos, playerPos, distance)

			// Move to the calculated position.
			err = action.MoveToCoords(safePos)
			if err != nil {
				return err
			}

			// Clear a large area around the new position.
			rc.ctx.Logger.Info(fmt.Sprintf("Clearing a %d unit radius around the current position...", clearRadius))
			if err := action.ClearAreaAroundPlayer(clearRadius, data.MonsterAnyFilter()); err != nil {
				return err
			}
		}

		// --- End of new segmented approach ---

		err = action.MoveToCoords(inifussTreePos)
		if err != nil {
			return err
		}

		obj, found := rc.ctx.Data.Objects.FindOne(object.InifussTree)
		if !found {
			rc.ctx.Logger.Error("InifussTree not found, aborting quest.")
			return errors.New("InifussTree not found")
		}

		err = action.InteractObject(obj, func() bool {
			updatedObj, found := rc.ctx.Data.Objects.FindOne(object.InifussTree)
			return found && !updatedObj.Selectable
		})
		if err != nil {
			return fmt.Errorf("error interacting with Inifuss Tree: %w", err)
		}

	PickupLoop:
		for i := 0; i < 5; i++ {
			rc.ctx.RefreshGameData()

			foundInInv := false
			for _, itm := range rc.ctx.Data.Inventory.ByLocation(item.LocationInventory) {
				if itm.ID == scrollInifussUnitID {
					foundInInv = true
					break
				}
			}

			if foundInInv {
				rc.ctx.Logger.Info(fmt.Sprintf("%s found in inventory. Proceeding with quest.", scrollInifussName))
				break PickupLoop
			}

			// Find the scroll on the ground.
			var scrollObj data.Item
			foundOnGround := false
			for _, itm := range rc.ctx.Data.Inventory.ByLocation(item.LocationGround) {
				if itm.ID == scrollInifussUnitID {
					scrollObj = itm
					foundOnGround = true
					break
				}
			}

			if foundOnGround {
				rc.ctx.Logger.Info(fmt.Sprintf("%s found on the ground at position %v. Attempting pickup (Attempt %d)...", scrollInifussName, scrollObj.Position, i+1))

				playerPos := rc.ctx.Data.PlayerUnit.Position
				safeAwayPos := atDistance(scrollObj.Position, playerPos, -5)

				pickupAttempts := 0
				for pickupAttempts < 8 {
					rc.ctx.Logger.Debug("Moving away from scroll for a brief moment...")
					moveAwayErr := action.MoveToCoords(safeAwayPos)
					if moveAwayErr != nil {
						rc.ctx.Logger.Warn(fmt.Sprintf("Failed to move away from scroll: %v", moveAwayErr))
					}
					utils.Sleep(200)

					moveErr := action.MoveToCoords(scrollObj.Position)
					if moveErr != nil {
						rc.ctx.Logger.Error(fmt.Sprintf("Failed to move to scroll position: %v", moveErr))
						utils.Sleep(500)
						pickupAttempts++
						continue
					}

					// --- Refresh game data just before pickup attempt ---
					rc.ctx.RefreshGameData()

					pickupErr := action.ItemPickup(10)
					if pickupErr != nil {
						rc.ctx.Logger.Warn(fmt.Sprintf("Pickup attempt %d failed: %v", pickupAttempts+1, pickupErr))
						utils.Sleep(500)
						pickupAttempts++
						continue
					}

					rc.ctx.RefreshGameData()
					foundInInvAfterPickup := false
					for _, itm := range rc.ctx.Data.Inventory.ByLocation(item.LocationInventory) {
						if itm.ID == scrollInifussUnitID {
							foundInInvAfterPickup = true
							break
						}
					}
					if foundInInvAfterPickup {
						rc.ctx.Logger.Info(fmt.Sprintf("Pickup confirmed for %s after %d attempts. Proceeding.", scrollInifussName, pickupAttempts+1))
						break PickupLoop
					}
					pickupAttempts++
				}
			} else {
				rc.ctx.Logger.Debug(fmt.Sprintf("%s not found on the ground on attempt %d. Retrying.", scrollInifussName, i+1))
				utils.Sleep(1000)
			}
		}

		for _, itm := range rc.ctx.Data.Inventory.ByLocation(item.LocationInventory) {
			if itm.ID == scrollInifussUnitID {
				infusInInventory = true
				break
			}
		}
		if !infusInInventory {
			rc.ctx.Logger.Error(fmt.Sprintf("Failed to pick up %s after all attempts. Aborting current run.", scrollInifussName))
			return errors.New("failed to pick up Scroll of Inifuss")
		}

		err = action.ReturnTown()
		if err != nil {
			return err
		}
	}

	if infusInInventory {
		err = action.InteractNPC(npc.Akara)
		if err != nil {
			return err
		}

		step.CloseAllMenus()
	}

	err = NewTristram().Run(nil)
	if err != nil {
		return err
	}

	action.ReturnTown()

	return nil
}
