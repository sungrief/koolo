package step

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/skill"
	"github.com/hectorgimenez/d2go/pkg/data/stat"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/ui"
	"github.com/hectorgimenez/koolo/internal/utils"
)

const DistanceToFinishMoving = 4

var (
	ErrMonstersInPath           = errors.New("monsters detected in movement path")
	ErrPlayerStuck              = errors.New("player is stuck")
	ErrNoPath                   = errors.New("path couldn't be calculated")
	stepLastMonsterCheck        = time.Time{}
	stepMonsterCheckInterval    = 100 * time.Millisecond
	lastDestructibleAttemptTime = time.Time{}
	objectInteractionCooldown   = 500 * time.Millisecond
	//failedToPathToShrine        = make(map[data.Position]time.Time)
)

type MoveOpts struct {
	distanceOverride      *int
	stationaryMinDistance *int
	stationaryMaxDistance *int
	ignoreShrines         bool
	ignoreMonsters        bool
}

type MoveOption func(*MoveOpts)

// WithDistanceToFinish overrides the default DistanceToFinishMoving
func WithDistanceToFinish(distance int) MoveOption {
	return func(opts *MoveOpts) {
		opts.distanceOverride = &distance
	}
}

// WithStationaryDistance configures MoveTo to stop when within a specific range of the destination.
func WithStationaryDistance(min, max int) MoveOption {
	return func(opts *MoveOpts) {
		opts.stationaryMinDistance = &min
		opts.stationaryMaxDistance = &max
	}
}

func IgnoreShrines() MoveOption {
	return func(opts *MoveOpts) {
		opts.ignoreShrines = true
	}
}

func WithIgnoreMonsters() MoveOption {
	return func(opts *MoveOpts) {
		opts.ignoreMonsters = true
	}
}

// calculateDistance returns the Euclidean distance between two positions.
/*func calculateDistance(p1, p2 data.Position) float64 {
	dx := float64(p1.X - p2.X)
	dy := float64(p1.Y - p2.Y)
	return math.Sqrt(dx*dx + dy*dy)
}*/

