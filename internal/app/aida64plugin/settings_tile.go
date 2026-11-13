package aida64plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"strings"

	"github.com/poizenjam/aida64sd/pkg/graph"
	"github.com/poizenjam/aida64sd/pkg/streamdeck"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// The Settings action (matching hwinfo-streamdeck v3): a key that shows the
// applied refresh interval, and a PI that holds the refresh/INI controls, tile
// appearance, the global threshold library and the connection status.
const settingsAction = "com.poizenjam.aida64sd.settings"

const settingsTitleFontSize = 9.0

// settingsTileSettings is the per-key appearance of a Settings key, stored as
// that key's action settings.
type settingsTileSettings struct {
	TileBackground   string `json:"tileBackground"`   // solid background when ShowLabel is off
	TileTextColor    string `json:"tileTextColor"`    // interval text colour
	ShowLabel        bool   `json:"showLabel"`        // true: draw over the placeholder key art
	Title            string `json:"title"`            // Stream Deck title, drawn in-image
	TitleColor       string `json:"titleColor"`       // title colour
	ShowTitleInGraph *bool  `json:"showTitleInGraph"` // draw the title in the image
}

func defaultSettingsTile() settingsTileSettings {
	return settingsTileSettings{
		TileBackground:   "#000000",
		TileTextColor:    "#ffffff",
		ShowLabel:        true,
		TitleColor:       "#b7b7b7",
		ShowTitleInGraph: boolPtr(true),
	}
}

