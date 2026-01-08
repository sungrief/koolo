package action

import (
	"log/slog"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/item"
	"github.com/hectorgimenez/d2go/pkg/data/skill"
	"github.com/hectorgimenez/d2go/pkg/data/stat"
	"github.com/hectorgimenez/d2go/pkg/data/state"
	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/utils"
)

// ============================================================================
// CONSTANTS
// ============================================================================

const (
	buffCooldown       = 30 * time.Second        // Minimum time between full buff sequences
	stateWaitTimeout   = 1000 * time.Millisecond // Max time to wait for buff state after cast
	stateCheckInterval = 50 * time.Millisecond   // Poll interval for state check
	maxCastRetries     = 3                       // Max retries if buff doesn't apply
	postCastBaseDelay  = 300                     // Base delay after cast (ms)
	swapDelay          = 250                     // Delay after weapon swap (ms)
)

// ============================================================================
// PUBLIC API
// ============================================================================

// BuffIfRequired conditionally applies buffs if needed and safe to do so.
func BuffIfRequired() {
	ctx := context.Get()

	if !IsRebuffRequired() || ctx.Data.PlayerUnit.Area.IsTown() {
		return
	}

	// Safety: Don't buff if too many monsters nearby
	closeMonsters := 0
	for _, m := range ctx.Data.Monsters {
		if ctx.PathFinder.DistanceFromMe(m.Position) < 15 {
			closeMonsters++
			if closeMonsters >= 2 {
				return
			}
		}
	}

	Buff()
}

// Buff executes the complete buff sequence with:
// - State verification after each buff (retry if failed)
// - Automatic weapon set selection per skill (picks set with better +skills)
// - Proper Barbarian handling (native warcries)
func Buff() {
	ctx := context.Get()
	ctx.SetLastAction("Buff")

	if ctx.Data.PlayerUnit.Area.IsTown() || time.Since(ctx.LastBuffAt) < buffCooldown {
		return
	}

	// Handle loading screen
	if ctx.Data.OpenMenus.LoadingScreen {
		ctx.Logger.Debug("Loading screen detected, waiting for game to load")
		ctx.WaitForGameToLoad()
		utils.Sleep(500)
	}

	isBarbarian := isBarbarianByClass()
	hasCTA := ctaFound(*ctx.Data)

	ctx.Logger.Debug("Starting buff sequence",
		slog.Bool("isBarbarian", isBarbarian),
		slog.Bool("hasCTA", hasCTA))

	// Ensure we start on primary weapon
	ensurePrimaryWeapon()

	// --- Phase 1: Pre-CTA Buffs ---
	castPreCTABuffs()

	// --- Phase 2: Warcries (BO/BC/Shout) ---
	if isBarbarian {
		castBarbarianWarcries()
	} else if hasCTA {
		castCTAWarcries()
	}

	// --- Phase 3: Post-CTA Class Buffs ---
	castPostCTABuffs(isBarbarian)

	// Safety: Always end on primary weapon
	ensurePrimaryWeapon()

	// Update timestamp
	ctx.LastBuffAt = time.Now()
	ctx.Logger.Debug("Buff sequence completed")
}

// IsRebuffRequired checks if any buff has expired and needs reapplication.
func IsRebuffRequired() bool {
	ctx := context.Get()
	ctx.SetLastAction("IsRebuffRequired")

	if ctx.Data.PlayerUnit.Area.IsTown() || time.Since(ctx.LastBuffAt) < buffCooldown {
		return false
	}

	// Check warcries
	isBarbarian := isBarbarianByClass()
	hasCTA := ctaFound(*ctx.Data)

	if (isBarbarian || hasCTA) &&
		(!ctx.Data.PlayerUnit.States.HasState(state.Battleorders) ||
			!ctx.Data.PlayerUnit.States.HasState(state.Battlecommand)) {
		return true
	}

	// Check class-specific buff states
	for _, buff := range ctx.Char.BuffSkills() {
		if _, found := ctx.Data.KeyBindings.KeyBindingForSkill(buff); found {
			if needsRebuff(buff) {
				return true
			}
		}
	}

	return false
}

