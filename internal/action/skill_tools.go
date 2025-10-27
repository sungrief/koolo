package action

import (
	"fmt"
	"slices"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/item"
	"github.com/hectorgimenez/d2go/pkg/data/skill"
	"github.com/hectorgimenez/d2go/pkg/data/stat"
	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/ui"
	"github.com/hectorgimenez/koolo/internal/utils"
)

func HasSkillPointsToUse() bool {
	ctx := context.Get()

	_, isLevelingChar := ctx.Char.(context.LevelingCharacter)
	skillPoints, hasUnusedPoints := ctx.Data.PlayerUnit.FindStat(stat.SkillPoints, 0)

	if !isLevelingChar || !hasUnusedPoints || skillPoints.Value == 0 {
		return false
	}

	return true
}

func EnsureSkillPoints() error {
	ctx := context.Get()

	char, isLevelingChar := ctx.Char.(context.LevelingCharacter)
	skillPoints, hasUnusedPoints := ctx.Data.PlayerUnit.FindStat(stat.SkillPoints, 0)
	remainingPoints := skillPoints.Value

	if !isLevelingChar || !hasUnusedPoints || remainingPoints == 0 {
		if ctx.Data.OpenMenus.SkillTree {
			step.CloseAllMenus()
		}
		return nil
	}

	skillsBuild := char.SkillPoints()
	targetLevels := make(map[skill.ID]int)

	for _, sk := range skillsBuild {
		targetLevels[sk]++
		currentSkillLevel := 0
		if skillData, found := ctx.Data.PlayerUnit.Skills[sk]; found {
			currentSkillLevel = int(skillData.Level)
		}

		if currentSkillLevel < targetLevels[sk] {
			if spendSkillPoint(sk) {
				remainingPoints--
				/*ctx.Logger.Debug(fmt.Sprintf("Increased skill %v to level %d (%d total points remaining)",
				skill.SkillNames[sk], currentSkillLevel+1, remainingPoints))*/
				if remainingPoints <= 0 {
					break
				}
			} else {
				break
			}
		}
	}

	return step.CloseAllMenus()
}

func spendSkillPoint(skillID skill.ID) bool {
	ctx := context.Get()
	beforePoints, _ := ctx.Data.PlayerUnit.FindStat(stat.SkillPoints, 0)

	if !ctx.Data.OpenMenus.SkillTree {
		ctx.HID.PressKeyBinding(ctx.Data.KeyBindings.SkillTree)
		utils.Sleep(100)
	}

	skill, found := skill.Skills[skillID]
	skillDesc := skill.Desc()

	if !found {
		ctx.Logger.Error(fmt.Sprintf("skill not found for character: %v", skillID))
		return false
	}

	if ctx.Data.LegacyGraphics {
		ctx.HID.Click(game.LeftButton, uiSkillPagePositionLegacy[skillDesc.Page-1].X, uiSkillPagePositionLegacy[skillDesc.Page-1].Y)
	} else {
		ctx.HID.Click(game.LeftButton, uiSkillPagePosition[skillDesc.Page-1].X, uiSkillPagePosition[skillDesc.Page-1].Y)
	}
	utils.Sleep(200)

	if ctx.Data.LegacyGraphics {
		ctx.HID.Click(game.LeftButton, uiSkillColumnPositionLegacy[skillDesc.Column-1], uiSkillRowPositionLegacy[skillDesc.Row-1])
	} else {
		ctx.HID.Click(game.LeftButton, uiSkillColumnPosition[skillDesc.Column-1], uiSkillRowPosition[skillDesc.Row-1])
	}
	utils.Sleep(300)

	afterPoints, _ := ctx.Data.PlayerUnit.FindStat(stat.SkillPoints, 0)
	return beforePoints.Value-afterPoints.Value == 1
}

