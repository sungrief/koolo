package action

import (
	"fmt"

	"github.com/hectorgimenez/d2go/pkg/data/area"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/koolo/internal/context"
)

// VendorLocationMap maps vendors to their town areas
var VendorLocationMap = map[npc.ID]area.ID{
	// Act 1 - Rogue Encampment
	npc.Akara:        area.RogueEncampment,
	npc.Charsi:       area.RogueEncampment,
	npc.Gheed:        area.RogueEncampment,
	npc.Kashya:       area.RogueEncampment,
	npc.Warriv:       area.RogueEncampment,
	npc.DeckardCain5: area.RogueEncampment,

	// Act 2 - Lut Gholein
	npc.Fara:         area.LutGholein,
	npc.Drognan:      area.LutGholein,
	npc.Elzix:        area.LutGholein,
	npc.Greiz:        area.LutGholein,
	npc.Lysander:     area.LutGholein,
	npc.DeckardCain2: area.LutGholein,
	npc.Warriv2:      area.LutGholein,
	npc.Meshif:       area.LutGholein,

	// Act 3 - Kurast Docks
	npc.Alkor:        area.KurastDocks,
	npc.Asheara:      area.KurastDocks,
	npc.Hratli:       area.KurastDocks,
	npc.Ormus:        area.KurastDocks,
	npc.Meshif2:      area.KurastDocks,
	npc.DeckardCain3: area.KurastDocks,

	// Act 4 - Pandemonium Fortress
	npc.Halbu:        area.ThePandemoniumFortress,
	npc.Jamella:      area.ThePandemoniumFortress,
	npc.DeckardCain4: area.ThePandemoniumFortress,
	npc.Tyrael:       area.ThePandemoniumFortress,

	// Act 5 - Harrogath
	npc.Larzuk:       area.Harrogath,
	npc.Malah:        area.Harrogath,
	npc.Drehya:       area.Harrogath, // Anya
	npc.Nihlathak:    area.Harrogath,
	npc.QualKehk:     area.Harrogath,
	npc.DeckardCain6: area.Harrogath,
}

// GetRequiredTownForVendors determines which town is needed to shop at the given vendors
// Returns an error if vendors are in different towns (not supported in a single run)
func GetRequiredTownForVendors(vendors []npc.ID) (area.ID, error) {
	if len(vendors) == 0 {
		return 0, fmt.Errorf("no vendors specified")
	}

	// Get the required town for the first vendor
	requiredTown, found := VendorLocationMap[vendors[0]]
	if !found {
		return 0, fmt.Errorf("vendor %d not found in location map", vendors[0])
	}

	// Verify all vendors are in the same town
	for i, vendor := range vendors {
		if i == 0 {
			continue // Skip first vendor, already checked
		}

		vendorTown, found := VendorLocationMap[vendor]
		if !found {
			return 0, fmt.Errorf("vendor %d not found in location map", vendor)
		}

		if vendorTown != requiredTown {
			return 0, fmt.Errorf("shopping run contains vendors from different towns: %s and %s. Please split into separate runs",
				requiredTown.Area().Name, vendorTown.Area().Name)
		}
	}

	return requiredTown, nil
}

// IsVendorInCurrentTown checks if a vendor exists in the player's current area
func IsVendorInCurrentTown(vendorID npc.ID) bool {
	ctx := context.Get()
	currentArea := ctx.Data.PlayerUnit.Area

	requiredTown, found := VendorLocationMap[vendorID]
	if !found {
		return false
	}

	return currentArea == requiredTown
}

// UseWaypoint is a wrapper for the shopping system to use the existing WayPoint function
// This function is required by the shopping bot for vendor refresh
func UseWaypoint(destination area.ID) error {
	// Use your existing WayPoint function
	return WayPoint(destination)
}

// UseWaypointToTown returns to the current act's town using waypoint
// This function is required by the shopping bot for vendor refresh

func UseWaypointToTown() error {
	ctx := context.Get()

	// If already in town, no need to do anything
	if ctx.Data.PlayerUnit.Area.IsTown() {
		return nil
	}

	// Try to identify the current act's town by checking known neighbors
	candidateTowns := []area.ID{
		area.RogueEncampment,
		area.LutGholein,
		area.KurastDocks,
		area.ThePandemoniumFortress,
		area.Harrogath,
	}

	for _, town := range candidateTowns {
		if err := WayPoint(town); err == nil {
			return nil
		}
	}

	// Fallback to ReturnTown if none of the waypoints succeeded
	return ReturnTown()
}