// decodeSettingsTile decodes a Settings key's action settings. Fields missing
// from raw keep their value from existing (or the defaults), because the PI
// saves only the appearance fields while the title fields come from
// titleParametersDidChange. partial reports that raw lacked a field, so the
// caller can write the completed settings back.
func decodeSettingsTile(raw *json.RawMessage, existing *settingsTileSettings) (ts settingsTileSettings, partial bool) {
	ts = defaultSettingsTile()
	if existing != nil {
		ts = *existing
	}
	if raw == nil || len(*raw) == 0 {
		return ts, true
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(*raw, &fields); err != nil {
		log.Printf("decodeSettingsTile: %v\n", err)
		return ts, true
	}
	var in settingsTileSettings
	if err := json.Unmarshal(*raw, &in); err != nil {
		log.Printf("decodeSettingsTile: %v\n", err)
		return ts, true
	}
	if _, ok := fields["tileBackground"]; ok && in.TileBackground != "" {
		ts.TileBackground = in.TileBackground
	}
	if _, ok := fields["tileTextColor"]; ok && in.TileTextColor != "" {
		ts.TileTextColor = in.TileTextColor
	}
	if _, ok := fields["showLabel"]; ok {
		ts.ShowLabel = in.ShowLabel
	}
	if _, ok := fields["title"]; ok {
		ts.Title = in.Title
	}
	if _, ok := fields["titleColor"]; ok && in.TitleColor != "" {
		ts.TitleColor = in.TitleColor
	}
	if _, ok := fields["showTitleInGraph"]; ok && in.ShowTitleInGraph != nil {
		ts.ShowTitleInGraph = in.ShowTitleInGraph
	}
	for _, k := range []string{"tileBackground", "tileTextColor", "showLabel", "title", "titleColor", "showTitleInGraph"} {
		if _, ok := fields[k]; !ok {
			partial = true
			break
		}
	}
	return ts, partial
}

func (p *Plugin) settingsTile(context string) *settingsTileSettings {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if ts := p.settingsContexts[context]; ts != nil {
		cp := *ts
		return &cp
	}
	return nil
}

func (p *Plugin) storeSettingsTile(context string, ts settingsTileSettings) {
	p.mu.Lock()
	p.settingsContexts[context] = &ts
	p.mu.Unlock()
}

func (p *Plugin) handleSettingsWillAppear(event *streamdeck.EvWillAppear) {
	ts, partial := decodeSettingsTile(event.Payload.Settings, p.settingsTile(event.Context))
	p.storeSettingsTile(event.Context, ts)
	if partial {
		if err := p.sd.SetSettings(event.Context, &ts); err != nil {
			log.Printf("settings WillAppear SetSettings: %v\n", err)
		}
	}
	p.updateSettingsTile(event.Context)
}

func (p *Plugin) handleSettingsWillDisappear(event *streamdeck.EvWillDisappear) {
	p.mu.Lock()
	delete(p.settingsContexts, event.Context)
	delete(p.piContexts, event.Context)
	p.mu.Unlock()
}

func (p *Plugin) handleSettingsDidReceiveSettings(event *streamdeck.EvDidReceiveSettings) {
	ts, partial := decodeSettingsTile(event.Payload.Settings, p.settingsTile(event.Context))
	p.storeSettingsTile(event.Context, ts)
	if partial {
		if err := p.sd.SetSettings(event.Context, &ts); err != nil {
			log.Printf("settings DidReceiveSettings SetSettings: %v\n", err)
		}
	}
	p.updateSettingsTile(event.Context)
}

// handleSettingsTitleParametersDidChange draws the Stream Deck title in-image
// (like the sensor tiles) when the native title is hidden.
func (p *Plugin) handleSettingsTitleParametersDidChange(event *streamdeck.EvTitleParametersDidChange) {
	ts := defaultSettingsTile()
	if existing := p.settingsTile(event.Context); existing != nil {
		ts = *existing
	}
	ts.Title = event.Payload.Title
	if event.Payload.TitleParameters.TitleColor != "" {
		ts.TitleColor = event.Payload.TitleParameters.TitleColor
	}
	ts.ShowTitleInGraph = boolPtr(!event.Payload.TitleParameters.ShowTitle)
	p.storeSettingsTile(event.Context, ts)
	if err := p.sd.SetSettings(event.Context, &ts); err != nil {
		log.Printf("settings TitleParametersDidChange SetSettings: %v\n", err)
	}
	p.updateSettingsTile(event.Context)
}

// handleSettingsAppearance applies an updateTileAppearance command from the
// Settings PI (it also saves the same fields with setSettings; this is the
// immediate-redraw path).
func (p *Plugin) handleSettingsAppearance(context string, raw *json.RawMessage) {
	ts, _ := decodeSettingsTile(raw, p.settingsTile(context))
	p.storeSettingsTile(context, ts)
	p.updateSettingsTile(context)
}

// handleSettingsPropertyInspectorConnected registers the Settings PI for live
// status pushes and sends it the current status and threshold library.
func (p *Plugin) handleSettingsPropertyInspectorConnected(event *streamdeck.EvSendToPlugin) {
	p.mu.Lock()
	p.piContexts[event.Context] = event.Action
	p.mu.Unlock()
	p.sendStatusToPIs()
	p.sendGlobalThresholdsTo(event.Action, event.Context)
}

func (p *Plugin) sendGlobalThresholdsTo(action, context string) {
	p.mu.RLock()
	globals := make([]Threshold, len(p.globalSettings.GlobalThresholds))
	copy(globals, p.globalSettings.GlobalThresholds)
	p.mu.RUnlock()
	if err := p.sd.SendToPropertyInspector(action, context, map[string]interface{}{"globalThresholds": globals}); err != nil {
		log.Printf("sendGlobalThresholdsTo: %v\n", err)
	}
}

// updateAllSettingsTiles redraws every Settings key. Called from
// sendStatusToPIs, which runs on every interval, auto-refresh and connection
// change.
func (p *Plugin) updateAllSettingsTiles() {
	p.mu.RLock()
	contexts := make([]string, 0, len(p.settingsContexts))
	for ctx := range p.settingsContexts {
		contexts = append(contexts, ctx)
	}
	p.mu.RUnlock()
	for _, ctx := range contexts {
		p.updateSettingsTile(ctx)
	}
}

func (p *Plugin) updateSettingsTile(context string) {
	ts := p.settingsTile(context)
	if ts == nil {
		d := defaultSettingsTile()
		ts = &d
	}
	_, _, _, auto, _ := p.piStatusSnapshot()
	intervalMs := int(p.am.GetInterval().Milliseconds())

	// The title and interval are drawn in the image; keep the native title
	// empty so Stream Deck does not draw a second, misaligned copy.
	if err := p.sd.SetTitle(context, ""); err != nil {
		log.Printf("updateSettingsTile SetTitle: %v\n", err)
	}
	img, err := renderSettingsTile(*ts, p.placeholderImage, intervalMs, auto)
	if err != nil {
		log.Printf("updateSettingsTile render: %v\n", err)
		return
	}
	if err := p.sd.SetImage(context, img); err != nil {
		log.Printf("updateSettingsTile SetImage: %v\n", err)
	}
}

// renderSettingsTile draws a Settings key: the title at the sensor tiles'
// title baseline (19), the applied interval on the value line (44) and, while
// auto refresh is on, "auto" on the second line (56). With ShowLabel the
// placeholder key art is the background, as in hwinfo-streamdeck; otherwise a
// solid TileBackground.
func renderSettingsTile(ts settingsTileSettings, placeholder []byte, intervalMs int, auto bool) ([]byte, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, tileWidth, tileHeight))
	drewArt := false
	if ts.ShowLabel && len(placeholder) > 0 {
		if base, err := png.Decode(bytes.NewReader(placeholder)); err == nil {
			draw.Draw(canvas, canvas.Bounds(), base, base.Bounds().Min, draw.Src)
			drewArt = true
		}
	}
	if !drewArt {
		bg := hexToRGBA(ts.TileBackground)
		if bg == nil {
			bg = &color.RGBA{0, 0, 0, 255}
		}
		draw.Draw(canvas, canvas.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	}

	textClr := hexToRGBA(ts.TileTextColor)
	if textClr == nil {
		textClr = &color.RGBA{255, 255, 255, 255}
	}
	titleClr := hexToRGBA(ts.TitleColor)
	if titleClr == nil {
		titleClr = &color.RGBA{183, 183, 183, 255}
	}

	ffm := graph.GetSharedFontFaceManager()
	faceValue, err := ffm.GetFaceOfSize(10.5)
	if err != nil {
		return nil, fmt.Errorf("font value: %w", err)
	}
	faceSmall, err := ffm.GetFaceOfSize(settingsTitleFontSize)
	if err != nil {
		return nil, fmt.Errorf("font title: %w", err)
	}

	// Over the key art, outline the text in the art's field colour (like the
	// sensor tiles' text stroke) so it stays legible across the outline box
	// and the graph.
	text := func(face font.Face, clr *color.RGBA, s string, baseline int) {
		if drewArt {
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if dx != 0 || dy != 0 {
						drawTextAt(canvas, face, settingsArtStroke, s, dx, baseline+dy)
					}
				}
			}
		}
		drawCenteredText(canvas, face, clr, s, baseline)
	}
	if ts.ShowTitleInGraph == nil || *ts.ShowTitleInGraph {
		title := strings.TrimSpace(ts.Title)
		if title == "" {
			title = "Refresh Rate"
		}
		text(faceSmall, titleClr, title, 19)
	}
	text(faceValue, textClr, fmt.Sprintf("%dms", intervalMs), 44)
	if auto {
		text(faceSmall, titleClr, "auto", 56)
	}

	var out bytes.Buffer
	if err := png.Encode(&out, canvas); err != nil {
		return nil, fmt.Errorf("encode settings tile: %w", err)
	}
	return out.Bytes(), nil
}

// settingsArtStroke is the key art's field colour (art/make_icons.py FIELD).
var settingsArtStroke = &color.RGBA{0x17, 0x0A, 0x0B, 255}

// drawTextAt draws s centred like drawCenteredText, shifted by dx pixels.
func drawTextAt(dst *image.RGBA, face font.Face, clr *color.RGBA, s string, dx, baselineY int) {
	d := &font.Drawer{Dst: dst, Src: image.NewUniform(clr), Face: face}
	x := (dst.Bounds().Dx()-d.MeasureString(s).Round())/2 + dx
	if x < 0 {
		x = 0
	}
	d.Dot = fixed.P(x, baselineY)
	d.DrawString(s)
}
