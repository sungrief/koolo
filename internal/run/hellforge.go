package run

import (
	"errors"
	"time"

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
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/ui"
	"github.com/hectorgimenez/koolo/internal/utils"
)

type Hellforge struct {
	ctx *context.Status
}

func NewHellforge() *Hellforge {
	return &Hellforge{
		ctx: context.Get(),
	}
}

func (h Hellforge) Name() string {
	return string(config.IzualRun)
}

func (h Hellforge) CheckConditions(parameters *RunParameters) SequencerResult {
	if IsFarmingRun(parameters) {
		return SequencerError
	}

	if !h.ctx.Data.Quests[quest.Act3TheGuardian].Completed() {
		return SequencerStop
	}

	if h.ctx.Data.Quests[quest.Act4HellForge].Completed() {
		return SequencerSkip
	}

	return SequencerOk
}

func (h Hellforge) Run(parameters *RunParameters) error {
	action.WayPoint(area.RiverOfFlame)

	hellforge, found := h.ctx.Data.Objects.FindOne(object.HellForge)
	if !found {
		return errors.New("couldn't find hellforge")
	}

	err := action.MoveToCoords(hellforge.Position, step.WithDistanceToFinish(20))
	if err != nil {
		return err
	}

	action.ClearAreaAroundPlayer(30, data.MonsterAnyFilter())

	_, corpseFound := h.ctx.Data.Corpses.FindOne(npc.Hephasto, data.MonsterTypeNone)
	if !corpseFound {
		hephasto, found := h.ctx.Data.Monsters.FindOne(npc.Hephasto, data.MonsterTypeNone)
		if found {
			h.ctx.Char.KillMonsterSequence(func(d game.Data) (data.UnitID, bool) {
				return hephasto.UnitID, true
			}, nil)
		}
	}

	action.ItemPickup(30)

	err = action.MoveToCoords(hellforge.Position)
	if err != nil {
		return err
	}

	action.ClearAreaAroundPlayer(40, data.MonsterAnyFilter())

	action.ItemPickup(40)

	err = action.MoveToCoords(hellforge.Position)
	if err != nil {
		return err
	}

	for h.ctx.PathFinder.DistanceFromMe(hellforge.Position) < 5 {
		h.ctx.PathFinder.RandomMovement()
		utils.Sleep(500)
	}

	err = action.ReturnTown()
	if err != nil {
		return err
	}

	action.InteractNPC(npc.DeckardCain4)

	h.equipHammer()

	action.UsePortalInTown()

	if !h.hasSoul() {
		return errors.New("mephisto soulstone not found")
	}

	err = h.breakStone()
	if err != nil {
		return err
	}

	start := time.Now()
	for time.Since(start) < time.Millisecond*6000 {
		action.ItemPickup(20)
		utils.Sleep(100)
	}

	action.ReturnTown()

	return nil
}

func (h Hellforge) breakStone() error {
	hellforge, found := h.ctx.Data.Objects.FindOne(object.HellForge)
	if !found {
		return errors.New("couldn't find hellforge")
	}
	err := action.InteractObject(hellforge, func() bool {
		return !h.hasSoul()
	})
	if err != nil {
		return err
	}

	if h.ctx.Data.ActiveWeaponSlot == 0 {
		utils.Sleep(500)
		h.ctx.HID.PressKeyBinding(h.ctx.Data.KeyBindings.SwapWeapons)
		utils.Sleep(500)
	}

	defer func() {
		if h.ctx.Data.ActiveWeaponSlot == 1 {
			utils.Sleep(500)
			h.ctx.HID.PressKeyBinding(h.ctx.Data.KeyBindings.SwapWeapons)
			utils.Sleep(500)
		}
	}()

	err = action.InteractObject(hellforge, func() bool {
		return !h.hasHammer()
	})
	if err != nil {
		return err
	}

	utils.Sleep(500)

	return nil
}

func (h Hellforge) hasSoul() bool {
	_, found := h.ctx.Data.Inventory.Find("MephistosSoulstone", item.LocationInventory)
	return found
}

func (h Hellforge) hasHammer() bool {
	_, found := h.ctx.Data.Inventory.Find("HellforgeHammer", item.LocationInventory, item.LocationStash, item.LocationEquipped)
	return found
}

func (h Hellforge) equipHammer() error {
	hammer, found := h.ctx.Data.Inventory.Find("HellforgeHammer", item.LocationInventory, item.LocationStash, item.LocationEquipped)
	if found {
		if hammer.Location.LocationType != item.LocationEquipped {
			step.CloseAllMenus()
			if h.ctx.Data.ActiveWeaponSlot == 0 {
				utils.Sleep(500)
				h.ctx.HID.PressKeyBinding(h.ctx.Data.KeyBindings.SwapWeapons)
				utils.Sleep(500)
			}
			if hammer.Location.LocationType == item.LocationStash {
				bank, found := h.ctx.Data.Objects.FindOne(object.Bank)
				if !found {
					h.ctx.Logger.Info("bank object not found")
				}
				utils.Sleep(300)
				err := action.InteractObject(bank, func() bool {
					return h.ctx.Data.OpenMenus.Stash
				})
				if err != nil {
					return err
				}
			}
			if hammer.Location.LocationType == item.LocationInventory && !h.ctx.Data.OpenMenus.Inventory {
				h.ctx.HID.PressKeyBinding(h.ctx.Data.KeyBindings.Inventory)
			}
			screenPos := ui.GetScreenCoordsForItem(hammer)
			h.ctx.HID.ClickWithModifier(game.LeftButton, screenPos.X, screenPos.Y, game.ShiftKey)
			utils.Sleep(300)
			if h.ctx.Data.ActiveWeaponSlot == 1 {
				utils.Sleep(500)
				h.ctx.HID.PressKeyBinding(h.ctx.Data.KeyBindings.SwapWeapons)
				utils.Sleep(500)

			}
			step.CloseAllMenus()
		}
		return nil
	}
	return errors.New("hellforge hammer not found")
}
