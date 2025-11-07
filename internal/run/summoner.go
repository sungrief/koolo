package run

import (
	"errors"

	"github.com/hectorgimenez/d2go/pkg/data/area"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/d2go/pkg/data/object"
	"github.com/hectorgimenez/d2go/pkg/data/quest"
	"github.com/hectorgimenez/koolo/internal/action"
	"github.com/hectorgimenez/koolo/internal/config"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/utils"
)

type Summoner struct {
	ctx *context.Status
}

func NewSummoner() *Summoner {
	return &Summoner{
		ctx: context.Get(),
	}
}

func (s Summoner) Name() string {
	return string(config.SummonerRun)
}

func (s Summoner) CheckConditions(parameters *RunParameters) SequencerResult {
	farmingRun := IsFarmingRun(parameters)
	questCompleted := s.ctx.Data.Quests[quest.Act2TheSummoner].Completed()
	if (farmingRun && !questCompleted) || (!farmingRun && questCompleted) {
		return SequencerSkip
	}
	return SequencerOk
}

func (s Summoner) Run(parameters *RunParameters) error {
	// Use the waypoint to get to Arcane Sanctuary
	if s.ctx.CharacterCfg.Game.Summoner.KillFireEye {
		NewFireEye().Run(parameters)

		obj, _ := s.ctx.Data.Objects.FindOne(object.ArcaneSanctuaryPortal)

		err := action.InteractObject(obj, func() bool {
			updatedObj, found := s.ctx.Data.Objects.FindOne(object.ArcaneSanctuaryPortal)
			if found {
				if !updatedObj.Selectable {
					s.ctx.Logger.Debug("Interacted with ArcaneSanctuaryPortal")
				}
				return !updatedObj.Selectable
			}
			return false
		})

		if err != nil {
			return err
		}

		err = action.InteractObject(obj, func() bool {
			return s.ctx.Data.PlayerUnit.Area == area.ArcaneSanctuary
		})

		if err != nil {
			return err
		}

		utils.Sleep(300)

	}

	if !s.ctx.CharacterCfg.Game.Summoner.KillFireEye {
		err := action.WayPoint(area.ArcaneSanctuary)
		if err != nil {
			return err
		}
	}

	action.Buff()

	// Get the Summoner's position from the cached map data
	areaData := s.ctx.Data.Areas[area.ArcaneSanctuary]
	summonerNPC, found := areaData.NPCs.FindOne(npc.Summoner)
	if !found || len(summonerNPC.Positions) == 0 {
		return errors.New("failed to find the Summoner")
	}

	// Move to the Summoner's position using the static coordinates from map data
	if err := action.MoveToCoords(summonerNPC.Positions[0]); err != nil {
		return err
	}

	// Kill Summoner
	if err := s.ctx.Char.KillSummoner(); err != nil {
		return err
	}

	if IsQuestRun(parameters) {
		if err := s.goToCanyon(); err != nil {
			return err
		}
	}

	return nil
}

func (s Summoner) goToCanyon() error {
	// Interact with journal to open poratl
	tome, found := s.ctx.Data.Objects.FindOne(object.YetAnotherTome)
	if !found {
		s.ctx.Logger.Error("YetAnotherTome (journal) not found after Summoner kill. This is unexpected.")
		return errors.New("Journal not found after summoner")
	}

	err := action.InteractObject(tome, func() bool {
		_, found := s.ctx.Data.Objects.FindOne(object.PermanentTownPortal)
		return found
	})
	if err != nil {
		return err
	}

	//go through portal
	portal, _ := s.ctx.Data.Objects.FindOne(object.PermanentTownPortal)
	err = action.InteractObject(portal, func() bool {
		return s.ctx.Data.PlayerUnit.Area == area.CanyonOfTheMagi && s.ctx.Data.AreaData.IsInside(s.ctx.Data.PlayerUnit.Position)
	})
	if err != nil {
		return err
	}

	//Get WP
	err = action.DiscoverWaypoint()
	if err != nil {
		return err
	}
	return nil // Return to re-evaluate after completing this chain.
}
