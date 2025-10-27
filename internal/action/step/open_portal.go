package step

import (
	"errors" 
	"time"

	"github.com/hectorgimenez/d2go/pkg/data/object"
	"github.com/hectorgimenez/d2go/pkg/data/skill"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/utils"
)


var ErrPlayerDied = errors.New("player is dead")

func OpenPortal() error {
	ctx := context.Get()
	ctx.SetLastStep("OpenPortal")

	lastRun := time.Time{}
	for {
		// IMPORTANT: Check for player death at the beginning of each loop iteration
		if ctx.Data.PlayerUnit.HPPercent() <= 0 {
			return ErrPlayerDied // Player is dead, stop trying to open portal
		}

		// Pause the execution if the priority is not the same as the execution priority
		ctx.PauseIfNotPriority()

		_, found := ctx.Data.Objects.FindOne(object.TownPortal)
		if found {
			return nil // Portal found, success!
		}

		// Give some time to portal to popup before retrying...
		if time.Since(lastRun) < time.Millisecond*500 {
		continue
		}

		ctx.HID.PressKeyBinding(ctx.Data.KeyBindings.MustKBForSkill(skill.TomeOfTownPortal))
		utils.Sleep(250)
		ctx.HID.Click(game.RightButton, 300, 300)
		lastRun = time.Now()
	}
}