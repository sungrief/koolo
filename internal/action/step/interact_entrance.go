package step

import (
	"fmt"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/area"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/utils"
)

const (
	maxEntranceDistance = 6
	maxMoveRetries      = 3
)

func InteractEntrance(targetArea area.ID) error {
	ctx := context.Get()
	ctx.SetLastStep("InteractEntrance")

	// Force mouse-only for Act 3 Sewers (due to lever mechanism complexity)
	currentArea := ctx.Data.PlayerUnit.Area
	isSewerEntrance := (currentArea == area.SewersLevel1Act3 && targetArea == area.SewersLevel2Act3) ||
		(currentArea == area.KurastBazaar && targetArea == area.SewersLevel1Act3)

	if isSewerEntrance {
		ctx.Logger.Debug("Act 3 Sewers entrance detected, forcing mouse interaction for reliability")
		return InteractEntranceMouse(targetArea)
	}

	// Check if packet casting is enabled for entrance interaction
	if ctx.CharacterCfg.PacketCasting.UseForEntranceInteraction {
		ctx.Logger.Debug("Attempting entrance interaction via packet method")
		return InteractEntrancePacket(targetArea)
	}

	// Use mouse-based interaction (original implementation)
	return InteractEntranceMouse(targetArea)
}

func InteractEntranceMouse(targetArea area.ID) error {
	maxInteractionAttempts := 5
	interactionAttempts := 0
	waitingForInteraction := false
	currentMouseCoords := data.Position{}
	lastRun := time.Time{}

	// If we move the mouse to interact with an entrance, we will set this variable.
	var lastEntranceLevel data.Level

	ctx := context.Get()

	for {
		ctx.PauseIfNotPriority()

		if ctx.Data.AreaData.Area == targetArea && time.Since(lastRun) > time.Millisecond*500 && ctx.Data.AreaData.IsInside(ctx.Data.PlayerUnit.Position) {
			return nil
		}

		if interactionAttempts > maxInteractionAttempts {
			return fmt.Errorf("area %s [%d] could not be interacted", targetArea.Area().Name, targetArea)
		}

		if waitingForInteraction && time.Since(lastRun) < time.Millisecond*500 {
			continue
		}

		lastRun = time.Now()
		for _, l := range ctx.Data.AdjacentLevels {
			// It is possible to have multiple entrances to the same area (A2 sewers, A2 palace, etc)
			// Once we "select" an area and start to move the mouse to hover with it, we don't want
			// to change the area to the 2nd entrance in the same area on the next iteration.
			if l.Area == targetArea && (lastEntranceLevel == (data.Level{}) || lastEntranceLevel == l) {
				distance := ctx.PathFinder.DistanceFromMe(l.Position)
				if distance > maxEntranceDistance {
					// Try to move closer with retries
					for retry := 0; retry < maxMoveRetries; retry++ {
						if err := MoveTo(l.Position); err != nil {
							// If MoveTo fails, try direct movement
							screenX, screenY := ctx.PathFinder.GameCoordsToScreenCords(
								l.Position.X-2,
								l.Position.Y-2,
							)
							ctx.HID.Click(game.LeftButton, screenX, screenY)
							utils.Sleep(800)
							ctx.RefreshGameData()
						}

						// Check if we're close enough now
						newDistance := ctx.PathFinder.DistanceFromMe(l.Position)
						if newDistance <= maxEntranceDistance {
							break
						}

						if retry == maxMoveRetries-1 {
							return fmt.Errorf("entrance too far away (distance: %d)", distance)
						}
					}
				}

				if l.IsEntrance {
					lx, ly := ctx.PathFinder.GameCoordsToScreenCords(l.Position.X-1, l.Position.Y-1)
					if ctx.Data.HoverData.UnitType == 5 || ctx.Data.HoverData.UnitType == 2 && ctx.Data.HoverData.IsHovered {
						ctx.HID.Click(game.LeftButton, currentMouseCoords.X, currentMouseCoords.Y)
						waitingForInteraction = true
						utils.Sleep(200)
					}

					x, y := utils.Spiral(interactionAttempts)
					x = x / 3
					y = y / 3
					currentMouseCoords = data.Position{X: lx + x, Y: ly + y}
					ctx.HID.MovePointer(lx+x, ly+y)
					interactionAttempts++
					utils.Sleep(100)

					//Add a random movement logic when interaction attempts fail
					if interactionAttempts > 1 && interactionAttempts%3 == 0 {
						ctx.Logger.Debug("Failed to interact with entrance, performing random movement to reset position.")
						ctx.PathFinder.RandomMovement()
						utils.Sleep(1000)
					}

					lastEntranceLevel = l

					continue
				}

				return fmt.Errorf("area %s [%d] is not an entrance", targetArea.Area().Name, targetArea)
			}
		}
	}
}
