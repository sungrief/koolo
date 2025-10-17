package character

import (
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/item"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/d2go/pkg/data/skill"
	"github.com/hectorgimenez/d2go/pkg/data/stat"
	"github.com/hectorgimenez/koolo/internal/action"
	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/pather"
)

const (
	maxAmazonLevelingAttackLoops = 10
	minAmazonLevelingDistance    = 10
	maxAmazonLevelingDistance    = 30
)

type AmazonLeveling struct {
	BaseCharacter
}

var _ context.LevelingCharacter = (*AmazonLeveling)(nil)

func (s AmazonLeveling) CheckKeyBindings() []skill.ID {
	return []skill.ID{}
}

func (s AmazonLeveling) KillMonsterSequence(
	monsterSelector func(d game.Data) (data.UnitID, bool),
	skipOnImmunities []stat.Resist,
) error {
	completedAttackLoops := 0
	previousUnitID := 0
	var lastThrowTime time.Time
	const numOfLightningFuries = 3
	const timeBetweenPoisonJavs = 5

	for {
		id, found := monsterSelector(*s.Data)
		if !found {
			return nil
		}
		if previousUnitID != int(id) {
			completedAttackLoops = 0
		}

		if !s.preBattleChecks(id, skipOnImmunities) {
			return nil
		}

		monster, found := s.Data.Monsters.FindByID(id)
		if !found {
			s.Logger.Info("Monster not found", slog.String("monster", fmt.Sprintf("%v", monster)))
			return nil
		}

		if completedAttackLoops >= maxAmazonLevelingAttackLoops {
			if !game.IsActBoss(monster) && !game.IsQuestEnemy(monster) {
				return nil
			} else {
				completedAttackLoops = 0
			}
		}

		closeMonsters := 0
		for _, mob := range s.Data.Monsters {
			if mob.IsPet() || mob.IsMerc() || mob.IsGoodNPC() || mob.IsSkip() || monster.Stats[stat.Life] <= 0 && mob.UnitID != monster.UnitID {
				continue
			}
			if pather.DistanceFromPoint(mob.Position, monster.Position) <= 15 {
				closeMonsters++
			}
			if closeMonsters >= 3 {
				break
			}
		}

		canRangeAttack := s.getRemainingThrowables() > 5
		rangedAttack := false
		if canRangeAttack {
			if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.LightningFury); found {
				if closeMonsters >= 3 {
					if step.SecondaryAttack(skill.LightningFury, id, numOfLightningFuries, step.Distance(minAmazonLevelingDistance, maxAmazonLevelingDistance)) == nil {
						rangedAttack = true
					}
				}
			} else if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.PoisonJavelin); found {
				if time.Since(lastThrowTime) > timeBetweenPoisonJavs*time.Second {
					if closeMonsters >= 3 {
						if step.SecondaryAttack(skill.PoisonJavelin, id, 1, step.Distance(minAmazonLevelingDistance, maxAmazonLevelingDistance)) == nil {
							rangedAttack = true
							lastThrowTime = time.Now()
						}
					}
				}
			}
		} else {
			//Back to town ?
		}

		if !rangedAttack {
			step.SecondaryAttack(s.getMeleeSkill(monster), id, 1, step.Distance(1, 1))
		} else {
			lastThrowTime = time.Now()
		}

		completedAttackLoops++
		previousUnitID = int(id)
	}
}

