package aida64plugin

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	service "github.com/poizenjam/aida64sd/pkg/service"
)

// decodeActionSettings decodes a reading-tile's persisted settings. AIDA64
// reading ids are plain strings (e.g. "TCPU"), so a direct unmarshal is the
// whole decode; the legacy Warning/Critical threshold migration and snooze
// normalization still run because older tile settings can carry them.
func decodeActionSettings(raw *json.RawMessage) (actionSettings, bool, error) {
	var settings actionSettings
	if raw == nil || *raw == nil {
		return settings, false, nil
	}
	if err := json.Unmarshal(*raw, &settings); err != nil {
		return settings, false, err
	}

	migrated := migrateToThresholds(&settings)
	if normalized := normalizeThresholdSnoozeDurations(settings.SnoozeDurations); !sameIntSlice(normalized, settings.SnoozeDurations) {
		settings.SnoozeDurations = normalized
		migrated = true
	}

	return settings, migrated, nil
}

func syncSettingsWithReadings(settings *actionSettings, readings []service.Reading) bool {
	changed := false
	if settings.ReadingID != "" {
		for _, r := range readings {
			if r.ID() == settings.ReadingID {
				if settings.ReadingLabel == "" {
					settings.ReadingLabel = r.Label()
					changed = true
				}
				if !settings.IsValid {
					settings.IsValid = true
					changed = true
				}
				return changed
			}
		}
	}
	if settings.ReadingLabel != "" {
		for _, r := range readings {
			if r.Label() == settings.ReadingLabel {
				if settings.ReadingID != r.ID() {
					settings.ReadingID = r.ID()
					changed = true
				}
				if !settings.IsValid {
					settings.IsValid = true
					changed = true
				}
				return changed
			}
		}
	}
	return changed
}

// migrateToThresholds converts legacy Warning/Critical threshold settings to the new dynamic threshold array
func migrateToThresholds(settings *actionSettings) bool {
	if len(settings.Thresholds) > 0 {
		return false // Already has thresholds, no migration needed
	}

	migrated := false

	// Migrate Warning threshold
	if settings.WarningEnabled || settings.WarningValue != 0 || settings.WarningOperator != "" {
		settings.Thresholds = append(settings.Thresholds, Threshold{
			ID:              fmt.Sprintf("threshold_%d", time.Now().UnixNano()),
			Name:            "Warning",
			Enabled:         settings.WarningEnabled,
			Operator:        defaultOperator(settings.WarningOperator),
			Value:           settings.WarningValue,
			BackgroundColor: defaultColor(settings.WarningBackgroundColor, "#333300"),
			ForegroundColor: defaultColor(settings.WarningForegroundColor, "#999900"),
			HighlightColor:  defaultColor(settings.WarningHighlightColor, "#ffff00"),
			ValueTextColor:  defaultColor(settings.WarningValueTextColor, "#ffff00"),
			TextColor:       defaultColor(settings.WarningValueTextColor, "#ffff00"),
		})
		migrated = true
	}

	// Migrate Critical threshold
	if settings.CriticalEnabled || settings.CriticalValue != 0 || settings.CriticalOperator != "" {
		settings.Thresholds = append(settings.Thresholds, Threshold{
			ID:              fmt.Sprintf("threshold_%d", time.Now().UnixNano()+1),
			Name:            "Critical",
			Enabled:         settings.CriticalEnabled,
			Operator:        defaultOperator(settings.CriticalOperator),
			Value:           settings.CriticalValue,
			BackgroundColor: defaultColor(settings.CriticalBackgroundColor, "#660000"),
			ForegroundColor: defaultColor(settings.CriticalForegroundColor, "#990000"),
			HighlightColor:  defaultColor(settings.CriticalHighlightColor, "#ff3333"),
			ValueTextColor:  defaultColor(settings.CriticalValueTextColor, "#ff0000"),
			TextColor:       defaultColor(settings.CriticalValueTextColor, "#ff0000"),
		})
		migrated = true
	}

	// Clear legacy fields after migration
	if migrated {
		// Map old CurrentAlertState to new CurrentThresholdID
		if settings.CurrentAlertState != "" && settings.CurrentAlertState != "none" {
			for _, t := range settings.Thresholds {
				if strings.EqualFold(t.Name, settings.CurrentAlertState) {
					settings.CurrentThresholdID = t.ID
					break
				}
			}
		}
		// Clear legacy fields
		settings.WarningEnabled = false
		settings.WarningOperator = ""
		settings.WarningValue = 0
		settings.WarningBackgroundColor = ""
		settings.WarningForegroundColor = ""
		settings.WarningHighlightColor = ""
		settings.WarningValueTextColor = ""
		settings.CriticalEnabled = false
		settings.CriticalOperator = ""
		settings.CriticalValue = 0
		settings.CriticalBackgroundColor = ""
		settings.CriticalForegroundColor = ""
		settings.CriticalHighlightColor = ""
		settings.CriticalValueTextColor = ""
		settings.CurrentAlertState = ""
	}

	return migrated
}

func defaultOperator(op string) string {
	if op == "" {
		return ">="
	}
	return op
}

func defaultColor(color, defaultVal string) string {
	if color == "" {
		return defaultVal
	}
	return color
}
