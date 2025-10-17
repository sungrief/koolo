// internal/action/move.go
package action

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/hectorgimenez/koolo/internal/pather"
	"github.com/hectorgimenez/koolo/internal/ui"
	"github.com/hectorgimenez/koolo/internal/utils"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/area"
	"github.com/hectorgimenez/d2go/pkg/data/object"
	"github.com/hectorgimenez/d2go/pkg/data/stat"
	"github.com/hectorgimenez/d2go/pkg/data/state"
	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/event"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/health"
)

const (
	maxAreaSyncAttempts   = 10
	areaSyncDelay         = 100 * time.Millisecond
	monsterHandleCooldown = 500 * time.Millisecond // Reduced cooldown for more immediate re-engagement
	lootAfterCombatRadius = 25                     // Define a radius for looting after combat
)

var (
	actionLastMonsterHandlingTime = time.Time{}
	failedToPathToShrine          = make(map[data.Position]time.Time)
)

var alwaysTakeShrines = []object.ShrineType{
	object.RefillShrine,
	object.HealthShrine,
	object.ManaShrine,
}

var prioritizedShrines = []struct {
	shrineType object.ShrineType
	state      state.State
}{
	{shrineType: object.ExperienceShrine, state: state.ShrineExperience},
	{shrineType: object.ManaRegenShrine, state: state.ShrineManaRegen},
	{shrineType: object.StaminaShrine, state: state.ShrineStamina},
	{shrineType: object.SkillShrine, state: state.ShrineSkill},
}

var curseBreakingShrines = []object.ShrineType{
	object.ExperienceShrine,
	object.ManaRegenShrine,
	object.StaminaShrine,
	object.SkillShrine,
	object.ArmorShrine,
	object.CombatShrine,
	object.ResistLightningShrine,
	object.ResistFireShrine,
	object.ResistColdShrine,
	object.ResistPoisonShrine,
}

// checkPlayerDeath checks if the player is dead and returns ErrDied if so.
func checkPlayerDeath(ctx *context.Status) error {
	if ctx.Data.PlayerUnit.HPPercent() <= 0 {
		return health.ErrDied
	}
	return nil
}

func ensureAreaSync(ctx *context.Status, expectedArea area.ID) error {
	// Skip sync check if we're already in the expected area and have valid area data
	if ctx.Data.PlayerUnit.Area == expectedArea {
		return nil
	}

	// Wait for area data to sync
	for attempts := 0; attempts < maxAreaSyncAttempts; attempts++ {
		ctx.RefreshGameData()

		// Check for death during area sync
		if err := checkPlayerDeath(ctx); err != nil {
			return err
		}

		if ctx.Data.PlayerUnit.Area == expectedArea {
			return nil
		}

		time.Sleep(areaSyncDelay)
	}

	return fmt.Errorf("area sync timeout - expected: %v, current: %v", expectedArea, ctx.Data.PlayerUnit.Area)
}