func EnsureSkillBindings() error {
	ctx := context.Get()
	ctx.SetLastAction("EnsureSkillBindings")

	char, isLevelingChar := ctx.Char.(context.LevelingCharacter)
	if !isLevelingChar {
		return nil
	}

	mainSkill, skillsToBind := char.SkillsToBind()

	notBoundSkills := make([]skill.ID, 0)
	for _, sk := range skillsToBind {
		// Only add skills that are not already bound AND are either TomeOfTownPortal or the player has learned them.
		// The check for skill.TomeOfTownPortal ensures it's considered even if not "learned" via skill points.
		if _, found := ctx.Data.KeyBindings.KeyBindingForSkill(sk); !found && (sk == skill.TomeOfTownPortal || ctx.Data.PlayerUnit.Skills[sk].Level > 0) {
			notBoundSkills = append(notBoundSkills, sk)
			slices.Sort(notBoundSkills)
			notBoundSkills = slices.Compact(notBoundSkills) // In case we have duplicates (tp tome)
		}
	}

	clvl, _ := ctx.Data.PlayerUnit.FindStat(stat.Level, 0)
	// Hacky way to find if we're lvling a sorc at clvl 1
	str, _ := ctx.Data.PlayerUnit.FindStat(stat.Strength, 0)

	// This block handles binding skills to F-keys if they are not already bound.
	if len(notBoundSkills) > 0 || (clvl.Value == 1 && str.Value == 10) {
		ctx.Logger.Debug("Unbound skills found, trying to bind")
		if ctx.GameReader.LegacyGraphics() {
			ctx.HID.Click(game.LeftButton, ui.SecondarySkillButtonXClassic, ui.SecondarySkillButtonYClassic)
		} else {
			ctx.HID.Click(game.LeftButton, ui.SecondarySkillButtonX, ui.SecondarySkillButtonY)
		}
		utils.Sleep(300) // Give time for the secondary skill menu to open

		availableKB := getAvailableSkillKB()
		ctx.Logger.Debug(fmt.Sprintf("Available KB: %v", availableKB))
		if len(notBoundSkills) > 0 {
			for i, sk := range notBoundSkills {
				if i >= len(availableKB) { // Prevent out-of-bounds if more skills than available keybindings
					ctx.Logger.Warn(fmt.Sprintf("Not enough available keybindings for skill %v", skill.SkillNames[sk]))
					break
				}
				skillPosition, found := calculateSkillPositionInUI(false, sk)
				if !found {
					ctx.Logger.Error(fmt.Sprintf("Skill %v UI position not found for binding.", skill.SkillNames[sk]))
					continue
				}

				if sk == skill.TomeOfTownPortal {
					gfx := "D2R"
					if ctx.GameReader.LegacyGraphics() {
						gfx = "Legacy"
					}
					ctx.Logger.Info(fmt.Sprintf("TomeOfTownPortal will be bound now at (%d,%d) [%s]", skillPosition.X, skillPosition.Y, gfx))
					ctx.Logger.Info(fmt.Sprintf("EnsureSkillBindings Tome coords (secondary): X=%d Y=%d [Legacy=%v]", skillPosition.X, skillPosition.Y, ctx.GameReader.LegacyGraphics()))
				}

				ctx.HID.MovePointer(skillPosition.X, skillPosition.Y)
				utils.Sleep(100)
				ctx.HID.PressKeyBinding(availableKB[i])
				utils.Sleep(300)
				if sk == skill.TomeOfTownPortal {
					ctx.GameReader.GetData()
					utils.Sleep(150)
					if _, ok := ctx.Data.KeyBindings.KeyBindingForSkill(skill.TomeOfTownPortal); ok {
						ctx.Logger.Info("TomeOfTownPortal binding verified")
					} else {
						ctx.Logger.Warn("TomeOfTownPortal binding verification failed after click")
					}
				}
			}
		} else {
			if _, found := ctx.Data.KeyBindings.KeyBindingForSkill(skill.FireBolt); !found {
				ctx.Logger.Debug("Lvl 1 sorc found - forcing fire bolt bind")
				if ctx.GameReader.LegacyGraphics() {
					ctx.HID.MovePointer(1000, 530) // Position for Fire Bolt in Legacy
				} else {
					ctx.HID.MovePointer(685, 545) // Position for Fire Bolt in Resurrected
				}
				utils.Sleep(100)
				// Assuming availableKB[0] is the first available F-key for Fire Bolt
				if len(availableKB) > 0 {
					ctx.HID.PressKeyBinding(availableKB[0])
					utils.Sleep(300)
				} else {
					ctx.Logger.Warn("No available keybindings to bind Fire Bolt for level 1 sorceress.")
				}
			}
		}
		// Close the skill assignment menu if it was opened for binding F-keys
		step.CloseAllMenus()
		utils.Sleep(300)
	}

	// Set left (main) skill
	if ctx.GameReader.LegacyGraphics() {
		ctx.HID.Click(game.LeftButton, ui.MainSkillButtonXClassic, ui.MainSkillButtonYClassic)
	} else {
		ctx.HID.Click(game.LeftButton, ui.MainSkillButtonX, ui.MainSkillButtonY)
	}
	utils.Sleep(300) // Give time for the main skill assignment UI to open

	skillPosition, found := calculateSkillPositionInUI(true, mainSkill)
	if found {
		ctx.HID.Click(game.LeftButton, skillPosition.X, skillPosition.Y)
		utils.Sleep(300)
	} else {
		ctx.Logger.Error(fmt.Sprintf("Failed to find UI position for main skill %v (ID: %d)", skill.SkillNames[mainSkill], mainSkill))
	}

	return step.CloseAllMenus()
}

