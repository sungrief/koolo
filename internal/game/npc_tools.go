package game

import (
	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
)

func IsActBoss(m data.Monster) bool {
	switch m.Name {
	case npc.Andariel:
	case npc.Duriel:
	case npc.Mephisto:
	case npc.Diablo:
	case npc.BaalCrab:
		return true
	}
	return false
}

func IsQuestEnemy(m data.Monster) bool {
	if IsActBoss(m) {
		return true
	}
	switch m.Name {
	case npc.Summoner:
	case npc.CouncilMember:
	case npc.CouncilMember2:
	case npc.CouncilMember3:
		return true
	}
	return false
}
