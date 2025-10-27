package run

import (
	"log/slog"

	"github.com/hectorgimenez/d2go/pkg/data/area"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/koolo/internal/action"
	"github.com/hectorgimenez/koolo/internal/context"
)

// Shopping is a single configurable shopping run
type Shopping struct {
	ShoppingConfig action.ShopConfig
}

func (s Shopping) Name() string {
	return "Shopping"
}

func (s Shopping) Run() error {
	ctx := context.Get()
	
	if !s.ShoppingConfig.Enabled {
		ctx.Logger.Debug("Shopping is disabled, skipping run")
		return nil
	}
	
	if len(s.ShoppingConfig.VendorsToShop) == 0 {
		ctx.Logger.Warn("No vendors configured for shopping, skipping run")
		return nil
	}
	
	ctx.Logger.Info("Starting shopping run",
		slog.Int("refreshes", s.ShoppingConfig.RefreshesPerRun),
		slog.Int("totalVendors", len(s.ShoppingConfig.VendorsToShop)))

	// Make sure we're in town first
	if !ctx.Data.PlayerUnit.Area.IsTown() {
		ctx.Logger.Info("Not in town, using waypoint to return")
		err := action.UseWaypointToTown()
		if err != nil {
			ctx.Logger.Error("Failed to return to town, skipping shopping", slog.Any("error", err))
			return nil
		}
	}

	// Group vendors by town/act
	vendorsByTown := groupVendorsByTown(s.ShoppingConfig.VendorsToShop)
	
	ctx.Logger.Info("Vendors grouped by town", slog.Int("townCount", len(vendorsByTown)))

	totalGoldSpent := 0
	totalItemsPurchased := 0

	// Shop at each town
	for townArea, vendors := range vendorsByTown {
		ctx.Logger.Info("Shopping in town", 
			slog.String("town", townArea.Area().Name),
			slog.Int("vendors", len(vendors)))

		// Travel to this town if needed
		if ctx.Data.PlayerUnit.Area != townArea {
			ctx.Logger.Info("Traveling to town", slog.String("town", townArea.Area().Name))
			err := action.WayPoint(townArea)
			if err != nil {
				ctx.Logger.Error("Failed to travel to town, skipping",
					slog.String("town", townArea.Area().Name),
					slog.Any("error", err))
				continue
			}
			
			ctx.RefreshGameData()
			if ctx.Data.PlayerUnit.Area != townArea {
				ctx.Logger.Error("Failed to reach town, skipping",
					slog.String("town", townArea.Area().Name))
				continue
			}
		}

		// Create a temporary config for this town's vendors
		townConfig := s.ShoppingConfig
		townConfig.VendorsToShop = vendors

		// Shop at vendors in this town
		err := action.ShopAtVendors(townConfig)
		if err != nil {
			ctx.Logger.Error("Shopping failed in town",
				slog.String("town", townArea.Area().Name),
				slog.Any("error", err))
			continue
		}
	}

	ctx.Logger.Info("Shopping run completed",
		slog.Int("goldSpent", totalGoldSpent),
		slog.Int("itemsPurchased", totalItemsPurchased))
	
	return nil
}

// groupVendorsByTown groups vendors by their town location
func groupVendorsByTown(vendors []npc.ID) map[area.ID][]npc.ID {
	grouped := make(map[area.ID][]npc.ID)
	
	for _, vendorID := range vendors {
		townArea, found := action.VendorLocationMap[vendorID]
		if !found {
			continue
		}
		grouped[townArea] = append(grouped[townArea], vendorID)
	}
	
	return grouped
}

// NewShoppingRun creates a shopping run from character config
func NewShoppingRun() Run {
	ctx := context.Get()
	
	// Load shopping rules from the pickit directory
	shoppingRules, err := action.LoadShoppingRules(ctx.CharacterCfg.ConfigFolderName)
	if err != nil {
		ctx.Logger.Warn("Failed to load shopping rules, shopping will be disabled", slog.Any("error", err))
		shoppingRules = nil
	}
	
	return Shopping{
		ShoppingConfig: action.ShopConfig{
			Enabled:         ctx.CharacterCfg.Shopping.Enabled,
			MaxGoldToSpend:  ctx.CharacterCfg.Shopping.MaxGoldToSpend,
			MinGoldReserve:  ctx.CharacterCfg.Shopping.MinGoldReserve,
			RefreshesPerRun: ctx.CharacterCfg.Shopping.RefreshesPerRun,
			VendorsToShop:   ctx.CharacterCfg.Shopping.GetVendorList(),
			ItemTypesToShop: ctx.CharacterCfg.Shopping.ItemTypes,
			ShoppingRules:   shoppingRules,
		},
	}
}