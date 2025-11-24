package action

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/ui"
	"github.com/hectorgimenez/koolo/internal/utils"
	"github.com/lxn/win"
)

var classClick = map[string][2]int{
	"amazon":      {ui.CharAmazonX, ui.CharAmazonY},
	"assassin":    {ui.CharAssassinX, ui.CharAssassinY},
	"necromancer": {ui.CharNecroX, ui.CharNecroY},
	"barbarian":   {ui.CharBarbX, ui.CharBarbY},
	"paladin":     {ui.CharPallyX, ui.CharPallyY},
	"sorceress":   {ui.CharSorcX, ui.CharSorcY},
	"druid":       {ui.CharDruidX, ui.CharDruidY},
}

// AutoCreateCharacter handles the entire flow of creating a new character.
func AutoCreateCharacter(class, name string) error {
	ctx := context.Get()
	ctx.Logger.Info("[AutoCreate] Starting auto-create flow", slog.String("class", class), slog.String("name", name))

	// 1. Enter the character creation screen
	if err := enterCharacterCreationScreen(); err != nil {
		return err
	}

	// 2. Fill character details
	if err := createCharacterDetails(class, name); err != nil {
		return fmt.Errorf("create character details failed: %w", err)
	}

	// 3. Wait until we return to character selection screen
	timeout := time.After(5 * time.Second)
	for !ctx.GameReader.IsInCharacterSelectionScreen() {
		select {
		case <-timeout:
			return errors.New("timed out waiting for character selection after creation")
		default:
			utils.Sleep(300)
		}
	}

	return nil
}

// enterCharacterCreationScreen tries to click "Create New" until the screen opens.
func enterCharacterCreationScreen() error {
	ctx := context.Get()
	opened := false

	pos := getCreateNewPosition()

	for i := 0; i < 5; i++ {
		ctx.Logger.Debug("[AutoCreate] Clicking Create New", slog.Int("attempt", i+1), slog.Int("x", pos[0]), slog.Int("y", pos[1]))

		ctx.HID.Click(game.LeftButton, pos[0], pos[1])
		utils.Sleep(200)
		ctx.HID.Click(game.LeftButton, pos[0], pos[1])
		utils.Sleep(1500)

		if ctx.GameReader.IsInCharacterCreationScreen() {
			opened = true
			break
		}
	}

	if !opened {
		return errors.New("failed to open character creation screen")
	}
	return nil
}

// createCharacterDetails performs the actions inside the creation screen (Class, Name, Ladder).
func createCharacterDetails(class, name string) error {
	ctx := context.Get()
	ctx.SetLastAction("CreateCharacter")

	if !ctx.GameReader.IsInCharacterCreationScreen() {
		return fmt.Errorf("not in character creation screen")
	}

	normClass := normalizeClassName(class)
	if normClass == "" {
		return fmt.Errorf("unknown class: %s", class)
	}

	pos, ok := classClick[normClass]
	if !ok {
		return fmt.Errorf("unknown class: %s", class)
	}

	ctx.Logger.Info(fmt.Sprintf("Creating character: Class[%s] Name[%s]", normClass, name))

	ctx.HID.Click(game.LeftButton, pos[0], pos[1])
	utils.Sleep(500)

	if !ctx.CharacterCfg.Game.IsNonLadderChar {
		ctx.Logger.Info("Configured as Ladder: Clicking Ladder option")
		ctx.HID.Click(game.LeftButton, ui.CharLadderBtnX, ui.CharLadderBtnY)
		utils.Sleep(300)
	}

	ctx.HID.Click(game.LeftButton, ui.CharNameInputX, ui.CharNameInputY)
	utils.Sleep(300)
	for i := 0; i < 16; i++ {
		ctx.HID.PressKey(win.VK_BACK)
		utils.Sleep(20)
	}
	utils.Sleep(200)

	for _, char := range name {
		if char < 128 {
			upperChar := unicode.ToUpper(char)
			ctx.HID.PressKey(byte(upperChar))
			utils.Sleep(60)
		}
	}

	utils.Sleep(500)
	ctx.HID.Click(game.LeftButton, ui.CharCreateBtnX, ui.CharCreateBtnY)

	utils.Sleep(2000)

	return nil
}

func normalizeClassName(class string) string {
	c := strings.ToLower(class)
	switch {
	case strings.Contains(c, "amazon"):
		return "amazon"
	case strings.Contains(c, "assassin"):
		return "assassin"
	case strings.Contains(c, "necro"):
		return "necromancer"
	case strings.Contains(c, "barb"):
		return "barbarian"
	case strings.Contains(c, "pala"):
		return "paladin"
	case strings.Contains(c, "sorc"):
		return "sorceress"
	case strings.Contains(c, "druid"):
		return "druid"
	default:
		return ""
	}
}

func getCreateNewPosition() [2]int {
	return [2]int{ui.CharCreateNewBtnX, ui.CharCreateNewBtnY}
}
