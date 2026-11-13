package aida64plugin

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"

	"golang.org/x/image/font"

	"github.com/poizenjam/aida64sd/pkg/graph"
)

// A configured Sensor, Composite or Derived key shows the disconnected image
// while AIDA64 is closed or its shared memory is off: the default key art
// dimmed, with "AIDA64 / not detected" in white. An unconfigured key keeps the
// plain default art, so the two states look different (as in
// hwinfo-streamdeck).

// disconnectedShade is the black layer over the key art (alpha 166 of 255,
// about 65%).
var disconnectedShade = color.NRGBA{0, 0, 0, 166}

func renderDisconnectedImage(art []byte) ([]byte, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, tileWidth, tileHeight))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	if len(art) > 0 {
		if base, err := png.Decode(bytes.NewReader(art)); err == nil {
			draw.Draw(canvas, canvas.Bounds(), base, base.Bounds().Min, draw.Over)
			draw.Draw(canvas, canvas.Bounds(), image.NewUniform(disconnectedShade), image.Point{}, draw.Over)
		}
	}

	ffm := graph.GetSharedFontFaceManager()
	faceName, err := ffm.GetFaceOfSize(10.5)
	if err != nil {
		return nil, fmt.Errorf("font: %w", err)
	}
	faceNote, err := ffm.GetFaceOfSize(settingsTitleFontSize)
	if err != nil {
		return nil, fmt.Errorf("font: %w", err)
	}
	white := &color.RGBA{255, 255, 255, 255}
	stroke := &color.RGBA{0, 0, 0, 255}
	text := func(face font.Face, s string, baseline int) {
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if dx != 0 || dy != 0 {
					drawTextAt(canvas, face, stroke, s, dx, baseline+dy)
				}
			}
		}
		drawCenteredText(canvas, face, white, s, baseline)
	}
	text(faceName, "AIDA64", 34)
	text(faceNote, "not detected", 47)

	var out bytes.Buffer
	if err := png.Encode(&out, canvas); err != nil {
		return nil, fmt.Errorf("encode disconnected image: %w", err)
	}
	return out.Bytes(), nil
}

// markDisconnected records that ctx is disconnected and reports whether this
// is a new outage for the key (so the image must be sent). The record is
// dropped when the key disappears, so a key that reappears during an outage
// (page switch) gets the image again.
func (p *Plugin) markDisconnected(ctx string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.disconnectedShown == nil {
		p.disconnectedShown = make(map[string]bool)
	}
	if p.disconnectedShown[ctx] {
		return false
	}
	p.disconnectedShown[ctx] = true
	return true
}

// clearDisconnected reports whether ctx was showing the disconnected image, in
// which case the caller must redraw it even if nothing else changed.
func (p *Plugin) clearDisconnected(ctx string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.disconnectedShown[ctx] {
		return false
	}
	delete(p.disconnectedShown, ctx)
	return true
}

func (p *Plugin) showDisconnected(ctx string) {
	if !p.markDisconnected(ctx) || len(p.disconnectedImage) == 0 {
		return
	}
	if err := p.sd.SetImage(ctx, p.disconnectedImage); err != nil {
		log.Printf("showDisconnected SetImage: %v", err)
	}
}

// compositeConfigured: at least one active slot has a reading.
func compositeConfigured(s *compositeActionSettings) bool {
	for i := 0; i < s.SlotCount && i < len(s.Slots); i++ {
		if s.Slots[i].IsValid && s.Slots[i].SensorUID != "" {
			return true
		}
	}
	return false
}

// derivedConfigured: at least one active slot has a reading.
func derivedConfigured(s *derivedActionSettings) bool {
	for i := 0; i < s.SlotCount && i < len(s.Slots); i++ {
		if s.Slots[i].IsValid && s.Slots[i].SensorUID != "" {
			return true
		}
	}
	return false
}