func calculateSkillPositionInUI(mainSkill bool, skillID skill.ID) (data.Position, bool) {
	ctx := context.Get()

	foundInSkills := true
	if _, found := ctx.Data.PlayerUnit.Skills[skillID]; !found {
		if skillID == skill.TomeOfTownPortal {
			foundInSkills = false
		} else {
			return data.Position{}, false
		}
	}

	targetSkill := skill.Skills[skillID]
	descs := make(map[skill.ID]skill.Skill)
	totalRows := make([]int, 0)
	pageSkills := make(map[int][]skill.ID)
	row := 0
	column := 0

	for skID := range ctx.Data.PlayerUnit.Skills {
		sk := skill.Skills[skID]
		// Skip skills that can not be bind
		if sk.Desc().ListRow < 0 {
			continue
		}

		// Skip skills that can not be bind to current mouse button
		if (mainSkill && !sk.LeftSkill) || (!mainSkill && !sk.RightSkill) {
			continue
		}

		//Skip skills with charges
		if ctx.Data.PlayerUnit.Skills[skID].Charges > 0 {
			continue
		}

		descs[skID] = sk
		if sk.Desc().Page == targetSkill.Desc().Page {
			pageSkills[sk.Desc().Page] = append(pageSkills[sk.Desc().Page], skID)
		}
		totalRows = append(totalRows, sk.Desc().ListRow)

	}

	if !foundInSkills {
		totalRows = append(totalRows, targetSkill.Desc().ListRow)
		pageSkills[targetSkill.Desc().Page] = append(pageSkills[targetSkill.Desc().Page], skillID)
	}

	if ctx.GameReader.LegacyGraphics() && !mainSkill && skillID == skill.TomeOfTownPortal {
		if _, hasIdentify := ctx.Data.Inventory.Find(item.TomeOfIdentify, item.LocationInventory); hasIdentify {
			if _, identifyInSkills := ctx.Data.PlayerUnit.Skills[skill.TomeOfIdentify]; !identifyInSkills {
				identifyDesc := skill.Skills[skill.TomeOfIdentify].Desc()
				totalRows = append(totalRows, identifyDesc.ListRow)
				pageSkills[targetSkill.Desc().Page] = append(pageSkills[targetSkill.Desc().Page], skill.TomeOfIdentify)
			}
		}
	}

	slices.Sort(totalRows)
	totalRows = slices.Compact(totalRows)

	for i, currentRow := range totalRows {
		if currentRow == targetSkill.Desc().ListRow {
			row = i
			break
		}
	}

	skillsInPage := pageSkills[targetSkill.Desc().Page]
	slices.Sort(skillsInPage)
	for i, skills := range skillsInPage {
		if skills == targetSkill.ID {
			column = i
			break
		}
	}

	// Special handling for Legacy + secondary list + TomeOfTownPortal:
	// Column is determined by presence of TomeOfIdentify (left shift by one slot when present)
	if ctx.GameReader.LegacyGraphics() && !mainSkill && skillID == skill.TomeOfTownPortal {
		if _, hasIdentify := ctx.Data.Inventory.Find(item.TomeOfIdentify, item.LocationInventory); hasIdentify {
			column = 1
		} else {
			column = 0
		}
	}

	if ctx.GameReader.LegacyGraphics() {
		skillOffsetX := ui.MainSkillListFirstSkillXClassic + (ui.SkillListSkillOffsetClassic * column)
		if !mainSkill {
			if skillID == skill.TomeOfTownPortal {
				if column == 0 {
					return data.Position{X: 1000, Y: ui.SkillListFirstSkillYClassic - ui.SkillListSkillOffsetClassic*row}, true
				}
				if column == 1 {
					return data.Position{X: 940, Y: ui.SkillListFirstSkillYClassic - ui.SkillListSkillOffsetClassic*row}, true
				}
			}
			skillOffsetX = ui.SecondarySkillListFirstSkillXClassic - (ui.SkillListSkillOffsetClassic * column)
		}

		return data.Position{
			X: skillOffsetX,
			Y: ui.SkillListFirstSkillYClassic - ui.SkillListSkillOffsetClassic*row,
		}, true
	} else {
		skillOffsetX := ui.MainSkillListFirstSkillX - (ui.SkillListSkillOffset * (len(skillsInPage) - (column + 1)))
		if !mainSkill {
			skillOffsetX = ui.SecondarySkillListFirstSkillX + (ui.SkillListSkillOffset * (len(skillsInPage) - (column + 1))) // Order is reversed in resurrected gfx
		}

		return data.Position{
			X: skillOffsetX,
			Y: ui.SkillListFirstSkillY - ui.SkillListSkillOffset*row,
		}, true
	}
}

func GetSkillTotalLevel(skill skill.ID) uint {
	ctx := context.Get()
	skillLevel := ctx.Data.PlayerUnit.Skills[skill].Level

	if skillLevel > 0 {
		if allSkill, allFound := ctx.Data.PlayerUnit.Stats.FindStat(stat.AllSkills, 0); allFound {
			skillLevel += uint(allSkill.Value)
		}

		//Assume it's a player class skill for now
		if classSkills, classFound := ctx.Data.PlayerUnit.Stats.FindStat(stat.AddClassSkills, int(ctx.Data.PlayerUnit.Class)); classFound {
			skillLevel += uint(classSkills.Value)
		}

		//Todo Tabs + skills

		//Todo individual + skills
	}

	return skillLevel
}
