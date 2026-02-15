package action

import (
	d2goGame "github.com/hectorgimenez/d2go/pkg/data/game" // alias needed because of name comflict with /koolo/internal/game
	"github.com/hectorgimenez/d2go/pkg/memory"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/ui"
	"github.com/hectorgimenez/koolo/internal/utils"
)

const (
	expansionTypeOffset      = uintptr(0x1DEE468) // Mirrors d2go memory offset for the expansion pointer (offset is not exported by d2go).
	expansionCharValueOffset = uintptr(0x5C)      // Offset from expansion pointer to the character type value: 1=Classic, 2=LoD, 3=DLC.
	legacySwitchPollAttempts = 4
	legacySwitchPollDelayMs  = 300
)

func SwitchToLegacyMode() {
	ctx := context.Get()
	ctx.SetLastAction("SwitchToLegacyMode")

	if !ctx.CharacterCfg.ClassicMode {
		return
	}

	enableLegacyMode(ctx, true)
}

func EnableLegacyMode() bool {
	ctx := context.Get()
	ctx.SetLastAction("EnableLegacyMode")

	return enableLegacyMode(ctx, false)
}

func enableLegacyMode(ctx *context.Status, closeMiniPanel bool) bool {
	if ctx == nil || ctx.GameReader == nil {
		return false
	}

	if ctx.Data.LegacyGraphics {
		return true
	}

	// Prevent toggling legacy mode while in lobby or character selection
	// so lobby-game joins are not affected by unintended legacy input.
	if ctx.GameReader.IsInLobby() || ctx.GameReader.IsInCharacterSelectionScreen() {
		return false
	}

	if !legacyGraphicsSupported(ctx) {
		ctx.Logger.Debug("Skipping legacy mode switch for dlc character")
		return false
	}

	if len(ctx.Data.KeyBindings.LegacyToggle.Key1) == 0 {
		ctx.Logger.Warn("Legacy toggle key binding not configured, skipping legacy mode switch")
		return false
	}

	ctx.Logger.Debug("Switching to legacy mode...")
	ctx.HID.PressKey(ctx.Data.KeyBindings.LegacyToggle.Key1[0])
	if !waitForLegacyGraphicsState(ctx, true) {
		ctx.Logger.Debug("Legacy graphics did not activate after toggle input")
		return false
	}

	if closeMiniPanel {
		ctx.Logger.Debug("Closing mini panel...")
		ctx.HID.Click(game.LeftButton, ui.CloseMiniPanelClassicX, ui.CloseMiniPanelClassicY)
		utils.Sleep(100)
	}

	return true
}

func legacyGraphicsSupported(ctx *context.Status) bool {
	if ctx == nil || ctx.GameReader == nil || ctx.GameReader.Process == nil {
		return false
	}

	baseAddr := ctx.GameReader.Process.ModuleBaseAddress()
	if baseAddr == 0 {
		return false
	}

	expansionPtr := uintptr(ctx.GameReader.Process.ReadUInt(baseAddr+expansionTypeOffset, memory.Uint64))
	if expansionPtr == 0 {
		return false
	}

	// Read current character mode directly from memory.
	charType := uint16(ctx.GameReader.Process.ReadUInt(expansionPtr+expansionCharValueOffset, memory.Uint16))
	return charType == d2goGame.CharClassic || charType == d2goGame.CharLoD
}

func waitForLegacyGraphicsState(ctx *context.Status, expected bool) bool {
	for i := 0; i < legacySwitchPollAttempts; i++ {
		utils.Sleep(legacySwitchPollDelayMs)
		ctx.RefreshGameData()
		if ctx.Data.LegacyGraphics == expected {
			return true
		}
	}

	return false
}
