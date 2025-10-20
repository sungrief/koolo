package run

import (
	"github.com/hectorgimenez/koolo/internal/config"
	"github.com/hectorgimenez/koolo/internal/context"
)

type Leveling struct {
	ctx *context.Status
}

func NewLeveling() *Leveling {
	return &Leveling{
		ctx: context.Get(),
	}
}

func (a Leveling) Name() string {
	return string(config.LevelingRun)
}

func (a Leveling) Run() error {
	// Adjust settings based on difficulty
	a.AdjustDifficultyConfig()

	if err := a.act1(); err != nil {
		return err
	}
	if err := a.act2(); err != nil {
		return err
	}
	if err := a.act3(); err != nil {
		return err
	}
	if err := a.act4(); err != nil {
		return err
	}
	if err := a.act5(); err != nil {
		return err
	}

	return nil
}
