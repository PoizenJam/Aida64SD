package aida64plugin

import (
	"encoding/json"
	"testing"
)

// TestDecodeActionSettingsStringReadingID guards that AIDA64 tile settings
// decode cleanly: reading ids are plain strings (e.g. "TCPU"), the sensor is a
// category tag, and nothing triggers migration.
func TestDecodeActionSettingsStringReadingID(t *testing.T) {
	legacy := json.RawMessage(`{
		"sensorUid": "temp",
		"readingId": "TCPU",
		"title": "CPU",
		"titleFontSize": 10.5,
		"valueFontSize": 10.5,
		"min": 0,
		"max": 120,
		"format": "",
		"divisor": "",
		"isValid": true,
		"titleColor": "#b7b7b7",
		"foregroundColor": "#005128",
		"backgroundColor": "#000000",
		"highlightColor": "#009e00",
		"valueTextColor": "#ffffff",
		"inErrorState": false
	}`)

	settings, migrated, err := decodeActionSettings(&legacy)
	if err != nil {
		t.Fatalf("decodeActionSettings: %v", err)
	}
	if migrated {
		t.Fatalf("plain string settings must not trigger migration")
	}
	if settings.SensorUID != "temp" {
		t.Errorf("SensorUID = %q, want %q", settings.SensorUID, "temp")
	}
	if settings.ReadingID != "TCPU" {
		t.Errorf("ReadingID = %q, want %q", settings.ReadingID, "TCPU")
	}
	if !settings.IsValid {
		t.Errorf("IsValid = false, want true")
	}
	if settings.Min != 0 || settings.Max != 120 {
		t.Errorf("min/max = %d/%d, want 0/120", settings.Min, settings.Max)
	}
}

// TestDecodeActionSettingsMigratesLegacyWarningCritical guards the upgrade
// path from the old single Warning/Critical threshold fields to the dynamic
// Threshold array, mirroring the reference behaviour.
func TestDecodeActionSettingsMigratesLegacyWarningCritical(t *testing.T) {
	legacy := json.RawMessage(`{
		"sensorUid": "pwr",
		"readingId": "PCPUPKG",
		"isValid": true,
		"warningEnabled": true,
		"warningOperator": ">",
		"warningValue": 150,
		"criticalEnabled": true,
		"criticalOperator": ">",
		"criticalValue": 250
	}`)

	settings, migrated, err := decodeActionSettings(&legacy)
	if err != nil {
		t.Fatalf("decodeActionSettings: %v", err)
	}
	if !migrated {
		t.Fatalf("expected legacy warning/critical to migrate to thresholds")
	}
	if len(settings.Thresholds) != 2 {
		t.Fatalf("expected 2 migrated thresholds, got %d", len(settings.Thresholds))
	}
	if settings.WarningEnabled || settings.CriticalEnabled {
		t.Fatalf("legacy enabled flags should be cleared after migration")
	}
	// The Warning threshold must carry the old value/operator.
	found := false
	for _, th := range settings.Thresholds {
		if th.Name == "Warning" && th.Value == 150 && th.Operator == ">" && th.Enabled {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a migrated Warning threshold with value 150, got %+v", settings.Thresholds)
	}
}