func MoveToArea(dst area.ID) error {
	ctx := context.Get()
	ctx.SetLastAction("MoveToArea")

	// Proactive death check at the start of the action
	if err := checkPlayerDeath(ctx); err != nil {
		return err
	}

	if err := ensureAreaSync(ctx, ctx.Data.PlayerUnit.Area); err != nil {
		return err
	}

	// Exceptions for:
	// Arcane Sanctuary
	if dst == area.ArcaneSanctuary && ctx.Data.PlayerUnit.Area == area.PalaceCellarLevel3 {
		ctx.Logger.Debug("Arcane Sanctuary detected, finding the Portal")
		portal, _ := ctx.Data.Objects.FindOne(object.ArcaneSanctuaryPortal)
		MoveToCoords(portal.Position)

		return step.InteractObject(portal, func() bool {
			return ctx.Data.PlayerUnit.Area == area.ArcaneSanctuary
		})
	}
	// Canyon of the Magi
	if dst == area.CanyonOfTheMagi && ctx.Data.PlayerUnit.Area == area.ArcaneSanctuary {
		ctx.Logger.Debug("Canyon of the Magi detected, finding the Portal")
		tome, _ := ctx.Data.Objects.FindOne(object.YetAnotherTome)
		MoveToCoords(tome.Position)
		InteractObject(tome, func() bool {
			if _, found := ctx.Data.Objects.FindOne(object.PermanentTownPortal); found {
				ctx.Logger.Debug("Opening YetAnotherTome!")
				return true
			}
			return false
		})
		ctx.Logger.Debug("Using Canyon of the Magi Portal")
		portal, _ := ctx.Data.Objects.FindOne(object.PermanentTownPortal)
		MoveToCoords(portal.Position)
		return step.InteractObject(portal, func() bool {
			return ctx.Data.PlayerUnit.Area == area.CanyonOfTheMagi
		})
	}

	lvl := data.Level{}
	for _, a := range ctx.Data.AdjacentLevels {
		if a.Area == dst {
			lvl = a
			break
		}
	}

	if lvl.Position.X == 0 && lvl.Position.Y == 0 {
		return fmt.Errorf("destination area not found: %s", dst.Area().Name)
	}

	toFun := func() (data.Position, bool) {
		// Check for death during movement target evaluation
		if err := checkPlayerDeath(ctx); err != nil {
			return data.Position{}, false // Signal to stop moving if dead
		}

		if ctx.Data.PlayerUnit.Area == dst {
			ctx.Logger.Debug("Reached area", slog.String("area", dst.Area().Name))
			return data.Position{}, false
		}

		if ctx.Data.PlayerUnit.Area == area.TamoeHighland && dst == area.MonasteryGate {
			ctx.Logger.Debug("Monastery Gate detected, moving to static coords")
			return data.Position{X: 15139, Y: 5056}, true
		}

		if ctx.Data.PlayerUnit.Area == area.MonasteryGate && dst == area.TamoeHighland {
			ctx.Logger.Debug("Monastery Gate detected, moving to static coords")
			return data.Position{X: 15142, Y: 5118}, true
		}

		// To correctly detect the two possible exits from Lut Gholein
		if dst == area.RockyWaste && ctx.Data.PlayerUnit.Area == area.LutGholein {
			if _, _, found := ctx.PathFinder.GetPath(data.Position{X: 5004, Y: 5065}); found {
				return data.Position{X: 4989, Y: 5063}, true
			} else {
				return data.Position{X: 5096, Y: 4997}, true
			}
		}

		// This means it's a cave, we don't want to load the map, just find the entrance and interact
		if lvl.IsEntrance {
			return lvl.Position, true
		}

		objects := ctx.Data.Areas[lvl.Area].Objects
		// Sort objects by the distance from me
		sort.Slice(objects, func(i, j int) bool {
			distanceI := ctx.PathFinder.DistanceFromMe(objects[i].Position)
			distanceJ := ctx.PathFinder.DistanceFromMe(objects[j].Position)

			return distanceI < distanceJ
		})

		// Let's try to find any random object to use as a destination point, once we enter the level we will exit this flow
		for _, obj := range objects {
			_, _, found := ctx.PathFinder.GetPath(obj.Position)
			if found {
				return obj.Position, true
			}
		}

		return lvl.Position, true
	}

	var err error

	// Areas that require a distance override for proper entrance interaction (Tower, Harem, Sewers)
	if dst == area.HaremLevel1 && ctx.Data.PlayerUnit.Area == area.LutGholein ||
		dst == area.SewersLevel3Act2 && ctx.Data.PlayerUnit.Area == area.SewersLevel2Act2 ||
		dst == area.TowerCellarLevel1 && ctx.Data.PlayerUnit.Area == area.ForgottenTower ||
		dst == area.TowerCellarLevel2 && ctx.Data.PlayerUnit.Area == area.TowerCellarLevel1 ||
		dst == area.TowerCellarLevel3 && ctx.Data.PlayerUnit.Area == area.TowerCellarLevel2 ||
		dst == area.TowerCellarLevel4 && ctx.Data.PlayerUnit.Area == area.TowerCellarLevel3 ||
		dst == area.TowerCellarLevel5 && ctx.Data.PlayerUnit.Area == area.TowerCellarLevel4 {

		// Use a custom loop to integrate the distance override with monster handling.
		entrancePosition, _ := toFun()

		for {
			moveErr := step.MoveTo(entrancePosition, step.WithDistanceToFinish(7))

			if moveErr != nil {
				if errors.Is(moveErr, step.ErrMonstersInPath) {
					// RE-INTRODUCING COMBAT LOGIC FROM MoveTo(toFun)
					clearPathDist := ctx.CharacterCfg.Character.ClearPathDist
					ctx.Logger.Debug("Monster detected while using distance override. Engaging.")

					if time.Since(actionLastMonsterHandlingTime) > monsterHandleCooldown {
						actionLastMonsterHandlingTime = time.Now()
						_ = ClearAreaAroundPosition(ctx.Data.PlayerUnit.Position, clearPathDist, data.MonsterAnyFilter())

						lootErr := ItemPickup(lootAfterCombatRadius)
						if lootErr != nil {
							ctx.Logger.Warn("Error picking up items after combat (Tower/Harem/Sewers)", slog.String("error", lootErr.Error()))
						}
					}
					continue
				}
				// Handle other errors (like pathfinding failure or death)
				err = moveErr
				break
			}
			err = nil
			break
		}
	} else {
		err = MoveTo(toFun)
	}

	if err != nil {
		if errors.Is(err, health.ErrDied) { // Propagate death error
			return err
		}
		ctx.Logger.Warn("error moving to area, will try to continue", slog.String("error", err.Error()))
	}

	if lvl.IsEntrance {
		maxAttempts := 3
		for attempt := 0; attempt < maxAttempts; attempt++ {
			// Check current distance
			currentDistance := ctx.PathFinder.DistanceFromMe(lvl.Position)

			if currentDistance > 7 {
				// For distances > 7, recursively call MoveToArea as it includes the entrance interaction
				return MoveToArea(dst)
			} else if currentDistance > 3 && currentDistance <= 7 {
				// For distances between 4 and 7, use direct click
				screenX, screenY := ctx.PathFinder.GameCoordsToScreenCords(
					lvl.Position.X-2,
					lvl.Position.Y-2,
				)
				ctx.HID.Click(game.LeftButton, screenX, screenY)
				utils.Sleep(800)
			}

			// Proactive death check before interacting with entrance
			if err := checkPlayerDeath(ctx); err != nil {
				return err
			}

			// Try to interact with the entrance
			err = step.InteractEntrance(dst)
			if err == nil {
				break
			}

			if attempt < maxAttempts-1 {
				ctx.Logger.Debug("Entrance interaction failed, retrying",
					slog.Int("attempt", attempt+1),
					slog.String("error", err.Error()))
				utils.Sleep(1000)
			}
		}

		if err != nil {
			return fmt.Errorf("failed to interact with area %s after %d attempts: %v", dst.Area().Name, maxAttempts, err)
		}

		// Wait for area transition to complete
		if err := ensureAreaSync(ctx, dst); err != nil {
			return err
		}
	}

	event.Send(event.InteractedTo(event.Text(ctx.Name, ""), int(dst), event.InteractionTypeEntrance))
	return nil
}