func (s AmazonLeveling) KillBossSequence(
	monsterSelector func(d game.Data) (data.UnitID, bool),
	skipOnImmunities []stat.Resist,
) error {
	completedAttackLoops := 0
	previousUnitID := 0
	//const numOfAttacks = 5

	for {
		id, found := monsterSelector(*s.Data)
		if !found {
			return nil
		}
		if previousUnitID != int(id) {
			completedAttackLoops = 0
		}

		if !s.preBattleChecks(id, skipOnImmunities) {
			return nil
		}

		if completedAttackLoops >= maxAmazonLevelingAttackLoops {
			return nil
		}

		completedAttackLoops++
		previousUnitID = int(id)

		if s.shouldSummonValkyrie() {
			step.SecondaryAttack(skill.Valkyrie, id, 1, step.Distance(minAmazonLevelingDistance, maxAmazonLevelingDistance))
		} else {
			step.SecondaryAttack(s.getMeleeSkill(data.Monster{}), id, 1, step.Distance(1, 1))
		}
	}
}

func (s AmazonLeveling) killMonster(npc npc.ID, t data.MonsterType) error {
	return s.KillMonsterSequence(func(d game.Data) (data.UnitID, bool) {
		m, found := d.Monsters.FindOne(npc, t)
		if !found {
			return 0, false
		}

		return m.UnitID, true
	}, nil)
}

func (s AmazonLeveling) killBoss(npc npc.ID, t data.MonsterType) error {
	return s.KillBossSequence(func(d game.Data) (data.UnitID, bool) {
		m, found := d.Monsters.FindOne(npc, t)
		if !found {
			return 0, false
		}

		return m.UnitID, true
	}, nil)
}

func (s AmazonLeveling) shouldSummonValkyrie() bool {
	if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.Valkyrie); found {
		needsValkyrie := true

		for _, monster := range s.Data.Monsters { // Check existing pets
			if monster.IsPet() {
				switch monster.Name {
				case npc.Valkyrie:
					needsValkyrie = false
				}
			}
		}
		return needsValkyrie
	}

	return false
}

func (s AmazonLeveling) PreCTABuffSkills() []skill.ID {
	if s.shouldSummonValkyrie() {
		return []skill.ID{skill.Valkyrie}
	}

	return []skill.ID{}
}

func (s AmazonLeveling) BuffSkills() []skill.ID {
	return []skill.ID{}
}

func (s AmazonLeveling) KillCountess() error {
	return s.killMonster(npc.DarkStalker, data.MonsterTypeSuperUnique)
}

func (s AmazonLeveling) KillAndariel() error {
	return s.killBoss(npc.Andariel, data.MonsterTypeUnique)
}

func (s AmazonLeveling) KillSummoner() error {
	return s.killMonster(npc.Summoner, data.MonsterTypeUnique)
}

func (s AmazonLeveling) KillDuriel() error {
	return s.killBoss(npc.Duriel, data.MonsterTypeUnique)
}

func (s AmazonLeveling) KillCouncil() error {
	return s.KillMonsterSequence(func(d game.Data) (data.UnitID, bool) {
		// Exclude monsters that are not council members
		var councilMembers []data.Monster
		for _, m := range d.Monsters {
			if m.Name == npc.CouncilMember || m.Name == npc.CouncilMember2 || m.Name == npc.CouncilMember3 {
				councilMembers = append(councilMembers, m)
			}
		}

		// Order council members by distance
		sort.Slice(councilMembers, func(i, j int) bool {
			distanceI := s.PathFinder.DistanceFromMe(councilMembers[i].Position)
			distanceJ := s.PathFinder.DistanceFromMe(councilMembers[j].Position)

			return distanceI < distanceJ
		})

		for _, m := range councilMembers {
			return m.UnitID, true
		}

		return 0, false
	}, nil)
}

func (s AmazonLeveling) KillMephisto() error {
	return s.killBoss(npc.Mephisto, data.MonsterTypeUnique)
}

func (s AmazonLeveling) KillIzual() error {
	return s.killBoss(npc.Izual, data.MonsterTypeUnique)
}