// ============================================================================
// INTERNAL: BUFF CASTING PHASES
// ============================================================================

func castPreCTABuffs() {
	ctx := context.Get()

	preBuffs := ctx.Char.PreCTABuffSkills()
	if len(preBuffs) == 0 {
		return
	}

	// Filter conflicting buffs (e.g., Shadow Master vs Shadow Warrior)
	preBuffs = filterConflictingBuffs(preBuffs)

	ctx.Logger.Debug("Casting Pre-CTA buffs", slog.Int("count", len(preBuffs)))
	for _, buff := range preBuffs {
		if kb, found := ctx.Data.KeyBindings.KeyBindingForSkill(buff); found {
			expectedState := getStateForSkill(buff)
			castBuffWithBestWeapon(buff, kb, expectedState)
		}
	}
}

func castBarbarianWarcries() {
	ctx := context.Get()
	ctx.Logger.Debug("Casting Barbarian warcries")

	buffSkills := ctx.Char.BuffSkills()

	// Cast in optimal order: BC -> Shout -> BO
	orderedWarcries := []skill.ID{skill.BattleCommand, skill.Shout, skill.BattleOrders}

	for _, warcry := range orderedWarcries {
		if !containsSkill(buffSkills, warcry) {
			continue
		}

		kb, found := ctx.Data.KeyBindings.KeyBindingForSkill(warcry)
		if !found {
			continue
		}

		expectedState := getStateForSkill(warcry)
		castBuffWithBestWeapon(warcry, kb, expectedState)
	}
}

func castCTAWarcries() {
	ctx := context.Get()
	ctx.Logger.Debug("Casting CTA warcries (non-Barbarian)")

	// Swap to CTA
	if _, hasBO := ctx.Data.PlayerUnit.Skills[skill.BattleOrders]; !hasBO {
		step.SwapToCTA()
		utils.Sleep(swapDelay)
	}

	// Cast BC with state verification
	if kb, found := ctx.Data.KeyBindings.KeyBindingForSkill(skill.BattleCommand); found {
		castBuffWithRetry(kb, skill.BattleCommand, state.Battlecommand)
	}

	// Cast BO with state verification
	if kb, found := ctx.Data.KeyBindings.KeyBindingForSkill(skill.BattleOrders); found {
		castBuffWithRetry(kb, skill.BattleOrders, state.Battleorders)
	}

	// Swap back
	utils.Sleep(swapDelay)
	step.SwapToMainWeapon()
}

func castPostCTABuffs(isBarbarian bool) {
	ctx := context.Get()

	buffSkills := ctx.Char.BuffSkills()
	if len(buffSkills) == 0 {
		return
	}

	// Filter out warcries for Barbarians - already handled
	var classBuffs []skill.ID
	for _, buff := range buffSkills {
		if isBarbarian && isWarcrySkill(buff) {
			continue
		}
		if _, found := ctx.Data.KeyBindings.KeyBindingForSkill(buff); found {
			classBuffs = append(classBuffs, buff)
		}
	}

	if len(classBuffs) == 0 {
		return
	}

	// Filter conflicting buffs (e.g., if both Fade and BoS are in list, keep only Fade)
	classBuffs = filterConflictingBuffs(classBuffs)

	ctx.Logger.Debug("Casting class-specific buffs", slog.Int("count", len(classBuffs)))

	for _, buff := range classBuffs {
		kb, found := ctx.Data.KeyBindings.KeyBindingForSkill(buff)
		if !found {
			continue
		}

		expectedState := getStateForSkill(buff)
		castBuffWithBestWeapon(buff, kb, expectedState)
	}
}

// ============================================================================
// INTERNAL: BUFF CASTING WITH WEAPON SELECTION
// ============================================================================