func MoveToCoords(to data.Position) error {
	ctx := context.Get()

	// Proactive death check at the start of the action
	if err := checkPlayerDeath(ctx); err != nil {
		return err
	}

	if err := ensureAreaSync(ctx, ctx.Data.PlayerUnit.Area); err != nil {
		return err
	}

	return MoveTo(func() (data.Position, bool) {
		return to, true
	})
}

func onSafeNavigation() {
	ctx := context.Get()

	if _, isLevelingChar := ctx.Char.(context.LevelingCharacter); isLevelingChar {
		statPoints, hasUnusedPoints := ctx.Data.PlayerUnit.FindStat(stat.StatPoints, 0)
		if hasUnusedPoints && statPoints.Value > 0 {
			EnsureSkillPoints()
			EnsureStatPoints()
			EnsureSkillBindings()
		}
	}
}

func getPathOffsets(to data.Position) (int, int) {
	ctx := context.Get()

	minOffsetX := ctx.Data.AreaData.OffsetX
	minOffsetY := ctx.Data.AreaData.OffsetY

	if !ctx.Data.AreaData.IsInside(to) {
		for _, otherArea := range ctx.Data.AreaData.AdjacentLevels {
			destination := ctx.Data.Areas[otherArea.Area]
			if destination.IsInside(to) {
				minOffsetX = min(minOffsetX, destination.OffsetX)
				minOffsetY = min(minOffsetY, destination.OffsetY)
			}
		}
	}

	return minOffsetX, minOffsetY
}