func (s AmazonLeveling) KillDiablo() error {
	timeout := time.Second * 20
	startTime := time.Now()
	diabloFound := false

	for {
		if time.Since(startTime) > timeout && !diabloFound {
			s.Logger.Error("Diablo was not found, timeout reached")
			return nil
		}

		diablo, found := s.Data.Monsters.FindOne(npc.Diablo, data.MonsterTypeUnique)
		if !found || diablo.Stats[stat.Life] <= 0 {
			// Already dead
			if diabloFound {
				return nil
			}

			// Keep waiting...
			time.Sleep(200)
			continue
		}

		diabloFound = true
		s.Logger.Info("Diablo detected, attacking")

		return s.killMonster(npc.Diablo, data.MonsterTypeUnique)
	}
}

func (s AmazonLeveling) KillPindle() error {
	return s.killBoss(npc.DefiledWarrior, data.MonsterTypeSuperUnique)
}

func (s AmazonLeveling) KillNihlathak() error {
	return s.killBoss(npc.Nihlathak, data.MonsterTypeSuperUnique)
}

func (s AmazonLeveling) KillAncients() error {
	originalBackToTownCfg := s.CharacterCfg.BackToTown
	s.CharacterCfg.BackToTown.NoHpPotions = false
	s.CharacterCfg.BackToTown.NoMpPotions = false
	s.CharacterCfg.BackToTown.EquipmentBroken = false
	s.CharacterCfg.BackToTown.MercDied = false

	for _, m := range s.Data.Monsters.Enemies(data.MonsterEliteFilter()) {
		foundMonster, found := s.Data.Monsters.FindOne(m.Name, data.MonsterTypeSuperUnique)
		if !found {
			continue
		}
		step.MoveTo(data.Position{X: 10062, Y: 12639})

		s.killMonster(foundMonster.Name, data.MonsterTypeSuperUnique)

	}

	s.CharacterCfg.BackToTown = originalBackToTownCfg
	s.Logger.Info("Restored original back-to-town checks after Ancients fight.")
	return nil
}

func (s AmazonLeveling) KillBaal() error {
	return s.killBoss(npc.BaalCrab, data.MonsterTypeUnique)
}

func (s AmazonLeveling) ShouldResetSkills() bool {
	lvl, _ := s.Data.PlayerUnit.FindStat(stat.Level, 0)
	if lvl.Value == 35 && s.Data.PlayerUnit.Skills[skill.PoisonJavelin].Level > 5 {
		s.Logger.Info("Resetting skills: Level 35 and Poison Javelin level > 5")
		return true
	}

	return false
}

func (s AmazonLeveling) getRemainingThrowables() int {
	for _, itm := range s.Data.Inventory.ByLocation(item.LocLeftArm) {
		if itm.Location.LocationType == item.LocationEquipped {
			if qty, twoHanded := itm.FindStat(stat.Quantity, 0); twoHanded {
				return qty.Value
			}
		}
	}
	return 0
}

func (s AmazonLeveling) getMeleeSkill(m data.Monster) skill.ID {
	meleeSkill := skill.AttackSkill
	mana, _ := s.Data.PlayerUnit.FindStat(stat.Mana, 0)
	manaRequired := 5

	if m.UnitID != 0 && m.IsImmune(stat.LightImmune) {
		meleeSkill = skill.Jab
	} else {
		if s.Data.PlayerUnit.Skills[skill.ChargedStrike].Level > 0 {
			meleeSkill = skill.ChargedStrike
			manaRequired = 10
		} else if s.Data.PlayerUnit.Skills[skill.PowerStrike].Level > 0 {
			meleeSkill = skill.PowerStrike
		} else if s.Data.PlayerUnit.Skills[skill.Jab].Level > 0 {
			meleeSkill = skill.Jab
		}
	}

	if mana.Value < manaRequired {
		meleeSkill = skill.AttackSkill
	}
	return meleeSkill
}