// castBuffWithBestWeapon automatically selects the weapon set with better +skills,
// casts the buff with state verification, then returns to primary weapon
func castBuffWithBestWeapon(buffSkill skill.ID, kb data.KeyBinding, expectedState state.State) {
	ctx := context.Get()

	// Calculate skill bonuses on both weapon sets
	mainBonus := getSkillBonusOnWeaponSet(buffSkill, false)
	swapBonus := getSkillBonusOnWeaponSet(buffSkill, true)

	shouldSwap := swapBonus > mainBonus

	ctx.Logger.Debug("Buff weapon selection",
		slog.String("skill", buffSkill.Desc().Name),
		slog.Int("mainBonus", mainBonus),
		slog.Int("swapBonus", swapBonus),
		slog.Bool("useSwap", shouldSwap))

	// Swap to better weapon set if needed
	if shouldSwap && ctx.Data.ActiveWeaponSlot == 0 {
		swapToSecondary()
		utils.Sleep(swapDelay)
	}

	// Cast with state verification and retry
	castBuffWithRetry(kb, buffSkill, expectedState)

	// Swap back to primary if we swapped
	if shouldSwap && ctx.Data.ActiveWeaponSlot == 1 {
		utils.Sleep(swapDelay)
		swapToPrimary()
	}
}

// castBuffWithRetry casts a buff and verifies the state appeared.
// Retries up to maxCastRetries times if state doesn't appear.
func castBuffWithRetry(kb data.KeyBinding, buffSkill skill.ID, expectedState state.State) {
	ctx := context.Get()
	skillName := buffSkill.Desc().Name

	for attempt := 0; attempt <= maxCastRetries; attempt++ {
		if attempt > 0 {
			ctx.Logger.Debug("Retrying buff cast",
				slog.String("skill", skillName),
				slog.Int("attempt", attempt))
		}

		// Perform the cast
		doCast(kb)

		// If no state to verify (0 = unknown), we're done
		if expectedState == 0 {
			ctx.Logger.Debug("Buff cast (no state to verify)", slog.String("skill", skillName))
			return
		}

		// Wait for state to appear
		if waitForState(expectedState) {
			ctx.Logger.Debug("Buff applied successfully",
				slog.String("skill", skillName),
				slog.Int("attempts", attempt+1))
			return
		}

		ctx.Logger.Debug("Buff state not detected",
			slog.String("skill", skillName),
			slog.Int("attempt", attempt))
	}

	ctx.Logger.Warn("Buff failed after max retries", slog.String("skill", skillName))
}

// doCast performs the actual key press and click to cast a buff
func doCast(kb data.KeyBinding) {
	ctx := context.Get()

	utils.Sleep(100)
	ctx.HID.PressKeyBinding(kb)
	utils.Sleep(200)
	ctx.HID.Click(game.RightButton, 640, 340)
	utils.Sleep(postCastBaseDelay)
}

// waitForState waits for a specific state to appear on the player
func waitForState(st state.State) bool {
	ctx := context.Get()
	deadline := time.Now().Add(stateWaitTimeout)

	for time.Now().Before(deadline) {
		ctx.RefreshGameData()
		if ctx.Data.PlayerUnit.States.HasState(st) {
			return true
		}
		time.Sleep(stateCheckInterval)
	}

	return false
}

// ============================================================================
// INTERNAL: SKILL BONUS CALCULATION
// ============================================================================

