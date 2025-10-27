package action

import (
	"github.com/hectorgimenez/d2go/pkg/nip"
	"github.com/hectorgimenez/koolo/internal/context"
)

// CreateShoppingConfig creates a shopping configuration from the character config
func CreateShoppingConfig() (ShopConfig, error) {
	ctx := context.Get()
	charCfg := ctx.CharacterCfg
	
	// Load shopping rules
	rules, err := LoadShoppingRules(charCfg.ConfigFolderName)
	if err != nil {
		ctx.Logger.Warn("Failed to load shopping rules, using empty rules", "error", err)
		rules = nip.Rules{}
	}
	
	// Get vendor list from config
	vendors := charCfg.Shopping.GetVendorList()
	
	return ShopConfig{
		Enabled:         charCfg.Shopping.Enabled,
		MaxGoldToSpend:  charCfg.Shopping.MaxGoldToSpend,
		MinGoldReserve:  charCfg.Shopping.MinGoldReserve,
		RefreshesPerRun: charCfg.Shopping.RefreshesPerRun,
		VendorsToShop:   vendors,
		ItemTypesToShop: charCfg.Shopping.ItemTypes,
		ShoppingRules:   rules,
	}, nil
}

// Helper function if you need to set defaults
func applyShoppingDefaults(cfg *ShopConfig) {
	if cfg.MaxGoldToSpend == 0 {
		cfg.MaxGoldToSpend = 500000
	}
	if cfg.MinGoldReserve == 0 {
		cfg.MinGoldReserve = 100000
	}
	if cfg.RefreshesPerRun == 0 {
		cfg.RefreshesPerRun = 20
	}
}