package service

import (
	"fmt"
	"sort"
	"strings"
)

// AIDA64 is local-machine only (unlike HWiNFO, which also exposes up to 8
// remote shared-memory mappings), so the source model is a single local
// source. LocalSourceID is "" to match the HWiNFO plugin's convention, and
// sensor UIDs are plain AIDA64 ids (no "source::" prefix). The Source
// contract is kept because the plugin's staleness gate and status display
// are written against it.

// LocalSourceID identifies the local machine's shared memory.
const LocalSourceID = ""

// LocalSourceName is the user-facing name of the local source.
const LocalSourceName = "Local"

// CategoryNames maps AIDA64 category tags to display names for the tile
// picker. This is the "sensor group" layer of the sensor/reading model:
// a category (temp/volt/...) is a sensor, a tag (TCPU, PCPUPKG, ...) is a
// reading.
var CategoryNames = map[string]string{
	"temp": "Temperatures",
	"volt": "Voltages",
	"fan":  "Fan Speeds",
	"duty": "Fan Duty Cycles",
	"curr": "Currents",
	"pwr":  "Power",
	"sys":  "System",
}

// CategoryName returns the display name for a category tag, falling back to
// the tag itself for a category AIDA64 may add in a future version.
func CategoryName(category string) string {
	if name, ok := CategoryNames[category]; ok {
		return name
	}
	return strings.ToUpper(category)
}

// CategoryOrder is the fixed display order of the sensor groups in the tile
// picker. Categories not listed here (future AIDA64 tags) sort after the
// known ones, alphabetically.
var CategoryOrder = []string{"temp", "volt", "fan", "duty", "curr", "pwr", "sys"}

// SortCategories orders category ids for stable display: known categories in
// CategoryOrder first, then unknown ones alphabetically.
func SortCategories(ids []string) {
	known := map[string]int{}
	for i, c := range CategoryOrder {
		known[c] = i
	}
	sort.SliceStable(ids, func(i, j int) bool {
		a, okA := known[ids[i]]
		b, okB := known[ids[j]]
		switch {
		case okA && okB:
			return a < b
		case okA:
			return true
		case okB:
			return false
		default:
			return ids[i] < ids[j]
		}
	})
}

// SourceDisplayName is the user-facing name of a source id.
func SourceDisplayName(sourceID string) string {
	if sourceID == LocalSourceID {
		return LocalSourceName
	}
	return fmt.Sprintf("Source %q", sourceID)
}