// getSkillBonusOnWeaponSet calculates total +skill bonus for a given skill on a weapon set
func getSkillBonusOnWeaponSet(sk skill.ID, checkSwap bool) int {
	ctx := context.Get()
	d := ctx.Data
	totalBonus := 0

	// Determine which weapon slots to check
	var leftSlot, rightSlot item.LocationType
	if checkSwap {
		leftSlot, rightSlot = item.LocLeftArmSecondary, item.LocRightArmSecondary
	} else {
		leftSlot, rightSlot = item.LocLeftArm, item.LocRightArm
	}

	skillDesc := sk.Desc()
	playerClass := getPlayerClass()

	for _, itm := range d.Inventory.ByLocation(item.LocationEquipped) {
		// Only check items in target weapon slots
		if itm.Location.BodyLocation != leftSlot && itm.Location.BodyLocation != rightSlot {
			continue
		}

		// +Specific Skill (NonClassSkill)
		if skillBonus, found := itm.FindStat(stat.NonClassSkill, int(sk)); found {
			totalBonus += skillBonus.Value
		}

		// +All Skills
		if allSkillsBonus, found := itm.FindStat(stat.AllSkills, 0); found {
			totalBonus += allSkillsBonus.Value
		}

		// +Class Skills & +Skill Tab (only for native skills)
		if isSkillNativeToClass(sk, playerClass) {
			if classSkillsBonus, found := itm.FindStat(stat.AddClassSkills, int(playerClass)); found {
				totalBonus += classSkillsBonus.Value
			}

			if skillDesc.Page >= 0 && skillDesc.Page < 3 {
				tabID := int(playerClass)*3 + skillDesc.Page
				if tabBonus, found := itm.FindStat(stat.AddSkillTab, tabID); found {
					totalBonus += tabBonus.Value
				}
			}
		}
	}

	return totalBonus
}

func getPlayerClass() data.Class {
	ctx := context.Get()
	if ctx.CharacterCfg == nil {
		return data.Barbarian
	}

	class := ctx.CharacterCfg.Character.Class

	switch {
	case isClassType(class, "amazon", "javazon", "amazon_leveling"):
		return data.Amazon
	case isClassType(class, "sorceress", "blizzard", "nova", "lightsorc", "fireballsorc", "hydraorb", "sorceress_leveling"):
		return data.Sorceress
	case isClassType(class, "necromancer", "necromancer_leveling"):
		return data.Necromancer
	case isClassType(class, "paladin", "hammerdin", "foh", "smiter", "dragondin", "paladin_leveling"):
		return data.Paladin
	case isClassType(class, "barbarian", "barb", "berserker", "warcry_barb", "barb_leveling"):
		return data.Barbarian
	case isClassType(class, "druid", "winddruid", "druid_leveling"):
		return data.Druid
	case isClassType(class, "assassin", "trapsin", "mosaic", "assassin_leveling"):
		return data.Assassin
	default:
		return data.Barbarian
	}
}

func isClassType(class string, types ...string) bool {
	for _, t := range types {
		if class == t {
			return true
		}
	}
	return false
}

func isSkillNativeToClass(sk skill.ID, class data.Class) bool {
	switch class {
	case data.Amazon:
		return sk >= skill.MagicArrow && sk <= skill.LightningFury
	case data.Sorceress:
		return sk >= skill.FireBolt && sk <= skill.ColdMastery
	case data.Necromancer:
		return sk >= skill.AmplifyDamage && sk <= skill.Revive
	case data.Paladin:
		return sk >= skill.Sacrifice && sk <= skill.Salvation
	case data.Barbarian:
		return sk >= skill.Bash && sk <= skill.BattleCommand
	case data.Druid:
		return sk >= skill.Raven && sk <= skill.Hurricane
	case data.Assassin:
		return sk >= skill.FireBlast && sk <= skill.PhoenixStrike
	default:
		return false
	}
}

// ============================================================================
// INTERNAL: CONFLICTING BUFF GROUPS
// ============================================================================

// Buff groups where only one should be active at a time
// Higher index = higher priority within group
var (
	// Sorceress armors - Chilling Armor > Shiver Armor > Frozen Armor
	sorcArmorGroup = []skill.ID{skill.FrozenArmor, skill.ShiverArmor, skill.ChillingArmor}

	// Assassin shadow buffs - Fade > Burst of Speed (Fade has DR + resists)
	assassinShadowGroup = []skill.ID{skill.BurstOfSpeed, skill.Fade}

	// Druid shapeshifting - mutually exclusive
	druidShapeGroup = []skill.ID{skill.Werewolf, skill.Werebear}

	// Assassin shadows - Shadow Master > Shadow Warrior
	assassinShadowSummonGroup = []skill.ID{skill.ShadowWarrior, skill.ShadowMaster}
)

