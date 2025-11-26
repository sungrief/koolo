package bot

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode"

	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/ui"
	"github.com/hectorgimenez/koolo/internal/utils"
	"github.com/lxn/win"
)

var classCoords = map[string][2]int{
	"amazon": {ui.CharAmazonX, ui.CharAmazonY}, "assassin": {ui.CharAssassinX, ui.CharAssassinY},
	"necro": {ui.CharNecroX, ui.CharNecroY}, "barb": {ui.CharBarbX, ui.CharBarbY},
	"pala": {ui.CharPallyX, ui.CharPallyY}, "sorc": {ui.CharSorcX, ui.CharSorcY},
	"druid": {ui.CharDruidX, ui.CharDruidY},
}

func AutoCreateCharacter(class, name string) error {
	ctx := context.Get()
	ctx.Logger.Info("[AutoCreate] Processing", slog.String("class", class), slog.String("name", name))

	// 1. Enter character creation screen
	if !ctx.GameReader.IsInCharacterCreationScreen() {
		opened := false
		for i := 0; i < 5; i++ {
			ctx.HID.Click(game.LeftButton, ui.CharCreateNewBtnX, ui.CharCreateNewBtnY)
			utils.Sleep(180)
			ctx.HID.Click(game.LeftButton, ui.CharCreateNewBtnX, ui.CharCreateNewBtnY)
			utils.Sleep(1500)
			if ctx.GameReader.IsInCharacterCreationScreen() {
				opened = true
				break
			}
		}
		if !opened {
			return errors.New("failed to enter creation screen")
		}
	}

	ctx.SetLastAction("CreateCharacter")

	// 2. Select Class
	classPos := [2]int{0, 0}
	lowerClass := strings.ToLower(class)
	for k, pos := range classCoords {
		if strings.Contains(lowerClass, k) {
			classPos = pos
			break
		}
	}
	if classPos[0] == 0 {
		return fmt.Errorf("unknown class: %s", class)
	}
	ctx.HID.Click(game.LeftButton, classPos[0], classPos[1])
	utils.Sleep(500)

	// 3. Toggle Ladder
	if !ctx.CharacterCfg.Game.IsNonLadderChar {
		ctx.HID.Click(game.LeftButton, ui.CharLadderBtnX, ui.CharLadderBtnY)
		utils.Sleep(300)
	}

	// 4. Input Name
	ctx.HID.Click(game.LeftButton, ui.CharNameInputX, ui.CharNameInputY)
	utils.Sleep(300)
	// Clear existing text
	for i := 0; i < 16; i++ {
		ctx.HID.PressKey(win.VK_BACK)
		utils.Sleep(20)
	}

	// Support special chars (-, _) and English
	nonAsciiDetected := false
	for _, c := range name {
		switch c {
		case '-':
			ctx.HID.PressKey(win.VK_OEM_MINUS)
		case '_':
			win.PostMessage(ctx.GameReader.HWND, win.WM_KEYDOWN, win.VK_LSHIFT, 0)
			utils.Sleep(20)
			ctx.HID.PressKey(win.VK_OEM_MINUS)
			utils.Sleep(20)
			win.PostMessage(ctx.GameReader.HWND, win.WM_KEYUP, win.VK_LSHIFT, 0)
		default:
			if c < 128 {
				ctx.HID.PressKey(byte(unicode.ToUpper(c)))
			} else {
				nonAsciiDetected = true
			}
		}
		utils.Sleep(60)
	}

	if nonAsciiDetected {
		ctx.Logger.Warn("[AutoCreate] Non-English characters detected (skipped).", slog.String("name", name))
	}

	utils.Sleep(500)

	// 5. Click Create Button
	ctx.HID.Click(game.LeftButton, ui.CharCreateBtnX, ui.CharCreateBtnY)
	utils.Sleep(1500)

	// Wait for character selection screen
	for i := 0; i < 5; i++ {
		if ctx.GameReader.IsInCharacterSelectionScreen() {
			return nil
		}
		utils.Sleep(300)
	}

	return errors.New("creation timeout or failed")
}
