package step

import (
	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/skill"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/utils"
)

// SelectRightSkill selects a skill for the right mouse button
// Uses packets if enabled in config, otherwise falls back to HID
func SelectRightSkill(skillID skill.ID) error {
	ctx := context.Get()

	if ctx.CharacterCfg.PacketCasting.UseForSkillSelection && ctx.PacketSender != nil {
		if err := ctx.PacketSender.SelectRightSkill(skillID); err != nil {
			ctx.Logger.Warn("Failed to select right skill via packet, falling back to HID", "skill", skillID, "error", err)
			return selectSkillViaHID(skillID)
		}
		utils.Sleep(50)
		return nil
	}

	return selectSkillViaHID(skillID)
}

// SelectLeftSkill selects a skill for the left mouse button
// Uses packets if enabled in config, otherwise falls back to HID
func SelectLeftSkill(skillID skill.ID) error {
	ctx := context.Get()

	if ctx.CharacterCfg.PacketCasting.UseForSkillSelection && ctx.PacketSender != nil {
		if err := ctx.PacketSender.SelectLeftSkill(skillID); err != nil {
			ctx.Logger.Warn("Failed to select left skill via packet, falling back to HID", "skill", skillID, "error", err)
			return selectSkillViaHID(skillID)
		}
		utils.Sleep(50)
		return nil
	}

	return selectSkillViaHID(skillID)
}

// selectSkillViaHID is the fallback method that uses keyboard binding
func selectSkillViaHID(skillID skill.ID) error {
	ctx := context.Get()

	kb, found := ctx.Data.KeyBindings.KeyBindingForSkill(skillID)
	if !found {
		ctx.Logger.Warn("No keybinding found for skill", "skill", skillID)
		return nil
	}

	ctx.HID.PressKeyBinding(kb)
	utils.Sleep(50)
	return nil
}

// SelectRightSkillByKeyBinding selects a skill using its keybinding directly
// Uses packets if enabled in config, otherwise falls back to HID
func SelectRightSkillByKeyBinding(kb data.KeyBinding) error {
	ctx := context.Get()

	// Try to find the skill ID from the keybinding
	for skillID, binding := range ctx.Data.KeyBindings.Skills {
		if binding.Key1[0] == kb.Key1[0] {
			return SelectRightSkill(skill.ID(skillID))
		}
	}

	// If we can't find the skill ID, just use HID
	ctx.HID.PressKeyBinding(kb)
	utils.Sleep(50)
	return nil
}

// SelectLeftSkillByKeyBinding selects a skill using its keybinding directly
// Uses packets if enabled in config, otherwise falls back to HID
func SelectLeftSkillByKeyBinding(kb data.KeyBinding) error {
	ctx := context.Get()

	// Try to find the skill ID from the keybinding
	for skillID, binding := range ctx.Data.KeyBindings.Skills {
		if binding.Key1[0] == kb.Key1[0] {
			return SelectLeftSkill(skill.ID(skillID))
		}
	}

	// If we can't find the skill ID, just use HID
	ctx.HID.PressKeyBinding(kb)
	utils.Sleep(50)
	return nil
}