func MoveTo(toFunc func() (data.Position, bool)) error {
	ctx := context.Get()
	ctx.SetLastAction("MoveTo")

	// Proactive death check at the start of the action
	if err := checkPlayerDeath(ctx); err != nil {
		return err
	}

	// Ensure no menus are open that might block movement
	for ctx.Data.OpenMenus.IsMenuOpen() {
		ctx.Logger.Debug("Found open menus while moving, closing them...")
		if err := step.CloseAllMenus(); err != nil {
			return err
		}

		utils.Sleep(500)
	}

	//lastMovement := false
	clearPathDist := ctx.CharacterCfg.Character.ClearPathDist // Get this once
	ignoreShrines := !ctx.CharacterCfg.Game.InteractWithShrines
	var pathOffsetX int
	var pathOffsetY int
	failedToPathToShrine = map[data.Position]time.Time{}
	var targetPosition data.Position
	var previousTargetPosition data.Position
	var shrine data.Object
	var door data.Object
	var chest data.Object
	var path pather.Path
	var pathDistance int
	var pathFound bool

	// Initial sync check
	if err := ensureAreaSync(ctx, ctx.Data.PlayerUnit.Area); err != nil {
		return err
	}

	for {
		ctx.PauseIfNotPriority()
		ctx.RefreshGameData()
		// Check for death after refreshing game data in the loop
		if err := checkPlayerDeath(ctx); err != nil {
			return err
		}

		to, found := toFunc()
		if !found {
			// This covers the case where toFunc itself might return false due to death
			return nil
		}

		targetPosition = to

		if !ctx.Data.AreaData.Area.IsTown() {
			//Safety first
			if time.Since(actionLastMonsterHandlingTime) > monsterHandleCooldown {
				actionLastMonsterHandlingTime = time.Now()
				_ = ClearAreaAroundPosition(ctx.Data.PlayerUnit.Position, clearPathDist, data.MonsterAnyFilter())
				// After clearing, immediately try to pick up items
				lootErr := ItemPickup(lootAfterCombatRadius)
				if lootErr != nil {
					ctx.Logger.Warn("Error picking up items after combat (teleporter)", slog.String("error", lootErr.Error()))
				}
			}

			//Check shrine nearby
			if !ignoreShrines && shrine.ID == 0 {
				if closestShrine := findClosestShrine(50.0); closestShrine != nil {
					shrine = *closestShrine
					ctx.Logger.Debug(fmt.Sprintf("MoveTo: Found shrine at %v, redirecting destination from %v", closestShrine.Position, targetPosition))

					chest = (data.Object{})
				}
			}

			//Check chests nearby
			if ctx.CharacterCfg.Game.InteractWithChests && shrine.ID == 0 && chest.ID == 0 {
				if closestChest, chestFound := ctx.PathFinder.GetClosestChest(ctx.Data.PlayerUnit.Position); chestFound {
					chest = *closestChest
					targetPosition = chest.Position
					ctx.Logger.Debug(fmt.Sprintf("MoveTo: Found chest at %v, redirecting destination from %v", chest.Position, targetPosition))
				}
			}

			if enemyFound, _ := IsAnyEnemyAroundPlayer(max(clearPathDist*2, 30)); !enemyFound {
				onSafeNavigation()
			}
		}

		if shrine.ID != 0 {
			targetPosition = shrine.Position
		} else if chest.ID != 0 {
			targetPosition = chest.Position
		}

		//Only recompute path if needed
		if !utils.IsSamePosition(previousTargetPosition, targetPosition) {
			previousTargetPosition = targetPosition
			path, pathDistance, pathFound = ctx.PathFinder.GetPath(targetPosition)
			pathOffsetX, pathOffsetY = getPathOffsets(targetPosition)
		} else {
			pathDistance = ctx.PathFinder.DistanceFromMe(targetPosition)
		}

		if !pathFound {
			return errors.New("path could not be calculated. Current area: [" + ctx.Data.PlayerUnit.Area.Area().Name + "]. Trying to path to Destination: [" + fmt.Sprintf("%d,%d", to.X, to.Y) + "]")
		}

		if pathDistance <= step.DistanceToFinishMoving {
			if shrine.ID != 0 && targetPosition == shrine.Position {
				if err := InteractObject(shrine, func() bool {
					obj, found := ctx.Data.Objects.FindByID(shrine.ID)
					return found && !obj.Selectable
				}); err != nil {
					ctx.Logger.Warn("Failed to interact with shrine", slog.Any("error", err))
				}
				shrine = data.Object{}
				continue
			} else if door.ID != 0 && targetPosition == door.Position {
				if err := InteractObject(door, func() bool {
					obj, found := ctx.Data.Objects.FindByID(door.ID)
					return found && !obj.Selectable
				}); err != nil {
					ctx.Logger.Warn("Failed to interact with door", slog.Any("error", err))
				}
				door = data.Object{}
				continue
			} else if chest.ID != 0 && targetPosition == chest.Position {
				if err := InteractObject(chest, func() bool {
					obj, found := ctx.Data.Objects.FindByID(chest.ID)
					return found && !obj.Selectable
				}); err != nil {
					ctx.Logger.Warn("Failed to interact with chest", slog.Any("error", err))
				}
				lootErr := ItemPickup(lootAfterCombatRadius)
				if lootErr != nil {
					ctx.Logger.Warn("Error picking up items after chest opening", slog.String("error", lootErr.Error()))
				}
				chest = data.Object{}
				continue
			}

			return nil
		}

		nextPosition := targetPosition
		pathStep := 0
		if !ctx.Data.AreaData.Area.IsTown() {
			maxPathStep := 30
			//Restrict path step when walking
			if !ctx.Data.CanTeleport() {
				maxPathStep = 8
			}
			pathStep = min(maxPathStep, len(path)-1)
			nextPathPos := path[pathStep]
			nextPosition = utils.PositionAddCoords(nextPathPos, pathOffsetX, pathOffsetY)
		}

		moveErr := step.MoveTo(nextPosition)
		if moveErr != nil {
			if errors.Is(moveErr, step.ErrMonstersInPath) {
				previousTargetPosition = (data.Position{})
				continue
			} else if errors.Is(moveErr, step.ErrPlayerStuck) {
				previousTargetPosition = (data.Position{})
				ctx.PathFinder.RandomMovement()
				time.Sleep(time.Millisecond * 200)
				continue
			} else if errors.Is(moveErr, step.ErrNoPath) && pathStep > 0 {
				ctx.PathFinder.RandomMovement()
				time.Sleep(time.Millisecond * 200)
				path = path[pathStep:]
				continue
			}
			return moveErr
		}
		if !ctx.Data.AreaData.Area.IsTown() {
			path = path[pathStep:]
		}
		//Check for Doors on path
		/*
			if !ctx.Data.CanTeleport() {
				if doorFound, doorObj := ctx.PathFinder.HasDoorBetween(ctx.Data.PlayerUnit.Position, targetPosition); doorFound {
					door = *doorObj
					shrine = data.Object{}
					chest = data.Object{}
					ctx.Logger.Debug(fmt.Sprintf("MoveTo: Found door at %v, redirecting destination from %v", door.Position, targetPosition))
					targetPosition = door.Position
					if doorInceptionFound, _ := ctx.PathFinder.HasDoorBetween(ctx.Data.PlayerUnit.Position, targetPosition); doorInceptionFound {
						ctx.Logger.Error("Door Inception !")
					}
				}
			}
		*/

		//moveErr := step.MoveTo(targetPosition)
		/*if moveErr != nil {
			if errors.Is(moveErr, step.ErrMonstersInPath) {
				ctx.Logger.Debug("Teleporting character encountered monsters in path. Engaging.")

					if time.Since(actionLastMonsterHandlingTime) > monsterHandleCooldown {
						actionLastMonsterHandlingTime = time.Now()
						_ = ClearAreaAroundPosition(ctx.Data.PlayerUnit.Position, clearPathDist, data.MonsterAnyFilter())
						// After clearing, immediately try to pick up items
						lootErr := ItemPickup(lootAfterCombatRadius)
						if lootErr != nil {
							ctx.Logger.Warn("Error picking up items after combat (teleporter)", slog.String("error", lootErr.Error()))
						}
					}

				continue
			} else if errors.Is(moveErr, step.ErrPlayerStuck) {
				if obj, found := ctx.PathFinder.GetClosestDestructible(ctx.Data.PlayerUnit.Position); found {
					if !obj.Selectable {
						// Already destroyed, move on
						continue
					}
					ctx.Logger.Debug("Immediate obstacle detected, attempting to interact.", slog.String("object", obj.Desc().Name))

					x, y := ui.GameCoordsToScreenCords(obj.Position.X, obj.Position.Y)
					ctx.HID.Click(game.LeftButton, x, y)

					time.Sleep(time.Millisecond * 100)
				} else if closeDoor, found := ctx.PathFinder.GetClosestDoor(ctx.Data.PlayerUnit.Position); found {
					InteractObject(*closeDoor, func() bool {
						door, found := ctx.Data.Objects.FindByID(closeDoor.ID)
						return found && !door.Selectable
					})
				} else {
					ctx.PathFinder.RandomMovement()
					time.Sleep(time.Millisecond * 200)
				}
			}
			return moveErr
		}*/

		/*if lastMovement {
			return nil
		}*/

		//_, distance, _ := ctx.PathFinder.GetPathFrom(ctx.Data.PlayerUnit.Position, targetPosition)
		// Are we close enough to destination
		/*if distance <= step.DistanceToFinishMoving {
			if shrine.ID != 0 && targetPosition == shrine.Position {
				if err := interactWithShrine(&shrine); err != nil {
					ctx.Logger.Warn("Failed to interact with shrine", slog.Any("error", err))
				}
				shrine = data.Object{}
				continue
			} else if door.ID != 0 && targetPosition == door.Position {
				if err := InteractObject(door, func() bool {
					door, found := ctx.Data.Objects.FindByID(door.ID)
					return found && !door.Selectable
				}); err != nil {
					ctx.Logger.Warn("Failed to interact with door", slog.Any("error", err))
				}
				door = data.Object{}
				continue
			} else if chest.ID != 0 && targetPosition == chest.Position {
				if err := InteractObject(chest, func() bool {
					chest, found := ctx.Data.Objects.FindByID(chest.ID)
					return found && !chest.Selectable
				}); err != nil {
					ctx.Logger.Warn("Failed to interact with chest", slog.Any("error", err))
				}
				lootErr := ItemPickup(lootAfterCombatRadius)
				if lootErr != nil {
					ctx.Logger.Warn("Error picking up items after combat (teleporter)", slog.String("error", lootErr.Error()))
				}
				chest = data.Object{}
				continue
			}

			lastMovement = true
		}*/
	}
}