func (s AmazonLeveling) SkillsToBind() (skill.ID, []skill.ID) {
	// Primary skill will be the basic attack for interacting with objects and as a fallback.
	mainSkill := skill.AttackSkill
	skillBindings := []skill.ID{}

	//Begining / Immune attack
	if s.Data.PlayerUnit.Skills[skill.Jab].Level > 0 {
		skillBindings = append(skillBindings, skill.Jab)
	}

	//Range attack
	if s.Data.PlayerUnit.Skills[skill.LightningFury].Level > 0 {
		skillBindings = append(skillBindings, skill.LightningFury)
	} else if s.Data.PlayerUnit.Skills[skill.PoisonJavelin].Level > 0 {
		skillBindings = append(skillBindings, skill.PoisonJavelin)
	}

	//Melee attack
	if s.Data.PlayerUnit.Skills[skill.ChargedStrike].Level > 0 {
		skillBindings = append(skillBindings, skill.ChargedStrike)
	} else if s.Data.PlayerUnit.Skills[skill.PowerStrike].Level > 0 {
		skillBindings = append(skillBindings, skill.PowerStrike)
	}

	if s.Data.PlayerUnit.Skills[skill.Valkyrie].Level > 0 {
		skillBindings = append(skillBindings, skill.Valkyrie)
	} else if s.Data.PlayerUnit.Skills[skill.Decoy].Level > 0 {
		skillBindings = append(skillBindings, skill.Decoy)
	}

	if s.Data.PlayerUnit.Skills[skill.BattleCommand].Level > 0 {
		skillBindings = append(skillBindings, skill.BattleCommand)
	}

	if s.Data.PlayerUnit.Skills[skill.BattleOrders].Level > 0 {
		skillBindings = append(skillBindings, skill.BattleOrders)
	}

	_, found := s.Data.Inventory.Find(item.TomeOfTownPortal, item.LocationInventory)
	if found {
		skillBindings = append(skillBindings, skill.TomeOfTownPortal)
	}

	s.Logger.Info("Skills bound", "mainSkill", mainSkill, "skillBindings", skillBindings)
	return mainSkill, skillBindings
}

func (s AmazonLeveling) StatPoints() []context.StatAllocation {
	stats := []context.StatAllocation{
		{Stat: stat.Dexterity, Points: 30},
		{Stat: stat.Vitality, Points: 50},
		{Stat: stat.Strength, Points: 30},
		{Stat: stat.Dexterity, Points: 35},
		{Stat: stat.Vitality, Points: 75},
		{Stat: stat.Strength, Points: 35},
		{Stat: stat.Vitality, Points: 100},
		{Stat: stat.Strength, Points: 40},
		{Stat: stat.Dexterity, Points: 40},
		{Stat: stat.Vitality, Points: 125},
		{Stat: stat.Strength, Points: 45},
		{Stat: stat.Dexterity, Points: 45},
		{Stat: stat.Vitality, Points: 150},
		{Stat: stat.Strength, Points: 50},
		{Stat: stat.Dexterity, Points: 50},
		{Stat: stat.Vitality, Points: 175},
		{Stat: stat.Dexterity, Points: 109},
		{Stat: stat.Strength, Points: 156},
		{Stat: stat.Vitality, Points: 999},
	}
	s.Logger.Debug("Stat point allocation plan", "stats", stats)
	return stats
}

