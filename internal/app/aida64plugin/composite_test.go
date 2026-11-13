package aida64plugin

import (
	"encoding/json"
	"testing"
)

func TestEffectiveCompositeSlotMode(t *testing.T) {
	tests := []struct {
		name     string
		tileMode string
		slotMode string
		want     string
	}{
		{name: "empty slot mode inherits tile mode", tileMode: compositeModeBoth, slotMode: "", want: compositeModeBoth},
		{name: "slot text overrides tile graph", tileMode: compositeModeGraph, slotMode: compositeModeText, want: compositeModeText},
		{name: "invalid slot mode inherits tile mode", tileMode: compositeModeGraph, slotMode: "bogus", want: compositeModeGraph},
		{name: "invalid tile mode falls back to text", tileMode: "bogus", slotMode: "", want: compositeModeText},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveCompositeSlotMode(tt.tileMode, tt.slotMode); got != tt.want {
				t.Fatalf("effectiveCompositeSlotMode(%q, %q) = %q, want %q", tt.tileMode, tt.slotMode, got, tt.want)
			}
		})
	}
}

func TestCopyCompositeStylingCopiesStylingNotIdentity(t *testing.T) {
	falseVal := false
	settings := &compositeActionSettings{
		SlotCount: 4,
		Slots: [4]compositeSlotSettings{
			// slot 0: the source, fully styled.
			{SensorUID: "temp", ReadingID: "TCPU", ReadingLabel: "CPU Package", IsValid: true,
				Mode: compositeModeGraph, HighlightColor: "#ff0000", FillAlpha: 30,
				Min: 0, Max: 120, AutoRange: &falseVal, GraphHeightPct: 80, Format: "%.1f"},
			// slot 1: different identity + its own styling that must be overwritten.
			{SensorUID: "fan", ReadingID: "FCPU", ReadingLabel: "CPU Fan", IsValid: true,
				Mode: compositeModeText, HighlightColor: "#00ff00", FillAlpha: 100, Format: "%.0f"},
			{SensorUID: "volt", ReadingID: "VCPU", ReadingLabel: "VCore"},
			{SensorUID: "pwr", ReadingID: "PGPU1", ReadingLabel: "GPU Power"},
		},
	}

	count := copyCompositeStyling(settings)
	if count != 4 {
		t.Fatalf("count = %d, want 4", count)
	}

	// Styling copied from slot 0 to every other slot.
	for i := 1; i < 4; i++ {
		s := &settings.Slots[i]
		if s.Mode != compositeModeGraph || s.HighlightColor != "#ff0000" || s.FillAlpha != 30 {
			t.Fatalf("slot %d styling not copied: mode=%q highlight=%q fillAlpha=%d", i, s.Mode, s.HighlightColor, s.FillAlpha)
		}
		if s.Min != 0 || s.Max != 120 || s.GraphHeightPct != 80 || s.Format != "%.1f" {
			t.Fatalf("slot %d scaling/format not copied", i)
		}
		if s.AutoRange == nil || *s.AutoRange != false {
			t.Fatalf("slot %d autoRange not copied (want false, not aliasing slot0)", i)
		}
	}

	// Identity (sensor, reading, label) must be untouched.
	if settings.Slots[1].SensorUID != "fan" || settings.Slots[1].ReadingID != "FCPU" || settings.Slots[1].ReadingLabel != "CPU Fan" {
		t.Fatalf("slot 1 identity was overwritten: %+v", settings.Slots[1])
	}
	if settings.Slots[3].SensorUID != "pwr" || settings.Slots[3].ReadingID != "PGPU1" {
		t.Fatalf("slot 3 identity was overwritten: %+v", settings.Slots[3])
	}
}

func TestDecodeCompositeSettingsKeepsSlotMode(t *testing.T) {
	raw := json.RawMessage(`{
		"slotCount": 2,
		"mode": "both",
		"slots": [
			{"mode": "text"},
			{"mode": "graph"}
		]
	}`)

	settings, err := decodeCompositeSettings(&raw)
	if err != nil {
		t.Fatalf("decodeCompositeSettings: %v", err)
	}
	if settings.Slots[0].Mode != compositeModeText {
		t.Fatalf("slot 0 mode = %q, want %q", settings.Slots[0].Mode, compositeModeText)
	}
	if settings.Slots[1].Mode != compositeModeGraph {
		t.Fatalf("slot 1 mode = %q, want %q", settings.Slots[1].Mode, compositeModeGraph)
	}
}

func TestRenderCompositeTileHonorsPerSlotGraphMode(t *testing.T) {
	settings := compositeActionSettings{
		SlotCount: 2,
		Mode:      compositeModeText,
		Slots: [4]compositeSlotSettings{
			{
				Mode:            compositeModeGraph,
				ForegroundColor: "#000000",
				HighlightColor:  "#ff0000",
				BackgroundColor: "#000000",
				ValueTextColor:  "#ffffff",
				TitleColor:      "#ffffff",
				FillAlpha:       100,
				Min:             0,
				Max:             100,
			},
			{
				Mode:            compositeModeText,
				ForegroundColor: "#000000",
				HighlightColor:  "#00ff00",
				BackgroundColor: "#000000",
				ValueTextColor:  "#ffffff",
				TitleColor:      "#ffffff",
				FillAlpha:       100,
				Min:             0,
				Max:             100,
			},
		},
	}
	state := &compositeState{graphs: initCompositeGraphs(&settings)}
	state.graphs[0].Update(100)
	state.graphs[1].Update(100)

	imgBytes, err := renderCompositeTile(&settings, state, [4]string{}, [4]*Threshold{})
	if err != nil {
		t.Fatalf("renderCompositeTile: %v", err)
	}
	img, err := decodePNGToRGBA(imgBytes)
	if err != nil {
		t.Fatalf("decodePNGToRGBA: %v", err)
	}

	redPixels := 0
	greenPixels := 0
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if p.R > 0 {
				redPixels++
			}
			if p.G > 0 {
				greenPixels++
			}
		}
	}

	if redPixels == 0 {
		t.Fatal("graph-only slot did not render any red graph pixels")
	}
	if greenPixels != 0 {
		t.Fatalf("text-only slot rendered %d green graph pixels", greenPixels)
	}
}
