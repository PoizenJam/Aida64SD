package aida64plugin

import (
	"bytes"
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/poizenjam/aida64sd/pkg/streamdeck"
)

func rawJSON(s string) *json.RawMessage {
	r := json.RawMessage(s)
	return &r
}

// The PI saves only the appearance fields; the title fields come from
// titleParametersDidChange. A partial save must keep the existing title.
func TestDecodeSettingsTileKeepsMissingFields(t *testing.T) {
	existing := defaultSettingsTile()
	existing.Title = "Rate"
	existing.TitleColor = "#123456"
	existing.ShowTitleInGraph = boolPtr(false)

	ts, partial := decodeSettingsTile(rawJSON(`{"tileBackground":"#202020","tileTextColor":"#ff0000","showLabel":false}`), &existing)
	if !partial {
		t.Error("partial = false for a payload without title fields")
	}
	if ts.TileBackground != "#202020" || ts.TileTextColor != "#ff0000" || ts.ShowLabel {
		t.Errorf("appearance not applied: %+v", ts)
	}
	if ts.Title != "Rate" || ts.TitleColor != "#123456" || ts.ShowTitleInGraph == nil || *ts.ShowTitleInGraph {
		t.Errorf("title fields not kept: %+v", ts)
	}
}

func TestDecodeSettingsTileDefaults(t *testing.T) {
	ts, partial := decodeSettingsTile(nil, nil)
	if !partial {
		t.Error("partial = false for nil settings")
	}
	if !ts.ShowLabel || ts.TileBackground != "#000000" || ts.TileTextColor != "#ffffff" ||
		ts.TitleColor != "#b7b7b7" || ts.ShowTitleInGraph == nil || !*ts.ShowTitleInGraph {
		t.Errorf("defaults wrong: %+v", ts)
	}
	full := `{"tileBackground":"#000000","tileTextColor":"#ffffff","showLabel":true,"title":"","titleColor":"#b7b7b7","showTitleInGraph":true}`
	if _, partial := decodeSettingsTile(rawJSON(full), nil); partial {
		t.Error("partial = true for a complete payload")
	}
}

// renderSettingsTile must produce a 72x72 key both over the placeholder art
// and on a solid background. Set AIDA64SD_DUMP=dir to write the PNGs.
func TestRenderSettingsTile(t *testing.T) {
	dump := os.Getenv("AIDA64SD_DUMP")
	if dump != "" {
		if abs, err := filepath.Abs(dump); err == nil {
			dump = abs
		}
	}
	// The font loads from the working directory (the plugin folder at runtime).
	t.Chdir(filepath.Join("..", "..", "..", "com.poizenjam.aida64sd.sdPlugin"))
	art, err := os.ReadFile("settingsImage.png")
	if err != nil {
		t.Fatalf("read settingsImage.png: %v", err)
	}
	solid := defaultSettingsTile()
	solid.ShowLabel = false
	solid.TileBackground = "#101820"
	cases := map[string]struct {
		ts   settingsTileSettings
		ms   int
		auto bool
	}{
		"art-auto":     {defaultSettingsTile(), 1000, true},
		"art-manual":   {defaultSettingsTile(), 2000, false},
		"solid-manual": {solid, 500, false},
	}
	for name, c := range cases {
		b, err := renderSettingsTile(c.ts, art, c.ms, c.auto)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		if img.Bounds().Dx() != tileWidth || img.Bounds().Dy() != tileHeight {
			t.Fatalf("%s: size %v, want %dx%d", name, img.Bounds(), tileWidth, tileHeight)
		}
		if dump != "" {
			_ = os.WriteFile(filepath.Join(dump, "settings-"+name+".png"), b, 0o644)
		}
	}
	// A broken placeholder falls back to the solid background, not an error.
	if _, err := renderSettingsTile(defaultSettingsTile(), []byte("not a png"), 1000, false); err != nil {
		t.Fatalf("broken placeholder: %v", err)
	}
}

// Removing a Settings key forgets its appearance and stops status pushes to
// its PI.
func TestSettingsWillDisappearPrunes(t *testing.T) {
	ts := defaultSettingsTile()
	p := &Plugin{
		settingsContexts: map[string]*settingsTileSettings{"k": &ts},
		piContexts:       map[string]string{"k": settingsAction, "other": "com.poizenjam.aida64sd.reading"},
	}
	p.handleSettingsWillDisappear(&streamdeck.EvWillDisappear{EvWillAppear: streamdeck.EvWillAppear{Action: settingsAction, Context: "k"}})
	if _, ok := p.settingsContexts["k"]; ok {
		t.Error("settings context kept")
	}
	if _, ok := p.piContexts["k"]; ok {
		t.Error("PI context kept")
	}
	if _, ok := p.piContexts["other"]; !ok {
		t.Error("unrelated PI context pruned")
	}
}

// Settings commands route on payload shape, whichever PI sends them.
func TestIsSettingsPayloadIncludesSettingsCommands(t *testing.T) {
	for _, k := range []string{"updateTileAppearance", "requestSettingsStatus", "setPollInterval", "addGlobalThreshold"} {
		if !isSettingsPayload(map[string]*json.RawMessage{k: rawJSON(`true`)}) {
			t.Errorf("%s not routed as a settings command", k)
		}
	}
}