func findClosestShrine(maxScanDistance float64) *data.Object {
	ctx := context.Get()

	// Check if the bot is dead or chickened before proceeding.
	if ctx.Data.PlayerUnit.HPPercent() <= 0 || ctx.Data.PlayerUnit.HPPercent() <= ctx.Data.CharacterCfg.Health.ChickenAt || ctx.Data.AreaData.Area.IsTown() || ctx.Data.AreaData.Area == area.TowerCellarLevel5 {
		ctx.Logger.Debug("Bot is dead or chickened, skipping shrine search.")
		return nil
	}

	if ctx.Data.PlayerUnit.States.HasState(state.Amplifydamage) ||
		ctx.Data.PlayerUnit.States.HasState(state.Lowerresist) ||
		ctx.Data.PlayerUnit.States.HasState(state.Decrepify) {

		ctx.Logger.Debug("Curse detected on player. Prioritizing finding any shrine to break it.")

		var closestCurseBreakingShrine *data.Object
		minDistance := maxScanDistance

		for _, o := range ctx.Data.Objects {
			if o.IsShrine() && o.Selectable {
				for _, sType := range curseBreakingShrines {
					if o.Shrine.ShrineType == sType {
						distance := float64(ctx.PathFinder.DistanceFromMe(o.Position))
						if distance < minDistance {
							minDistance = distance
							closestCurseBreakingShrine = &o
						}
					}
				}
			}
		}
		if closestCurseBreakingShrine != nil {
			return closestCurseBreakingShrine
		}
	}

	var closestAlwaysTakeShrine *data.Object
	minDistance := maxScanDistance
	for _, o := range ctx.Data.Objects {
		if o.IsShrine() && o.Selectable {
			for _, sType := range alwaysTakeShrines {
				if o.Shrine.ShrineType == sType {
					if sType == object.HealthShrine && ctx.Data.PlayerUnit.HPPercent() > 95 {
						continue
					}
					if sType == object.ManaShrine && ctx.Data.PlayerUnit.MPPercent() > 95 {
						continue
					}
					if sType == object.RefillShrine && ctx.Data.PlayerUnit.HPPercent() > 95 && ctx.Data.PlayerUnit.MPPercent() > 95 {
						continue
					}

					distance := float64(ctx.PathFinder.DistanceFromMe(o.Position))
					if distance < minDistance {
						minDistance = distance
						closestAlwaysTakeShrine = &o
					}
				}
			}
		}
	}

	if closestAlwaysTakeShrine != nil {
		return closestAlwaysTakeShrine
	}

	var currentPriorityIndex int = -1

	for i, p := range prioritizedShrines {
		if ctx.Data.PlayerUnit.States.HasState(p.state) {
			currentPriorityIndex = i
			break
		}
	}

	for _, o := range ctx.Data.Objects {
		if o.IsShrine() && o.Selectable {
			shrinePriorityIndex := -1
			for i, p := range prioritizedShrines {
				if o.Shrine.ShrineType == p.shrineType {
					shrinePriorityIndex = i
					break
				}
			}

			if shrinePriorityIndex != -1 && (currentPriorityIndex == -1 || shrinePriorityIndex <= currentPriorityIndex) {
				distance := float64(ctx.PathFinder.DistanceFromMe(o.Position))
				if distance < minDistance {
					minDistance = distance
					closestShrine := &o
					return closestShrine
				}
			}
		}
	}

	return nil
}

func interactWithShrine(shrine *data.Object) error {
	ctx := context.Get()
	ctx.Logger.Debug(fmt.Sprintf("Shrine [%s] found. Interacting with it...", shrine.Desc().Name))

	attempts := 0
	maxAttempts := 3

	for {
		ctx.RefreshGameData()
		s, found := ctx.Data.Objects.FindByID(shrine.ID)

		if !found || !s.Selectable {
			ctx.Logger.Debug("Shrine successfully activated.")
			return nil
		}

		if attempts >= maxAttempts {
			ctx.Logger.Warn(fmt.Sprintf("Failed to activate shrine [%s] after multiple attempts. Moving on.", shrine.Desc().Name))
			return fmt.Errorf("failed to activate shrine [%s] after multiple attempts", shrine.Desc().Name)
		}

		x, y := ui.GameCoordsToScreenCords(s.Position.X, s.Position.Y)
		ctx.HID.Click(game.LeftButton, x, y)
		attempts++
		utils.Sleep(100)
	}
}