func (s AmazonLeveling) SkillPoints() []skill.ID {
	lvl, _ := s.Data.PlayerUnit.FindStat(stat.Level, 0)

	var skillSequence []skill.ID

	if lvl.Value < 35 {
		skillSequence = []skill.ID{
			skill.Jab,
			skill.CriticalStrike,
			skill.InnerSight,
			skill.PoisonJavelin, skill.Dodge, skill.PowerStrike,
			skill.PoisonJavelin, skill.PoisonJavelin, skill.PoisonJavelin,
			skill.PoisonJavelin, skill.PoisonJavelin, skill.PoisonJavelin,
			skill.PoisonJavelin, skill.SlowMissiles, skill.Avoid,
			skill.LightningBolt,
			skill.Penetrate,
			skill.ChargedStrike, skill.ChargedStrike, skill.ChargedStrike,
			skill.ChargedStrike, skill.ChargedStrike, skill.ChargedStrike,
			skill.ChargedStrike, skill.ChargedStrike,
			skill.Decoy, skill.Evade,
			skill.ChargedStrike,
			skill.ChargedStrike, skill.ChargedStrike,
			skill.PlagueJavelin,
			skill.Valkyrie,
			skill.LightningFury, skill.LightningFury, skill.LightningFury,
			skill.LightningFury, skill.LightningFury, skill.LightningFury,
		}
	} else {
		skillSequence = []skill.ID{
			skill.CriticalStrike, skill.InnerSight,
			skill.Dodge,
			skill.SlowMissiles, skill.Avoid,
			skill.Penetrate,
			skill.Decoy, skill.Evade,
			skill.Valkyrie, skill.Pierce,
			skill.Pierce, skill.Pierce, skill.Pierce,
			skill.Jab,
			skill.PoisonJavelin, skill.PowerStrike,
			skill.LightningBolt, skill.PlagueJavelin,
			skill.ChargedStrike,
			skill.LightningFury, skill.LightningFury, skill.LightningFury,
			skill.LightningFury, skill.LightningFury, skill.LightningFury,
			skill.ChargedStrike, skill.ChargedStrike, skill.ChargedStrike, skill.ChargedStrike, skill.ChargedStrike,
			skill.ChargedStrike, skill.ChargedStrike, skill.ChargedStrike, skill.ChargedStrike, skill.ChargedStrike,
			skill.ChargedStrike, skill.ChargedStrike, skill.ChargedStrike,
			skill.LightningFury, skill.LightningFury, skill.LightningFury,
			skill.LightningFury, skill.LightningFury, skill.LightningFury,
			skill.ChargedStrike, skill.LightningFury,
			skill.LightningFury, skill.LightningFury,
			skill.ChargedStrike, skill.LightningFury,
			skill.LightningFury, skill.LightningFury, skill.LightningFury, skill.LightningFury,
			skill.ChargedStrike, skill.ChargedStrike, skill.ChargedStrike, skill.ChargedStrike,
			skill.LightningStrike, skill.LightningStrike, skill.LightningStrike, skill.LightningStrike, skill.LightningStrike,
			skill.LightningStrike, skill.LightningStrike, skill.LightningStrike, skill.LightningStrike, skill.LightningStrike,
			skill.LightningStrike, skill.LightningStrike, skill.LightningStrike, skill.LightningStrike, skill.LightningStrike,
			skill.LightningStrike, skill.LightningStrike, skill.LightningStrike, skill.LightningStrike, skill.LightningStrike,
			skill.PowerStrike, skill.PowerStrike, skill.PowerStrike, skill.PowerStrike, skill.PowerStrike,
			skill.PowerStrike, skill.PowerStrike, skill.PowerStrike, skill.PowerStrike, skill.PowerStrike,
			skill.PowerStrike, skill.PowerStrike, skill.PowerStrike, skill.PowerStrike, skill.PowerStrike,
			skill.PowerStrike, skill.PowerStrike, skill.PowerStrike, skill.PowerStrike,
			skill.LightningBolt, skill.LightningBolt, skill.LightningBolt, skill.LightningBolt, skill.LightningBolt,
			skill.LightningBolt, skill.LightningBolt, skill.LightningBolt, skill.LightningBolt, skill.LightningBolt,
			skill.LightningBolt, skill.LightningBolt, skill.LightningBolt, skill.LightningBolt, skill.LightningBolt,
			skill.LightningBolt, skill.LightningBolt, skill.LightningBolt, skill.LightningBolt,
		}
	}

	return skillSequence
}

func (s AmazonLeveling) GetAdditionalRunewords() []string {
	additionalRunewords := action.GetCastersCommonRunewords()
	return additionalRunewords
}
