package character

import (
	"fmt"
	"log/slog"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/d2go/pkg/data/skill"
	"github.com/hectorgimenez/d2go/pkg/data/stat"
	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
)

const (
	dragondinMeleeRange     = 5
	dragondinMaxAttackLoops = 20
)

type Dragondin struct {
	BaseCharacter
}

func (d Dragondin) ShouldIgnoreMonster(m data.Monster) bool {
	if m.Type == data.MonsterTypeNormal {
		return true
	}

	distance := d.PathFinder.DistanceFromMe(m.Position)

	return distance > dragondinMeleeRange
}

func (d Dragondin) CheckKeyBindings() []skill.ID {
	requireKeybindings := []skill.ID{skill.Conviction, skill.Zeal, skill.HolyShield, skill.TomeOfTownPortal}
	missingKeybindings := []skill.ID{}

	for _, cskill := range requireKeybindings {
		if _, found := d.Data.KeyBindings.KeyBindingForSkill(cskill); !found {
			missingKeybindings = append(missingKeybindings, cskill)
		}
	}

	if len(missingKeybindings) > 0 {
		d.Logger.Debug("There are missing required key bindings.", slog.Any("Bindings", missingKeybindings))
	}

	return missingKeybindings
}

func (d Dragondin) KillMonsterSequence(
	monsterSelector func(d game.Data) (data.UnitID, bool),
	skipOnImmunities []stat.Resist,
) error {
	completedAttackLoops := 0
	var previousUnitID data.UnitID
	outOfRangeAttempts := 0
	ctx := context.Get()

	for {
		ctx.PauseIfNotPriority()

		id, found := monsterSelector(*d.Data)
		if !found {
			return nil
		}

		monster, found := d.Data.Monsters.FindByID(id)
		if !found {
			d.Logger.Info("Monster not found", slog.String("monster", fmt.Sprintf("%v", monster)))
			return nil
		}

		if monster.Type == data.MonsterTypeNormal {
			return nil
		}

		if previousUnitID != id {
			completedAttackLoops = 0
		}

		if !d.preBattleChecks(id, skipOnImmunities) {
			return nil
		}

		distance := d.PathFinder.DistanceFromMe(monster.Position)
		if distance > dragondinMeleeRange {
			if err := step.MoveTo(monster.Position, step.WithIgnoreMonsters()); err != nil {
				d.Logger.Debug("Unable to move into melee range", slog.String("error", err.Error()))
			}

			outOfRangeAttempts++
			if outOfRangeAttempts >= dragondinMaxAttackLoops {
				return nil
			}

			previousUnitID = id

			continue
		}

		step.PrimaryAttack(
			id,
			1,
			false,
			step.Distance(1, 3),
			step.EnsureAura(skill.Conviction),
		)

		completedAttackLoops++
		outOfRangeAttempts = 0
		previousUnitID = id

		if completedAttackLoops >= dragondinMaxAttackLoops {
			return nil
		}
	}
}

func (d Dragondin) killMonster(npcID npc.ID, t data.MonsterType) error {
	return d.KillMonsterSequence(func(gd game.Data) (data.UnitID, bool) {
		m, found := gd.Monsters.FindOne(npcID, t)
		if !found {
			return 0, false
		}

		return m.UnitID, true
	}, nil)
}

func (d Dragondin) killMonsterByName(id npc.ID, monsterType data.MonsterType) error {
	for {
		if m, found := d.Data.Monsters.FindOne(id, monsterType); found {
			if m.Stats[stat.Life] <= 0 {
				break
			}

			d.KillMonsterSequence(func(gd game.Data) (data.UnitID, bool) {
				if m, found := gd.Monsters.FindOne(id, monsterType); found {
					return m.UnitID, true
				}
				return 0, false
			}, nil)
		} else {
			break
		}
	}
	return nil
}

func (d Dragondin) BuffSkills() []skill.ID {
	if _, found := d.Data.KeyBindings.KeyBindingForSkill(skill.HolyShield); found {
		return []skill.ID{skill.HolyShield}
	}

	return []skill.ID{}
}

func (d Dragondin) PreCTABuffSkills() []skill.ID {
	return []skill.ID{}
}

func (d Dragondin) KillCountess() error {
	return d.killMonsterByName(npc.DarkStalker, data.MonsterTypeSuperUnique)
}

func (d Dragondin) KillAndariel() error {
	return d.killMonsterByName(npc.Andariel, data.MonsterTypeUnique)
}

func (d Dragondin) KillSummoner() error {
	return d.killMonsterByName(npc.Summoner, data.MonsterTypeUnique)
}

func (d Dragondin) KillDuriel() error {
	return d.killMonsterByName(npc.Duriel, data.MonsterTypeUnique)
}

func (d Dragondin) KillCouncil() error {
	return d.KillMonsterSequence(func(gd game.Data) (data.UnitID, bool) {
		for _, m := range gd.Monsters.Enemies() {
			if (m.Name == npc.CouncilMember || m.Name == npc.CouncilMember2 || m.Name == npc.CouncilMember3) && m.Stats[stat.Life] > 0 {
				return m.UnitID, true
			}
		}
		return 0, false
	}, nil)
}

func (d Dragondin) KillMephisto() error {
	return d.killMonsterByName(npc.Mephisto, data.MonsterTypeUnique)
}

func (d Dragondin) KillIzual() error {
	return d.killMonster(npc.Izual, data.MonsterTypeUnique)
}

func (d Dragondin) KillDiablo() error {
	return d.killMonster(npc.Diablo, data.MonsterTypeUnique)
}

func (d Dragondin) KillPindle() error {
	return d.killMonsterByName(npc.DefiledWarrior, data.MonsterTypeSuperUnique)
}

func (d Dragondin) KillNihlathak() error {
	return d.killMonsterByName(npc.Nihlathak, data.MonsterTypeSuperUnique)
}

func (d Dragondin) KillBaal() error {
	return d.killMonster(npc.BaalCrab, data.MonsterTypeUnique)
}
