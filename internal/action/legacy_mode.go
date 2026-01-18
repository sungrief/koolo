package action

import (
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/ui"
	"github.com/hectorgimenez/koolo/internal/utils"
)

func SwitchToLegacyMode() {
	ctx := context.Get()
	ctx.SetLastAction("SwitchToLegacyMode")

	// Prevent toggling legacy mode while in lobby or character selection
	// so lobby-game joins are not affected by unintended legacy input.

	if ctx.CharacterCfg.ClassicMode && !ctx.Data.LegacyGraphics {
		if ctx.GameReader.IsInLobby() || ctx.GameReader.IsInCharacterSelectionScreen() {
			return
		}

		ctx.Logger.Debug("Switching to legacy mode...")
		ctx.HID.PressKey(ctx.Data.KeyBindings.LegacyToggle.Key1[0])
		utils.Sleep(1250) // delay to ensure legacy gfx is activ before closing mini panel

		ctx.Logger.Debug("Closing mini panel...")
		ctx.HID.Click(game.LeftButton, ui.CloseMiniPanelClassicX, ui.CloseMiniPanelClassicY)
		utils.Sleep(100)

	}
}