// filterConflictingBuffs removes lower priority buffs from conflicting groups
// Returns filtered list with only highest priority buff from each group
func filterConflictingBuffs(buffs []skill.ID) []skill.ID {
	result := make([]skill.ID, 0, len(buffs))

	// Track which groups we've already added a buff from
	handledGroups := make(map[*[]skill.ID]bool)

	groups := []*[]skill.ID{&sorcArmorGroup, &assassinShadowGroup, &druidShapeGroup, &assassinShadowSummonGroup}

	for _, buff := range buffs {
		belongsToGroup := false
		var buffGroup *[]skill.ID

		// Check if this buff belongs to any conflicting group
		for _, group := range groups {
			if containsSkill(*group, buff) {
				belongsToGroup = true
				buffGroup = group
				break
			}
		}

		if !belongsToGroup {
			// Not in any conflict group, keep it
			result = append(result, buff)
			continue
		}

		// Already handled this group?
		if handledGroups[buffGroup] {
			continue
		}

		// Find highest priority buff from this group that's in our list
		highestPriority := findHighestPriorityBuff(*buffGroup, buffs)
		if highestPriority != skill.AttackSkill { // AttackSkill used as "none found"
			result = append(result, highestPriority)
			handledGroups[buffGroup] = true
		}
	}

	return result
}

// findHighestPriorityBuff returns the highest priority buff from group that exists in buffs
// Group is ordered low->high priority, so we iterate backwards
func findHighestPriorityBuff(group []skill.ID, buffs []skill.ID) skill.ID {
	for i := len(group) - 1; i >= 0; i-- {
		if containsSkill(buffs, group[i]) {
			return group[i]
		}
	}
	return skill.AttackSkill // None found marker
}

// ============================================================================
// INTERNAL: STATE CHECKS
// ============================================================================

func getStateForSkill(sk skill.ID) state.State {
	switch sk {
	case skill.HolyShield:
		return state.Holyshield
	case skill.FrozenArmor:
		return state.Frozenarmor
	case skill.ShiverArmor:
		return state.Shiverarmor
	case skill.ChillingArmor:
		return state.Chillingarmor
	case skill.EnergyShield:
		return state.Energyshield
	case skill.CycloneArmor:
		return state.Cyclonearmor
	case skill.BoneArmor:
		return state.Bonearmor
	case skill.Shout:
		return state.Shout
	case skill.BattleOrders:
		return state.Battleorders
	case skill.BattleCommand:
		return state.Battlecommand
	case skill.Fade:
		return state.Fade
	case skill.BurstOfSpeed:
		return state.Quickness
	case skill.Hurricane:
		return state.Hurricane
	// Skills without reliable state detection
	case skill.ThunderStorm, skill.BladeShield, skill.Enchant,
		skill.Werewolf, skill.Werebear:
		return 0
	default:
		return 0
	}
}

// hasAnySorcArmor checks if player has any sorceress armor active
func hasAnySorcArmor() bool {
	ctx := context.Get()
	return ctx.Data.PlayerUnit.States.HasState(state.Frozenarmor) ||
		ctx.Data.PlayerUnit.States.HasState(state.Shiverarmor) ||
		ctx.Data.PlayerUnit.States.HasState(state.Chillingarmor)
}

// hasAnyShadowBuff checks if player has Fade or BoS active
func hasAnyShadowBuff() bool {
	ctx := context.Get()
	return ctx.Data.PlayerUnit.States.HasState(state.Fade) ||
		ctx.Data.PlayerUnit.States.HasState(state.Quickness)
}

