package run

import (
	"log/slog"
	"github.com/hectorgimenez/koolo/internal/action"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/config"
)

type Shopping struct{}

func NewShopping() *Shopping { return &Shopping{} }

func (r Shopping) Name() string { return string(config.ShoppingRun) }

func (r Shopping) Run() error {
	ctx := context.Get()
	shop := ctx.CharacterCfg.Shopping
	if !shop.Enabled {
		ctx.Logger.Debug("Shopping run disabled")
		return nil
	}
	cfg := action.ShoppingConfig{
		Enabled:         shop.Enabled,
		RefreshesPerRun: shop.RefreshesPerRun,
		MinGoldReserve:  shop.MinGoldReserve,
		Vendors:         shop.SelectedVendors(),
		Rules:           ctx.Data.CharacterCfg.Runtime.Rules,
		Types:           shop.ItemTypes,
	}
	ctx.Logger.Info("Starting Shopping run",
		slog.Int("vendors", len(cfg.Vendors)),
		slog.Int("passes", cfg.RefreshesPerRun),
	)
	return action.RunShopping(cfg)
}