func MoveTo(dest data.Position, options ...MoveOption) error {
	// Initialize options
	opts := &MoveOpts{}

	// Apply any provided options
	for _, o := range options {
		o(opts)
	}

	minDistanceToFinishMoving := DistanceToFinishMoving
	if opts.distanceOverride != nil {
		minDistanceToFinishMoving = *opts.distanceOverride
	}

	ctx := context.Get()
	ctx.SetLastStep("MoveTo")

	opts.ignoreShrines = !ctx.CharacterCfg.Game.InteractWithShrines
	//timeout := time.Second * 30
	//idleThreshold := time.Second * 3
	stuckThreshold := 150 * time.Millisecond

	//idleStartTime := time.Time{}
	stuckCheckStartTime := time.Time{}

	var walkDuration time.Duration
	if !ctx.Data.AreaData.Area.IsTown() {
		walkDuration = utils.RandomDurationMs(300, 350)
	} else {
		walkDuration = utils.RandomDurationMs(500, 800)
	}

	//startedAt := time.Now()
	lastRun := time.Time{}
	previousPosition := data.Position{}
	//previousDistance := 0

	longTermIdleReferencePosition := data.Position{}
	longTermIdleStartTime := time.Time{}
	const longTermIdleThreshold = 2 * time.Minute
	const minMovementThreshold = 30
	//var shrineDestination data.Position

	for {
		ctx.PauseIfNotPriority()
		ctx.RefreshGameData()
		currentDest := dest

		//Check for Doors on path
		if !ctx.Data.CanTeleport() {
			if doorFound, doorObj := ctx.PathFinder.HasDoorBetween(ctx.Data.PlayerUnit.Position, currentDest); doorFound {
				doorToOpen := *doorObj
				if err := InteractObject(doorToOpen, func() bool {
					door, found := ctx.Data.Objects.FindByID(doorToOpen.ID)
					return found && !door.Selectable
				}); err != nil {
					return err
				}
			}
		}

		//Shrines
		/*
			if !opts.ignoreShrines && shrineDestination == (data.Position{}) && !ctx.Data.AreaData.Area.IsTown() {
				if closestShrine := findClosestShrine(); closestShrine != nil {
					if failedTime, exists := failedToPathToShrine[closestShrine.Position]; exists {
						if time.Since(failedTime) < 5*time.Minute {
							ctx.Logger.Debug("Skipping shrine as it was previously unreachable and is on cooldown.")
							shrineDestination = data.Position{}
							currentDest = dest
						} else {
							delete(failedToPathToShrine, closestShrine.Position)
							shrineDestination = closestShrine.Position
							ctx.Logger.Debug(fmt.Sprintf("MoveTo: Found shrine at %v, redirecting destination from %v", closestShrine.Position, dest))
						}
					} else {
						shrineDestination = closestShrine.Position
						ctx.Logger.Debug(fmt.Sprintf("MoveTo: Found shrine at %v, redirecting destination from %v", closestShrine.Position, dest))
					}
				}
			}

			if shrineDestination != (data.Position{}) {
				currentDest = shrineDestination
			} else {
				currentDest = dest
			}*/

		currentDistanceToDest := ctx.PathFinder.DistanceFromMe(currentDest)
		if opts.stationaryMinDistance != nil && opts.stationaryMaxDistance != nil {
			if currentDistanceToDest >= *opts.stationaryMinDistance && currentDistanceToDest <= *opts.stationaryMaxDistance {
				ctx.Logger.Debug(fmt.Sprintf("MoveTo: Reached stationary distance %d-%d (current %d)", *opts.stationaryMinDistance, *opts.stationaryMaxDistance, currentDistanceToDest))
				return nil
			}
		}

		if !ctx.Data.CanTeleport() {
			// Handle immediate obstacles in the vicinity first
			/*
					if obj, found := handleImmediateObstacles(); found {
						if !obj.Selectable {
							// Already destroyed, move on
							continue
						}
						ctx.Logger.Debug("Immediate obstacle detected, attempting to interact.", slog.String("object", obj.Desc().Name))

						if obj.IsDoor() {
							InteractObject(*obj, func() bool {
								door, found := ctx.Data.Objects.FindByID(obj.ID)
								return found && !door.Selectable
							})
						} else {
							x, y := ui.GameCoordsToScreenCords(obj.Position.X, obj.Position.Y)
							ctx.HID.Click(game.LeftButton, x, y)
						}

						time.Sleep(time.Millisecond * 50)
						continue
					}

				if time.Since(lastRun) < walkDuration {
					time.Sleep(walkDuration - time.Since(lastRun))
					continue
				}
			*/
		} else {
			if time.Since(lastRun) < ctx.Data.PlayerCastDuration() {
				time.Sleep(ctx.Data.PlayerCastDuration() - time.Since(lastRun))
				continue
			}
		}

		if !opts.ignoreMonsters && !ctx.Data.AreaData.Area.IsTown() && !ctx.Data.CanTeleport() && time.Since(stepLastMonsterCheck) > stepMonsterCheckInterval {
			stepLastMonsterCheck = time.Now()

			monsterFound := false
			clearPathDist := ctx.CharacterCfg.Character.ClearPathDist

			for _, m := range ctx.Data.Monsters.Enemies() {
				if m.Stats[stat.Life] <= 0 {
					continue
				}

				distanceToMonster := ctx.PathFinder.DistanceFromMe(m.Position)
				if distanceToMonster <= clearPathDist {
					hasDoorBetween, _ := ctx.PathFinder.HasDoorBetween(ctx.Data.PlayerUnit.Position, m.Position)
					if ctx.PathFinder.LineOfSight(ctx.Data.PlayerUnit.Position, m.Position) && !hasDoorBetween {
						monsterFound = true
						break
					}
				}
			}

			if monsterFound {
				return ErrMonstersInPath
			}
		}

		if currentDistanceToDest < minDistanceToFinishMoving {
			/*
				if shrineDestination != (data.Position{}) && shrineDestination == currentDest {
					shrineFound := false
					var shrineObject data.Object
					for _, o := range ctx.Data.Objects {
						if o.Position == shrineDestination {
							shrineObject = o
							shrineFound = true
							break
						}
					}

					if shrineFound {
						if err := interactWithShrine(&shrineObject); err != nil {
							ctx.Logger.Warn("Failed to interact with shrine", slog.Any("error", err))
						}
					}

					shrineDestination = data.Position{}
					continue
				}
			*/

			if currentDest == dest {
				return nil
			}
		}

		currentPosition := ctx.Data.PlayerUnit.Position

		if longTermIdleStartTime.IsZero() {
			longTermIdleReferencePosition = currentPosition
			longTermIdleStartTime = time.Now()
		}

		distanceFromLongTermReference := utils.CalculateDistance(longTermIdleReferencePosition, currentPosition)

		if distanceFromLongTermReference > float64(minMovementThreshold) {
			longTermIdleStartTime = time.Time{}
			ctx.Logger.Debug(fmt.Sprintf("MoveTo: Player moved significantly (%.2f units), resetting long-term idle timer.", distanceFromLongTermReference))
		} else if time.Since(longTermIdleStartTime) > longTermIdleThreshold {
			ctx.Logger.Error(fmt.Sprintf("MoveTo: Player has been idle for more than %v, quitting game.", longTermIdleThreshold))
			return errors.New("player idle for too long, quitting game")
		}

		if currentPosition == previousPosition {
			if obj, found := ctx.PathFinder.GetClosestDestructible(ctx.Data.PlayerUnit.Position); found {
				if !obj.Selectable {
					// Already destroyed, move on
					continue
				}
				ctx.Logger.Debug("Immediate obstacle detected, attempting to interact.", slog.String("object", obj.Desc().Name))

				x, y := ui.GameCoordsToScreenCords(obj.Position.X, obj.Position.Y)
				ctx.HID.Click(game.LeftButton, x, y)

				time.Sleep(time.Millisecond * 100)
			} else {
				if stuckCheckStartTime.IsZero() {
					stuckCheckStartTime = time.Now()
				} else if time.Since(stuckCheckStartTime) > stuckThreshold {
					return ErrPlayerStuck
				}
			}
		}
		/*
			if currentPosition == previousPosition {
				if stuckCheckStartTime.IsZero() {
					stuckCheckStartTime = time.Now()
				} else if time.Since(stuckCheckStartTime) > stuckThreshold {
					ctx.Logger.Debug("Bot stuck (short term), attempting micro-shuffle.")
					ctx.PathFinder.RandomMovement()
					stuckCheckStartTime = time.Time{}
					idleStartTime = time.Time{}
				} else if idleStartTime.IsZero() {
					idleStartTime = time.Now()
				} else if time.Since(idleStartTime) > idleThreshold {
					ctx.Logger.Debug("Bot stuck (long term / idle), performing random movement as fallback.")
					ctx.PathFinder.RandomMovement()
					idleStartTime = time.Time{}
					stuckCheckStartTime = time.Time{}
				}
			} else {
				idleStartTime = time.Time{}
				stuckCheckStartTime = time.Time{}
				previousPosition = currentPosition
			}
		*/

		if ctx.Data.CanTeleport() {
			if ctx.Data.PlayerUnit.RightSkill != skill.Teleport {
				ctx.HID.PressKeyBinding(ctx.Data.KeyBindings.MustKBForSkill(skill.Teleport))
			}
		} else if kb, found := ctx.Data.KeyBindings.KeyBindingForSkill(skill.Vigor); found {
			if ctx.Data.PlayerUnit.RightSkill != skill.Vigor {
				ctx.HID.PressKeyBinding(kb)
			}
		}

		path, distance, found := ctx.PathFinder.GetPath(currentDest)
		if !found {
			/*if currentDest == shrineDestination {
				ctx.Logger.Warn(fmt.Sprintf("Path to shrine at %v could not be calculated. Marking shrine as unreachable for a few minutes.", currentDest))
				failedToPathToShrine[shrineDestination] = time.Now()
				shrineDestination = data.Position{}
				return nil
			}*/
			if opts.stationaryMinDistance == nil || opts.stationaryMaxDistance == nil ||
				currentDistanceToDest < *opts.stationaryMinDistance || currentDistanceToDest > *opts.stationaryMaxDistance {
				if ctx.PathFinder.DistanceFromMe(currentDest) < minDistanceToFinishMoving+5 {
					return nil
				}
				ctx.Logger.Debug("path could not be calculated. Current area: [" + ctx.Data.PlayerUnit.Area.Area().Name + "]. Trying to path to Destination: [" + fmt.Sprintf("%d,%d", currentDest.X, currentDest.Y) + "]")
				return ErrNoPath
			}
			return nil
		}
		if distance <= minDistanceToFinishMoving || len(path) <= minDistanceToFinishMoving || len(path) == 0 {
			if currentDest == dest {
				return nil
			}
			/*if currentDest == shrineDestination {
				shrineDestination = data.Position{}
				continue
			}*/
		}

		/*if timeout > 0 && time.Since(startedAt) > timeout {
			return nil
		}*/

		lastRun = time.Now()

		/*if distance < 20 && math.Abs(float64(previousDistance-distance)) < DistanceToFinishMoving {
			minDistanceToFinishMoving += DistanceToFinishMoving
		} else if opts.distanceOverride != nil {
			minDistanceToFinishMoving = *opts.distanceOverride
		} else {
			minDistanceToFinishMoving = DistanceToFinishMoving
		}*/

		previousPosition = ctx.Data.PlayerUnit.Position
		//previousDistance = distance
		ctx.PathFinder.MoveThroughPath(path, walkDuration)
	}
}