func needsRebuff(sk skill.ID) bool {
	ctx := context.Get()

	switch sk {
	case skill.HolyShield:
		return !ctx.Data.PlayerUnit.States.HasState(state.Holyshield)

	// Sorceress armors - check if ANY armor is active
	// If we want CA but have Frozen, that's still OK (build should handle priority)
	case skill.FrozenArmor, skill.ShiverArmor, skill.ChillingArmor:
		return !hasAnySorcArmor()

	case skill.EnergyShield:
		return !ctx.Data.PlayerUnit.States.HasState(state.Energyshield)
	case skill.CycloneArmor:
		return !ctx.Data.PlayerUnit.States.HasState(state.Cyclonearmor)
	case skill.BoneArmor:
		return !ctx.Data.PlayerUnit.States.HasState(state.Bonearmor)
	case skill.Shout:
		return !ctx.Data.PlayerUnit.States.HasState(state.Shout)
	case skill.BattleOrders:
		return !ctx.Data.PlayerUnit.States.HasState(state.Battleorders)
	case skill.BattleCommand:
		return !ctx.Data.PlayerUnit.States.HasState(state.Battlecommand)

	// Assassin shadow buffs - check if ANY is active
	case skill.Fade, skill.BurstOfSpeed:
		return !hasAnyShadowBuff()

	// Druid shapeshift - no reliable state detection, don't trigger rebuff
	case skill.Werewolf, skill.Werebear:
		return false

	case skill.Hurricane:
		return !ctx.Data.PlayerUnit.States.HasState(state.Hurricane)

	// Skills without reliable state detection - don't trigger auto-rebuff
	// They will be recast on the 30s cooldown
	case skill.ThunderStorm, skill.BladeShield, skill.Enchant:
		return false

	// Summons - don't trigger rebuff based on state
	case skill.OakSage, skill.SpiritOfBarbs, skill.HeartOfWolverine,
		skill.ShadowWarrior, skill.ShadowMaster, skill.Raven,
		skill.SummonSpiritWolf, skill.SummonDireWolf, skill.SummonGrizzly:
		return false

	default:
		return false
	}
}

func isWarcrySkill(sk skill.ID) bool {
	return sk == skill.BattleCommand ||
		sk == skill.BattleOrders ||
		sk == skill.Shout ||
		sk == skill.WarCry ||
		sk == skill.BattleCry
}

func containsSkill(skills []skill.ID, target skill.ID) bool {
	for _, s := range skills {
		if s == target {
			return true
		}
	}
	return false
}

// ============================================================================
// INTERNAL: CLASS & CTA DETECTION
// ============================================================================

func isBarbarianByClass() bool {
	ctx := context.Get()
	if ctx.CharacterCfg == nil {
		return false
	}

	class := ctx.CharacterCfg.Character.Class
	return class == "berserker" ||
		class == "warcry_barb" ||
		class == "barb_leveling" ||
		class == "barb" ||
		class == "barbarian"
}

func ctaFound(d game.Data) bool {
	for _, itm := range d.Inventory.ByLocation(item.LocationEquipped) {
		_, boFound := itm.FindStat(stat.NonClassSkill, int(skill.BattleOrders))
		_, bcFound := itm.FindStat(stat.NonClassSkill, int(skill.BattleCommand))
		if boFound && bcFound {
			return true
		}
	}
	return false
}

// ============================================================================
// INTERNAL: WEAPON SWAP
// ============================================================================

func swapToSecondary() {
	ctx := context.Get()
	if ctx.Data.ActiveWeaponSlot == 1 {
		return
	}
	ctx.HID.PressKeyBinding(ctx.Data.KeyBindings.SwapWeapons)
}

func swapToPrimary() {
	ctx := context.Get()
	if ctx.Data.ActiveWeaponSlot == 0 {
		return
	}
	ctx.HID.PressKeyBinding(ctx.Data.KeyBindings.SwapWeapons)
}

func ensurePrimaryWeapon() {
	ctx := context.Get()
	if ctx.Data.ActiveWeaponSlot != 0 {
		ctx.Logger.Debug("Swapping to primary weapon")
		swapToPrimary()
		utils.Sleep(swapDelay)
	}
}
