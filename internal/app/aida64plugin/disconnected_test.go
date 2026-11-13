package aida64plugin

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func decodeTestPNG(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return img
}

func luma(img image.Image, x, y int) uint32 {
	r, g, b, _ := img.At(x, y).RGBA()
	return (r*299 + g*587 + b*114) / 1000 >> 8
}

func TestRenderDisconnectedImage(t *testing.T) {
	t.Chdir(filepath.Join("..", "..", "..", "com.poizenjam.aida64sd.sdPlugin"))
	art, err := os.ReadFile("defaultImage.png")
	if err != nil {
		t.Fatalf("read art: %v", err)
	}
	out, err := renderDisconnectedImage(art)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if dir := os.Getenv("AIDA64SD_DUMP"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, "disconnected.png"), out, 0o644)
	}
	img := decodeTestPNG(t, out)
	if b := img.Bounds(); b.Dx() != tileWidth || b.Dy() != tileHeight {
		t.Fatalf("size = %v, want %dx%d", b, tileWidth, tileHeight)
	}

	// The art is dimmed: its brightest pixel outside the text band gets darker.
	base := decodeTestPNG(t, art)
	var artMax, outMax uint32
	for y := 0; y < tileHeight; y++ {
		if y >= 22 && y <= 50 {
			continue // text band
		}
		for x := 0; x < tileWidth; x++ {
			if v := luma(base, x, y); v > artMax {
				artMax = v
			}
			if v := luma(img, x, y); v > outMax {
				outMax = v
			}
		}
	}
	if outMax*2 > artMax {
		t.Errorf("art not dimmed: brightest %d, original %d", outMax, artMax)
	}

	// Both text lines are drawn in white.
	for _, band := range [][2]int{{24, 34}, {39, 47}} {
		white := 0
		for y := band[0]; y <= band[1]; y++ {
			for x := 0; x < tileWidth; x++ {
				if luma(img, x, y) > 240 {
					white++
				}
			}
		}
		if white < 20 {
			t.Errorf("rows %d-%d: %d white pixels, want text", band[0], band[1], white)
		}
	}
}

func TestDisconnectedMarkAndClear(t *testing.T) {
	p := &Plugin{}
	if p.clearDisconnected("k") {
		t.Fatal("clear on a connected key reported a change")
	}
	if !p.markDisconnected("k") {
		t.Fatal("first mark must ask for the image")
	}
	if p.markDisconnected("k") {
		t.Fatal("second mark in the same outage must not resend the image")
	}
	if !p.clearDisconnected("k") {
		t.Fatal("clear after an outage must ask for a redraw")
	}
	if !p.markDisconnected("k") {
		t.Fatal("a new outage must send the image again")
	}
}

func TestCompositeAndDerivedShowDisconnectedOnlyWhenConfigured(t *testing.T) {
	down := &bridgeRuntime{hw: stubHardwareService{pollErr: errors.New("AIDA64 closed")}}

	configured := compositeActionSettings{SlotCount: 2}
	configured.Slots[1] = compositeSlotSettings{SensorUID: "temp", ReadingID: "TCPU", IsValid: true}
	empty := compositeActionSettings{SlotCount: 2}

	dConfigured := derivedActionSettings{SlotCount: 2}
	dConfigured.Slots[0] = derivedSlotSettings{SensorUID: "temp", ReadingID: "TCPU", IsValid: true}
	dEmpty := derivedActionSettings{SlotCount: 2}

	p := &Plugin{
		bridge:            down,
		compositeSettings: map[string]*compositeActionSettings{"c1": &configured, "c2": &empty},
		compositeStates:   map[string]*compositeState{"c1": {}, "c2": {}},
		derivedSettings:   map[string]*derivedActionSettings{"d1": &dConfigured, "d2": &dEmpty},
		derivedStates:     map[string]*derivedState{"d1": {}, "d2": {}},
		thresholdDirty:    map[string]bool{},
	}
	// disconnectedImage is empty, so showDisconnected records the state
	// without writing to Stream Deck.
	p.updateCompositeTile("c1")
	p.updateCompositeTile("c2")
	p.updateDerivedTile("d1")
	p.updateDerivedTile("d2")

	for ctx, want := range map[string]bool{"c1": true, "c2": false, "d1": true, "d2": false} {
		if got := p.disconnectedShown[ctx]; got != want {
			t.Errorf("%s disconnected = %v, want %v", ctx, got, want)
		}
	}
}
