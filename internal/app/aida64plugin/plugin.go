package aida64plugin

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"log"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"runtime"

	"github.com/hashicorp/go-plugin"
	"github.com/poizenjam/aida64sd/pkg/graph"
	service "github.com/poizenjam/aida64sd/pkg/service"
	"github.com/poizenjam/aida64sd/pkg/streamdeck"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// bridgeRuntime holds the AIDA64 bridge process and gRPC client.
// Poll time cache is protected by Plugin.mu; c/hw/peg are protected by the runtime's own mu.
type bridgeRuntime struct {
	mu  sync.RWMutex
	c   *plugin.Client
	hw  service.HardwareService
	peg processExitGroup
	// starting is true while a start/restart is in flight, so the watchdog,
	// the liveness sampler and PI connects cannot spawn bridges concurrently.
	starting bool
	// failures counts consecutive failed liveness calls (sampler only).
	failures int
	// poll time cache; accessed under Plugin.mu
	cachedPollTime uint64
	cachedAt       time.Time
}

// Plugin handles information between AIDA64 and Stream Deck
type Plugin struct {
	mu sync.RWMutex // protects maps and cached state below

	bridge *bridgeRuntime
	sd     *streamdeck.StreamDeck
	am     *actionManager
	graphs map[string]*graph.Graph

	// Cached assets and state for performance
	placeholderImage []byte // cached startup chip placeholder image (set once at init, read-only after)
	// disconnectedImage is the dimmed key art with "AIDA64 not detected"
	// (set once at init); disconnectedShown marks keys currently showing it.
	disconnectedImage []byte
	disconnectedShown map[string]bool
	lastPollTime      map[string]uint64    // last processed PollTime per context
	lastRenderTime    map[string]time.Time // wall-time of last render per context (for per-tile interval override)
	smoothedValues    map[string]float64   // last smoothed graph value per context (for EMA)
	autoRangeMax      map[string]int       // observed high-water mark per context for auto-range (key = context, context|slot, dial:ctx|page)
	divisorCache      map[string]divisorCacheEntry
	thresholdStates   map[string]map[string]*thresholdRuntimeState
	thresholdSnoozes  map[string]*thresholdSnoozeState
	thresholdDirty    map[string]bool

	// Global settings
	globalSettings      globalSettings                   // plugin-wide settings (poll interval)
	globalSettingsExtra map[string]json.RawMessage       // global-settings fields owned by other PIs
	pollTimeCacheTTL    time.Duration                    // cache TTL (matches poll interval)
	piContexts          map[string]string                // open PI contexts -> action, for live status pushes
	settingsContexts    map[string]*settingsTileSettings // Settings keys -> appearance

	// Auto refresh reads AIDA64's own sensor update rate from the INI's
	// [Generic] HWMonUpdateFreq* keys. The shared-memory block has no rate
	// field, and measuring its write cadence overestimates the rate on an
	// idle box because AIDA64 skips rewriting unchanged content. iniStatus is
	// the PI readout state: "ok", "not-detected", "no-rate-key", "read-error",
	// "path-missing".
	iniDetectedPath  string    // last path locateAida64Ini found
	iniUsedPath      string    // path the last read attempt used (manual override or detected)
	iniFileMtime     time.Time // mtime of the last read (re-read gate)
	iniFileMtimePath string    // path the mtime belongs to
	iniRateMs        int       // resolved key value (pre-clamp); 0 = none
	iniRateKey       string    // key used (default, override, or fallback)
	iniNote          string    // resolution note surfaced in the PI readout
	iniStatus        string
	lastIniRead      time.Time // last readIniRate attempt (60 s gate)
	startedAt        time.Time // RunForever start; gates the first auto-detected INI read

	// bridgeAvailable mirrors the bridge's "block opened and parsed on the
	// last tick" flag (Source.Available over gRPC). It drives the Connection
	// line instead of the poll token because the bridge outlives AIDA64 and
	// keeps returning the last token after the block is gone. Refreshed by the
	// 1 s sampler; the AIDA64 launch/terminate hooks only clear it early.
	// The poll token still drives the separate 60 s tile staleness window.
	bridgeAvailable bool

	// sensorCount is the category count from the sampler's last successful
	// Sensors call (maybeRepushCatalog), shown in the Connection block. Cached
	// so the status push never calls the bridge itself.
	sensorCount int

	// lastCommandSet is true once the user has run a setPollInterval command.
	// From then on the command owns AutoRefresh and a global-settings blob's
	// autoRefresh is ignored; before it, the startup restore blob is trusted.
	lastCommandSet bool

	// autoDiagnose is a one-shot diagnostic line surfaced in the PI console so
	// the auto-refresh state transitions are observable without host log
	// access. Set by the command path and the global-settings handler; carried
	// in the next status push and cleared after.
	autoDiagnose string

	// Last readings set the sensor PIs were pushed (id -> true). The catalog
	// is otherwise pushed once per PI connect; if the snapshot is empty at
	// that moment (AIDA64 starting up or loading a report) the picker stays
	// blank forever, so the fixed sampler re-pushes it whenever the set
	// changes. Checked once per second (every 4th sample) and compared by
	// size first.
	lastCatalogReadings map[string]bool

	// Composite tile state
	compositeSettings map[string]*compositeActionSettings
	compositeStates   map[string]*compositeState

	// Derived metric tile state
	derivedSettings map[string]*derivedActionSettings
	derivedStates   map[string]*derivedState

	// Stream Deck+ dial carousel state
	dialSettings map[string]*dialActionSettings
	dialStates   map[string]*dialState
}

type sensorResult struct {
	sensors []service.Sensor
	err     error
}

type divisorCacheEntry struct {
	raw   string
	value float64
}

const defaultPollInterval = time.Second

// auxSampleInterval is the fixed period of the auxiliary sampler
// (runAuxSampler): sensor catalog re-push and, every 60 s, an INI
// re-check (mtime-gated, so a steady state costs one Stat per minute).
const auxSampleInterval = 250 * time.Millisecond

// iniRecheckInterval is how often the auto-refresh INI watch re-stats the
// file for an mtime change. AIDA64 rewrites the INI from memory on exit, so
// a change observed here means the user set the rate in the AIDA64 UI while
// both apps were running and closed AIDA64, or edited the file directly.
const iniRecheckInterval = 60 * time.Second

// iniInitialReadDelay gives AIDA64 time to start (its block and INI appear
// together) before the first auto INI read, so the first read is not a
// spurious not-detected state during AIDA64's own boot.
const iniInitialReadDelay = 3 * time.Second

// iniStatus values surfaced to the PIs and logged.
const (
	iniStatusOK          = "ok"
	iniStatusNotDetected = "not-detected"
	iniStatusNoRateKey   = "no-rate-key"
	iniStatusReadError   = "read-error"
	iniStatusPathMissing = "path-missing"
)

const defaultThresholdHysteresis = 1.0
const defaultThresholdDwellMs = int(defaultPollInterval / time.Millisecond)
const defaultThresholdCooldownMs = int((5 * defaultPollInterval) / time.Millisecond)

func pollTimeCacheTTLForInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		interval = defaultPollInterval
	}

	// Keep cache short enough so each ticker cycle fetches fresh PollTime,
	// while still allowing one shared value across actions in the same tick.
	ttl := interval / 2
	if ttl < 100*time.Millisecond {
		ttl = 100 * time.Millisecond
	}
	if ttl >= interval {
		ttl = interval - (25 * time.Millisecond)
		if ttl < 25*time.Millisecond {
			ttl = 25 * time.Millisecond
		}
	}
	return ttl
}

// bridgeBinaryName returns the sensor bridge binary. Windows runs the real
// AIDA64 shared-memory bridge; other platforms exist only for the test
// harness, which ships a mock bridge serving the same gRPC interface.
func bridgeBinaryName() string {
	if runtime.GOOS == "windows" {
		return "./aida64-bridge.exe"
	}
	return "./mock-bridge"
}

// newBridgeClient spawns the bridge and connects to it over go-plugin gRPC.
// It holds no locks: a slow or failing start (go-plugin waits up to a minute
// for the handshake) must not block the readers of rt.mu. On failure the
// half-started process is killed rather than left running.
func newBridgeClient() (*plugin.Client, service.HardwareService, processExitGroup, error) {
	cmd := exec.Command(bridgeBinaryName())
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  service.Handshake,
		Plugins:          service.PluginMap,
		Cmd:              cmd,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		AutoMTLS:         true,
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, nil, nil, err
	}

	var peg processExitGroup
	if g, gerr := newProcessExitGroup(); gerr == nil && g != nil {
		if attachProcessToJob(g, cmd.Process) == nil {
			peg = g
		} else {
			_ = g.dispose()
		}
	}

	raw, err := rpcClient.Dispense(service.PluginKey)
	if err != nil {
		client.Kill()
		if peg != nil {
			_ = peg.dispose()
		}
		return nil, nil, nil, err
	}
	return client, raw.(service.HardwareService), peg, nil
}

// startBridgeClient (re)starts the sensor bridge: it detaches and kills any
// current bridge, spawns a new one outside the lock, and swaps it in. While
// it runs rt.hw is nil, so callers see "bridge not ready" instead of blocking.
// A call made while another start is in flight is a no-op.
func (p *Plugin) startBridgeClient(rt *bridgeRuntime) error {
	rt.mu.Lock()
	if rt.starting {
		rt.mu.Unlock()
		return nil
	}
	rt.starting = true
	oldClient, oldPeg := rt.c, rt.peg
	rt.c, rt.hw, rt.peg = nil, nil, nil
	rt.failures = 0
	invalidatePollCacheForRuntime(rt)
	rt.mu.Unlock()

	if oldClient != nil {
		oldClient.Kill()
	}
	if oldPeg != nil {
		_ = oldPeg.dispose()
	}

	client, hw, peg, err := newBridgeClient()

	rt.mu.Lock()
	rt.starting = false
	if err == nil {
		rt.c, rt.hw, rt.peg = client, hw, peg
	}
	rt.mu.Unlock()
	return err
}

// restartBridge kills and restarts the sensor bridge.
func (p *Plugin) restartBridge(rt *bridgeRuntime) {
	if err := p.startBridgeClient(rt); err != nil {
		log.Printf("restartBridge: %v\n", err)
	}
}

// sensorsWithTimeout fetches sensors from the bridge.
func (p *Plugin) sensorsWithTimeout(d time.Duration) ([]service.Sensor, error) {
	rt := p.bridge
	rt.mu.RLock()
	hw := rt.hw
	rt.mu.RUnlock()
	if hw == nil {
		return nil, fmt.Errorf("AIDA64 bridge not ready")
	}
	ch := make(chan sensorResult, 1)
	go func() {
		s, err := hw.Sensors()
		ch <- sensorResult{sensors: s, err: err}
	}()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case res := <-ch:
		return res.sensors, res.err
	case <-timer.C:
		return nil, fmt.Errorf("sensors timeout")
	}
}

// getReading fetches a reading from the bridge.
func (p *Plugin) getReading(suid, rid string) (service.Reading, []service.Reading, error) {
	rt := p.bridge
	rt.mu.RLock()
	hw := rt.hw
	rt.mu.RUnlock()
	if hw == nil {
		return nil, nil, fmt.Errorf("AIDA64 bridge not ready")
	}
	rbs, err := hw.ReadingsForSensorID(suid)
	if err != nil {
		return nil, nil, fmt.Errorf("getReading ReadingsBySensor failed: %v", err)
	}
	for _, r := range rbs {
		if r.ID() == rid {
			return r, rbs, nil
		}
	}
	return nil, rbs, fmt.Errorf("reading %q not present in category %q", rid, suid)
}

// getCachedPollTime returns the cached content-change token from the bridge.
// The cache is a short TTL window (one shared value per ticker cycle) so a
// burst of tile updates does not hammer the gRPC bridge.
//
// This is the FRESHNESS signal (the 60 s tile staleness window), not the
// connection status: PollTime deliberately never errors on a read failure -
// the bridge keeps returning the last content-change stamp while AIDA64 is
// closed, so a non-zero token here does NOT mean the block is alive. The
// Connection block is driven by bridgeAvailable instead (see
// piStatusSnapshot); the two stay separate on purpose.
func (p *Plugin) getCachedPollTime() (uint64, error) {
	rt := p.bridge
	rt.mu.RLock()
	hw := rt.hw
	cached := rt.cachedPollTime
	cachedAt := rt.cachedAt
	rt.mu.RUnlock()
	if hw == nil {
		return 0, fmt.Errorf("AIDA64 bridge not ready")
	}

	p.mu.RLock()
	cacheTTL := p.pollTimeCacheTTL
	p.mu.RUnlock()
	if cacheTTL == 0 {
		cacheTTL = defaultPollInterval
	}
	if !cachedAt.IsZero() && time.Since(cachedAt) < cacheTTL {
		return cached, nil
	}

	pollTime, err := hw.PollTime()
	rt.mu.Lock()
	if err != nil {
		// Bridge process gone (a crash the terminate event may not fire for):
		// clear the cache so a stale token is not served past the TTL.
		rt.cachedPollTime = 0
		rt.cachedAt = time.Time{}
		rt.mu.Unlock()
		return 0, err
	}
	rt.cachedPollTime = pollTime
	rt.cachedAt = time.Now()
	rt.mu.Unlock()

	return pollTime, nil
}

// invalidatePollCacheForRuntime clears the poll time cache.
// Caller must hold rt.mu write lock.
func invalidatePollCacheForRuntime(rt *bridgeRuntime) {
	rt.cachedPollTime = 0
	rt.cachedAt = time.Time{}
}

// bridgeRestartAfterFailures is how many consecutive failed liveness calls
// (one per second, each bounded by the gRPC call timeout) mark the bridge as
// wedged and trigger a restart. A closed AIDA64 is not a failure: the bridge
// still answers, with available=false.
const bridgeRestartAfterFailures = 3

// updateBridgeLiveness refreshes bridgeAvailable from the bridge's "block
// opened and parsed on the last tick" flag (Sources()[0].Available) and
// reports whether it changed. It runs every second from the sampler, so it
// does not depend on the SDK application events firing. A failed call reads
// as not available; repeated failures mean the bridge process is alive but
// not answering, which the watchdog (it only sees exited processes) would
// never fix, so the bridge is restarted here.
func (p *Plugin) updateBridgeLiveness() bool {
	rt := p.bridge
	rt.mu.RLock()
	hw := rt.hw
	rt.mu.RUnlock()
	if hw == nil {
		return false
	}
	available := false
	srcs, err := hw.Sources()
	if err == nil && len(srcs) > 0 {
		available = srcs[0].Available()
	}
	rt.mu.Lock()
	if err != nil {
		rt.failures++
	} else {
		rt.failures = 0
	}
	restart := rt.failures >= bridgeRestartAfterFailures
	rt.mu.Unlock()
	if restart {
		log.Printf("bridge not answering (%v); restarting\n", err)
		go p.restartBridge(rt)
	}
	p.mu.Lock()
	changed := p.bridgeAvailable != available
	p.bridgeAvailable = available
	p.mu.Unlock()
	return changed
}

// stalenessWindow is how long the content-change token may lag before a tile
// renders as stale. It is far more generous than HWiNFO's 5 s because an idle
// box has AIDA64 rewrite byte-identical content for many seconds, so the
// freshness token goes quiet while everything is still healthy. The
// connection status (block opened and parsed this tick) stays the weaker,
// separate signal.
const stalenessWindow = 60 * time.Second

// pollTimeForUID returns the poll time that gates rendering for a sensor
// UID. AIDA64 has exactly one local source, so this is the shared
// freshness token for every tile.
func (p *Plugin) pollTimeForUID(string) (uint64, error) {
	return p.getCachedPollTime()
}

// NewPlugin creates an instance and initializes the plugin
func NewPlugin(port, uuid, event, info string) (*Plugin, error) {
	p := &Plugin{
		am:                  newActionManager(defaultPollInterval),
		bridge:              &bridgeRuntime{},
		graphs:              make(map[string]*graph.Graph),
		autoRangeMax:        make(map[string]int),
		lastPollTime:        make(map[string]uint64),
		lastRenderTime:      make(map[string]time.Time),
		smoothedValues:      make(map[string]float64),
		divisorCache:        make(map[string]divisorCacheEntry),
		thresholdStates:     make(map[string]map[string]*thresholdRuntimeState),
		thresholdSnoozes:    make(map[string]*thresholdSnoozeState),
		thresholdDirty:      make(map[string]bool),
		pollTimeCacheTTL:    pollTimeCacheTTLForInterval(defaultPollInterval),
		piContexts:          make(map[string]string),
		settingsContexts:    make(map[string]*settingsTileSettings),
		globalSettingsExtra: make(map[string]json.RawMessage),
		compositeSettings:   make(map[string]*compositeActionSettings),
		compositeStates:     make(map[string]*compositeState),
		derivedSettings:     make(map[string]*derivedActionSettings),
		derivedStates:       make(map[string]*derivedState),
		dialSettings:        make(map[string]*dialActionSettings),
		dialStates:          make(map[string]*dialState),
	}

	// Cache placeholder image at startup.
	// Preferred source is settings/default tile art (startup chip).
	for _, candidate := range []string{"./settingsImage.png", "./defaultImage.png", "./launch-aida64.png"} {
		if bts, err := os.ReadFile(candidate); err == nil {
			p.placeholderImage = bts
			log.Printf("Cached settings placeholder image from %s\n", candidate)
			break
		}
	}
	if len(p.placeholderImage) == 0 {
		log.Printf("Warning: could not cache placeholder image\n")
	}
	defaultArt, _ := os.ReadFile("./defaultImage.png")
	if img, err := renderDisconnectedImage(defaultArt); err == nil {
		p.disconnectedImage = img
	} else {
		log.Printf("Warning: could not render disconnected image: %v\n", err)
	}

	// Bridge starts are deferred until global settings arrive (OnDidReceiveGlobalSettings).
	// NewPlugin does not start any bridge here.
	p.sd = streamdeck.NewStreamDeck(port, uuid, event, info)
	return p, nil
}

// RunForever starts the plugin and waits for events, indefinitely
func (p *Plugin) RunForever() error {
	defer func() {
		rt := p.bridge
		rt.mu.Lock()
		if rt.c != nil {
			rt.c.Kill()
		}
		if rt.peg != nil {
			_ = rt.peg.dispose()
		}
		rt.mu.Unlock()
	}()

	p.sd.SetDelegate(p)
	p.am.Run(p.updateTiles, p.updateAuxTiles)
	p.startedAt = time.Now()

	// Must be a goroutine: runAuxSampler loops forever. Called
	// synchronously it starved the main goroutine so p.sd.Connect()
	// never ran and the whole plugin sat disconnected (every PI on
	// "Checking..."/"Loading...", all tiles on placeholder).
	go p.runAuxSampler()

	// Watch-dog: start the bridge and restart it when it exits.
	go func() {
		for {
			rt := p.bridge
			rt.mu.RLock()
			needsStart := rt.hw == nil
			needsRestart := !needsStart && rt.c != nil && rt.c.Exited()
			rt.mu.RUnlock()

			if needsStart || needsRestart {
				if err := p.startBridgeClient(rt); err != nil {
					log.Printf("startBridgeClient failed: %v\n", err)
				}
			}
			time.Sleep(1 * time.Second)
		}
	}()

	err := p.sd.Connect()
	if err != nil {
		return fmt.Errorf("StreamDeck Connect: %v", err)
	}
	defer p.sd.Close()
	p.sd.ListenAndWait()
	return nil
}

func (p *Plugin) applyDefaultFormatValueOnly(v float64, t service.ReadingType) string {
	switch t {
	case service.ReadingTypeNone:
		return fmt.Sprintf("%0.f", v)
	case service.ReadingTypeTemp:
		return fmt.Sprintf("%.0f", v)
	case service.ReadingTypeVolt:
		return fmt.Sprintf("%.0f", v)
	case service.ReadingTypeFan:
		return fmt.Sprintf("%.0f", v)
	case service.ReadingTypeCurrent:
		return fmt.Sprintf("%.0f", v)
	case service.ReadingTypePower:
		return fmt.Sprintf("%0.f", v)
	case service.ReadingTypeClock:
		return fmt.Sprintf("%.0f", v)
	case service.ReadingTypeUsage:
		return fmt.Sprintf("%.0f", v)
	case service.ReadingTypeOther:
		return fmt.Sprintf("%.0f", v)
	}
	return "Bad Format"
}

// normalizeForGraph converts data size values to the target unit for consistent graph scaling.
// This prevents jumps when the source switches units (e.g. 1000 KB/s to 1 MB/s).
// targetUnit can be: "B", "KB", "MB", "GB", "TB" or empty (no normalization).
func (p *Plugin) normalizeForGraph(value float64, sourceUnit string, targetUnit string) float64 {
	if targetUnit == "" {
		return value // no normalization
	}

	// Convert source value to bytes first
	sourceLower := strings.ToLower(sourceUnit)
	var bytes float64
	switch {
	case strings.HasPrefix(sourceLower, "tb") || strings.HasPrefix(sourceLower, "tib"):
		bytes = value * 1024 * 1024 * 1024 * 1024
	case strings.HasPrefix(sourceLower, "gb") || strings.HasPrefix(sourceLower, "gib"):
		bytes = value * 1024 * 1024 * 1024
	case strings.HasPrefix(sourceLower, "mb") || strings.HasPrefix(sourceLower, "mib"):
		bytes = value * 1024 * 1024
	case strings.HasPrefix(sourceLower, "kb") || strings.HasPrefix(sourceLower, "kib"):
		bytes = value * 1024
	case strings.HasPrefix(sourceLower, "b/") || sourceLower == "b":
		bytes = value
	default:
		return value // not a data size unit, no conversion
	}

	// Convert bytes to target unit
	switch strings.ToUpper(targetUnit) {
	case "TB":
		return bytes / (1024 * 1024 * 1024 * 1024)
	case "GB":
		return bytes / (1024 * 1024 * 1024)
	case "MB":
		return bytes / (1024 * 1024)
	case "KB":
		return bytes / 1024
	case "B":
		return bytes
	default:
		return value
	}
}

// formatDisplayValue formats a numeric value and unit into display strings.
// Returns (valueTextNoUnit, displayText).
func (p *Plugin) formatDisplayValue(v float64, displayUnit, format string, readingType service.ReadingType) (string, string) {
	valueTextNoUnit := ""
	displayText := ""
	if format != "" {
		result := fmt.Sprintf(format, v)
		if strings.Contains(result, "%!") {
			valueTextNoUnit = p.applyDefaultFormatValueOnly(v, readingType)
		} else {
			valueTextNoUnit = result
		}
		displayText = valueTextNoUnit
	} else {
		valueTextNoUnit = p.applyDefaultFormatValueOnly(v, readingType)
		if displayUnit != "" {
			if displayUnit == "%" {
				displayText = valueTextNoUnit + displayUnit
			} else {
				displayText = valueTextNoUnit + " " + displayUnit
			}
		} else {
			displayText = valueTextNoUnit
		}
	}
	return valueTextNoUnit, displayText
}

func (p *Plugin) getCachedDivisor(context, raw string) (float64, error) {
	if raw == "" {
		return 1, nil
	}
	p.mu.RLock()
	if entry, ok := p.divisorCache[context]; ok && entry.raw == raw {
		p.mu.RUnlock()
		return entry.value, nil
	}
	p.mu.RUnlock()

	fdiv, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, err
	}
	p.mu.Lock()
	p.divisorCache[context] = divisorCacheEntry{raw: raw, value: fdiv}
	p.mu.Unlock()
	return fdiv, nil
}

func (p *Plugin) updateTiles(data *actionData) {
	if data.action != "com.poizenjam.aida64sd.reading" {
		log.Printf("Unknown action updateTiles: %s\n", data.action)
		return
	}

	p.mu.RLock()
	g, ok := p.graphs[data.context]
	p.mu.RUnlock()
	if !ok {
		log.Printf("Graph not found for context: %s\n", data.context)
		return
	}

	showUnavailable := func() {
		if !data.settings.InErrorState {
			payload := evStatus{Error: true, Message: "AIDA64 Unavailable"}
			err := p.sd.SendToPropertyInspector("com.poizenjam.aida64sd.reading", data.context, payload)
			if err != nil {
				log.Println("updateTiles SendToPropertyInspector", err)
			}
			data.settings.InErrorState = true
			p.sd.SetSettings(data.context, &data.settings)
		}
		// Not tied to InErrorState, which is saved with the key's settings: a
		// key that reappears, or a plugin that restarts, during an outage
		// still needs the image.
		p.showDisconnected(data.context)
		// Clear lastPollTime so we re-render when AIDA64 comes back
		p.mu.Lock()
		delete(p.lastPollTime, data.context)
		p.mu.Unlock()
	}

	forceUpdate := p.consumeThresholdDirty(data.context)

	pollTime, err := p.pollTimeForUID(data.settings.SensorUID)
	if err != nil {
		log.Printf("PollTime failed: %v\n", err)
		showUnavailable()
		return
	}
	if pollTime == 0 || time.Since(time.Unix(0, int64(pollTime))) > stalenessWindow {
		showUnavailable()
		return
	}

	// AIDA64 is back: restore the PI (only once the poll succeeds, so an
	// outage does not flip it every retry) and redraw over the
	// disconnected image.
	if data.settings.InErrorState {
		payload := evStatus{Error: false, Message: "show_ui"}
		err := p.sd.SendToPropertyInspector("com.poizenjam.aida64sd.reading", data.context, payload)
		if err != nil {
			log.Println("updateTiles SendToPropertyInspector", err)
		}
		data.settings.InErrorState = false
		p.sd.SetSettings(data.context, &data.settings)
	}
	if p.clearDisconnected(data.context) {
		forceUpdate = true
	}
	s := data.settings

	if !forceUpdate {
		p.mu.RLock()
		last, ok := p.lastPollTime[data.context]
		p.mu.RUnlock()
		if ok && last == pollTime {
			return
		}
	}

	if override := s.UpdateIntervalOverrideMs; override > 0 && !forceUpdate {
		p.mu.RLock()
		lastRender := p.lastRenderTime[data.context]
		p.mu.RUnlock()
		if time.Since(lastRender) < time.Duration(override)*time.Millisecond {
			return
		}
	}

	r, readings, err := p.getReading(s.SensorUID, s.ReadingID)
	if err != nil {
		if s.ReadingLabel != "" {
			for _, candidate := range readings {
				if candidate.Label() == s.ReadingLabel {
					s.ReadingID = candidate.ID()
					r = candidate
					err = nil
					_ = p.sd.SetSettings(data.context, s)
					p.am.SetAction(data.action, data.context, s)
					break
				}
			}
		}
		if err != nil {
			log.Printf("getReading failed: %v\n", err)
			showUnavailable()
			return
		}
	}
	if s.ShowTitleInGraph != nil && *s.ShowTitleInGraph && s.Title == "" {
		g.SetLabelText(0, r.Label())
	}

	v := r.Value()
	divisor, err := p.getCachedDivisor(data.context, s.Divisor)
	if err != nil {
		log.Printf("Failed to parse float: %s\n", s.Divisor)
		return
	}
	if divisor != 1 {
		v = r.Value() / divisor
	}

	// Normalize the graph value to handle unit changes (e.g. KB/s to MB/s)
	// Only applies to throughput readings (units containing "/s")
	graphValue := r.Value()
	if s.GraphUnit != "" && strings.Contains(r.Unit(), "/s") {
		graphValue = p.normalizeForGraph(r.Value(), r.Unit(), s.GraphUnit)
	}
	if divisor != 1 {
		graphValue = graphValue / divisor
	}

	// Determine display value and unit
	displayValue := v
	displayUnit := r.Unit()
	if s.GraphUnit != "" && strings.Contains(r.Unit(), "/s") {
		// Convert display value to match GraphUnit
		displayValue = p.normalizeForGraph(v, r.Unit(), s.GraphUnit)
		displayUnit = s.GraphUnit + "/s"
	}

	// EMA smoothing: threshold eval uses raw v; smoothing applies to graph and display values
	if alpha := s.SmoothingAlpha; alpha > 0 && alpha < 1.0 {
		p.mu.Lock()
		prev, ok := p.smoothedValues[data.context]
		if !ok {
			prev = graphValue
		}
		smoothed := emaSmooth(alpha, graphValue, prev)
		p.smoothedValues[data.context] = smoothed
		p.mu.Unlock()
		if graphValue != 0 {
			ratio := smoothed / graphValue
			graphValue = smoothed
			displayValue *= ratio
		}
	}

	// Auto range: grow the graph scale to the observed data until the user
	// pins it. Tracks the plotted value (divisor + graph unit applied);
	// usage keeps a fixed 0 floor but may still exceed 100 (TDP %).
	if autoRangeEnabled(s.AutoRange) {
		iv := int(math.Round(graphValue))
		if nMin, nMax, changed := p.applyAutoRange(data.context, s.Min, s.Max, iv, r.TypeI() == 7); changed {
			s.Min, s.Max = nMin, nMax
			g.SetMin(nMin)
			g.SetMax(nMax)
			_ = p.sd.SetSettings(data.context, s)
		}
	}

	valueTextNoUnit, displayText := p.formatDisplayValue(displayValue, displayUnit, s.Format, service.ReadingType(r.TypeI()))

	now := time.Now()

	// Check threshold alerts (evaluate by priority, highest first)
	thresholds := p.resolveThresholdsForEval(s.Thresholds, s.SuppressedGlobalIDs, service.ReadingType(r.TypeI()))
	activeThreshold := p.evaluateThresholds(data.context, v, thresholds, now)

	newThresholdID := ""
	alertText := ""
	if activeThreshold != nil {
		newThresholdID = activeThreshold.ID
		if activeThreshold.Text != "" {
			alertText = p.applyThresholdText(activeThreshold.Text, valueTextNoUnit, displayUnit)
		}
	}

	snoozeState, snoozed, snoozeChanged := p.currentThresholdSnoozeState(data.context, now)
	if activeThreshold == nil {
		if p.clearThresholdSnooze(data.context) {
			snoozeChanged = true
		}
		snoozed = false
		snoozeState = thresholdSnoozeState{}
	}
	if snoozeChanged {
		forceUpdate = true
	}

	// Apply colors before drawing so the current frame matches the active threshold state.
	if forceUpdate || newThresholdID != s.CurrentThresholdID {
		if activeThreshold != nil && !snoozed {
			p.applyThresholdColors(g, activeThreshold)
		} else {
			p.applyNormalColors(g, s)
		}
		s.CurrentThresholdID = newThresholdID
		p.am.SetAction(data.action, data.context, s)
		_ = p.sd.SetSettings(data.context, s)
	}

	renderDisplayText, renderAlertText, renderGraphValue, freezeGraph := p.resolveThresholdDisplay(
		data.context,
		activeThreshold,
		v,
		graphValue,
		displayText,
		alertText,
	)
	if snoozed {
		renderDisplayText = displayText
		renderAlertText = thresholdSnoozeText(snoozeState, now)
		renderGraphValue = graphValue
		freezeGraph = false
	}
	switch s.GraphMode {
	case "text":
		g.Clear()
	default: // "both" or ""
		if !freezeGraph {
			g.Update(renderGraphValue)
		}
	case "graph":
		if !freezeGraph {
			g.Update(renderGraphValue)
		}
	}

	if s.GraphMode == "graph" {
		g.SetLabelText(0, "")
		g.SetLabelText(1, "")
		g.SetLabelText(2, "")
	} else {
		g.SetLabelText(1, renderDisplayText)
		if renderAlertText != "" {
			g.SetLabelText(2, renderAlertText)
		} else {
			g.SetLabelText(2, "")
		}
	}

	b, err := g.EncodePNG()
	if err != nil {
		log.Printf("Failed to encode graph: %v\n", err)
		return
	}

	err = p.sd.SetImage(data.context, b)
	if err != nil {
		log.Printf("Failed to setImage: %v\n", err)
		return
	}

	p.mu.Lock()
	p.lastPollTime[data.context] = pollTime
	if data.settings.UpdateIntervalOverrideMs > 0 {
		p.lastRenderTime[data.context] = time.Now()
	}
	p.mu.Unlock()
}

func (p *Plugin) refreshAction(action, context string) {
	settings, err := p.am.getSettings(context)
	if err != nil {
		log.Printf("refreshAction getSettings: %v\n", err)
		return
	}
	p.updateTiles(&actionData{
		action:   action,
		context:  context,
		settings: &settings,
	})
}

func (p *Plugin) updateAuxTiles() {
	p.updateCompositeTick()
	p.updateDerivedTick()
	p.updateDialTick()
}

// runAuxSampler is the plugin's fixed auxiliary timer (auxSampleInterval),
// independent of the update ticker whose period auto-refresh itself sets. It
// (1) lets auto refresh follow the rate read from AIDA64.INI (the INI
// re-check runs at iniRecheckInterval, mtime-gated), and (2) re-pushes the
// sensor catalog once per second if the readings set changed (self-heals the
// blank sensor picker when the snapshot was empty at PI connect).
func (p *Plugin) runAuxSampler() {
	ticker := time.NewTicker(auxSampleInterval)
	defer ticker.Stop()
	var n int
	for range ticker.C {
		n++
		rt := p.bridge
		rt.mu.RLock()
		hw := rt.hw
		rt.mu.RUnlock()
		if hw == nil {
			continue
		}
		p.applyAutoRefresh()
		if n%4 == 0 {
			// 1 s at the 250 ms period: refresh the bridge liveness flag and push
			// a status only on a real transition. This is the PRIMARY connection
			// signal and the self-correcting backstop that flips
			// Connected/Disconnected even if the SDK application events never fire.
			if p.updateBridgeLiveness() {
				p.sendStatusToPIs()
			}
			p.maybeRepushCatalog()
			// 60 s at the 250 ms period: re-stat the INI for an mtime change.
			if n%240 == 0 {
				p.readIniRate(false)
			}
		}
	}
}

// readIniRate re-reads AIDA64.INI and (in auto mode) applies the rate
// AIDA64 configured for itself. force (the PI's Refresh button, and the
// auto-mode re-enable) bypasses the initial-delay and 60 s period gates and
// re-applies the cached rate even when the file did not move.
//
// Path resolution: a user-set path is used as-is (a missing file is a
// warning, never a silent re-detect); otherwise the last auto-detection,
// re-run once if that path no longer exists; otherwise locateAida64Ini.
// A file whose mtime did not move is not re-read: AIDA64 rewrites the INI
// from memory on exit, so an mtime change is exactly "the rate changed".
// Any failure status (not detected, no rate key, read error, missing set
// path) clears the resolved rate, so applyAutoRefresh falls back to the
// manual PollInterval and the PI shows why.
func (p *Plugin) readIniRate(force bool) {
	now := time.Now()
	p.mu.Lock()
	manualPath := strings.TrimSpace(p.globalSettings.IniPath)
	manualKey := strings.TrimSpace(p.globalSettings.IniRateKey)
	lastRead := p.lastIniRead
	detected := p.iniDetectedPath
	mtime, mtimePath := p.iniFileMtime, p.iniFileMtimePath
	prevMs, prevKey, prevNote := p.iniRateMs, p.iniRateKey, p.iniNote
	started := p.startedAt
	p.mu.Unlock()

	if !force {
		// A user-set path does not depend on AIDA64's boot, so only the
		// auto-detect path waits out AIDA64's startup.
		if manualPath == "" && (started.IsZero() || started.Add(iniInitialReadDelay).After(now)) {
			return
		}
		if !lastRead.IsZero() && now.Sub(lastRead) < iniRecheckInterval {
			return
		}
	}

	status := iniStatusOK
	var path string
	if manualPath != "" {
		path = manualPath
	} else if detected != "" {
		path = detected
	}
	if path == "" {
		found, err := locateAida64Ini()
		if err != nil {
			status = iniStatusNotDetected
		} else {
			detected, path = found, found
		}
	}

	ms, key, note := 0, "", ""
	changed := false
	if path != "" {
		st, serr := os.Stat(path)
		if serr != nil {
			if manualPath != "" {
				// The file the user pointed at is gone: warn and fall back to
				// the manual interval. Do not silently adopt another install.
				status = iniStatusPathMissing
			} else if found, lerr := locateAida64Ini(); lerr == nil && found != "" {
				// Cached detection went stale (AIDA64 moved or was
				// reinstalled): adopt the new location.
				detected, path = found, found
				st, _ = os.Stat(path)
			} else {
				path, status = "", iniStatusNotDetected
			}
		}
		switch {
		case status == iniStatusOK && st != nil && st.ModTime().Equal(mtime) && mtimePath == path:
			// File has not moved since the last successful read, so the cached
			// rate is still valid. Only an explicit re-read re-applies it.
			ms, key, note = prevMs, prevKey, prevNote
			changed = force
		case status == iniStatusOK && st != nil:
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				status = iniStatusReadError
			} else {
				key, ms, note = iniRateResolution(parseIni(string(data)), manualKey)
				if key == "" {
					status = iniStatusNoRateKey
					ms = 0
				} else {
					mtime, mtimePath = st.ModTime(), path
				}
				changed = true
			}
		case status == iniStatusOK:
			status = iniStatusReadError // re-detect found a path but Stat still failed
		}
	}
	if status != iniStatusOK {
		ms = 0 // auto falls back to the manual PollInterval
	}
	if manualKey != "" && status == iniStatusOK && key != manualKey {
		note = noteWithOverrideWarning(note, manualKey)
	}

	p.mu.Lock()
	p.iniDetectedPath = detected
	p.iniUsedPath = path
	p.iniFileMtime, p.iniFileMtimePath = mtime, mtimePath
	p.iniRateMs, p.iniRateKey, p.iniNote = ms, key, note
	p.iniStatus = status
	p.lastIniRead = now
	p.mu.Unlock()

	if changed {
		if status == iniStatusOK {
			log.Printf("INI rate: %s %s = %dms (%s)\n", path, key, ms, note)
		} else {
			log.Printf("INI rate: %s (path %q)\n", status, path)
		}
		p.sendStatusToPIs()
	}
	p.applyAutoRefresh()
}

// maybeRepushCatalog re-sends the sensor catalog to every open sensor PI
// when the bridge's readings set changed since the last check. The set is
// summarized as the reading id set (sameReadingSet: size compared first, so
// a steady state with an unchanged snapshot is one Sensors call plus the
// per-category ReadingsForSensorID calls the tiles already make every tick).
func (p *Plugin) maybeRepushCatalog() {
	rt := p.bridge
	rt.mu.RLock()
	hw := rt.hw
	rt.mu.RUnlock()
	if hw == nil {
		return
	}
	sensors, err := p.sensorsWithTimeout(auxSampleInterval)
	p.mu.Lock()
	if err != nil {
		p.sensorCount = 0
	} else {
		p.sensorCount = len(sensors)
	}
	p.mu.Unlock()
	if err != nil {
		return
	}
	ids := map[string]bool{}
	for _, s := range sensors {
		readings, rerr := hw.ReadingsForSensorID(s.ID())
		if rerr != nil {
			continue
		}
		for _, r := range readings {
			ids[r.ID()] = true
		}
	}
	if sameReadingSet(p.lastCatalogReadings, ids) {
		return
	}
	p.mu.Lock()
	p.lastCatalogReadings = ids
	contexts := make(map[string]bool, len(p.piContexts))
	for ctx, action := range p.piContexts {
		contexts[ctx] = action == "com.poizenjam.aida64sd.reading"
	}
	p.mu.Unlock()
	for ctx, isSensor := range contexts {
		if !isSensor {
			continue
		}
		settings, serr := p.am.getSettings(ctx)
		if serr != nil {
			continue
		}
		if err := p.sendCatalogToPropertyInspector("com.poizenjam.aida64sd.reading", ctx, &settings, sensors); err != nil {
			log.Printf("maybeRepushCatalog %s: %v", ctx, err)
		}
	}
}

func sameReadingSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range b {
		if !a[k] {
			return false
		}
	}
	return true
}

func (p *Plugin) applyThresholdText(template, valueTextNoUnit, unit string) string {
	out := strings.ReplaceAll(template, "{value}", valueTextNoUnit)
	out = strings.ReplaceAll(out, "{unit}", unit)
	return out
}

// iniStatusFields is the INI rate state the PIs render in the Connection
// block: the path used and configured, the resolved key and rate, the
// resolution status, and the selector list.
type iniStatusFields struct {
	Path       string
	PathSet    string
	RateMs     int // raw value read from the INI (source of truth); 0 = none
	AppliedMs  int // clamped value the ticker actually runs at; 0 = none
	Status     string
	Key        string
	KeySet     string
	Note       string
	KeyOptions []string
}

// piStatusSnapshot returns the live connection status and refresh state the
// Settings PI and Settings key render. sensorCount is 0 until the bridge is up.
func (p *Plugin) piStatusSnapshot() (status string, intervalMs, sensorCount int, autoRefresh bool, ini iniStatusFields) {
	p.mu.RLock()
	intervalMs = p.globalSettings.PollInterval
	autoRefresh = p.globalSettings.AutoRefresh == nil || *p.globalSettings.AutoRefresh
	// The readout shows the clamped rate the ticker actually runs at, not the
	// raw INI value, which may sit outside 250ms-10s.
	appliedMs := 0
	if p.iniRateMs > 0 {
		appliedMs = int(clampIniRateMs(p.iniRateMs).Milliseconds())
	}
	ini = iniStatusFields{
		Path:       p.iniUsedPath,
		PathSet:    p.globalSettings.IniPath,
		RateMs:     p.iniRateMs,
		AppliedMs:  appliedMs,
		Status:     p.iniStatus,
		Key:        p.iniRateKey,
		KeySet:     p.globalSettings.IniRateKey,
		Note:       p.iniNote,
		KeyOptions: iniRateKeyNames(),
	}
	// Read under the lock: updateBridgeLiveness writes this from
	// the sampler goroutine while PI handlers call piStatusSnapshot.
	bridgeUp := p.bridgeAvailable
	sensorCount = p.sensorCount
	p.mu.RUnlock()
	if intervalMs <= 0 {
		intervalMs = int(p.am.GetInterval().Milliseconds())
	}
	// Connection status comes from the bridge liveness flag, not the poll
	// token (see bridgeAvailable). Plain "Disconnected" when the block is gone.
	status = "Disconnected"
	if bridgeUp {
		status = "Connected"
	}
	return status, intervalMs, sensorCount, autoRefresh, ini
}

// sendStatusToPIs pushes the live status to every open PI (the Settings PI
// renders it; the others ignore it) and redraws the Settings keys, so the
// connection line, rate readout and key update on every poll, auto-refresh
// and AIDA64 connect/disconnect change.
func (p *Plugin) sendStatusToPIs() {
	status, intervalMs, sensorCount, autoRefresh, ini := p.piStatusSnapshot()
	payload := map[string]interface{}{
		"connectionStatus": status,
		"pollInterval":     intervalMs,
		"sensorCount":      sensorCount,
		"autoRefresh":      autoRefresh,
		"iniPath":          ini.Path,
		"iniPathSet":       ini.PathSet,
		"iniRateMs":        ini.RateMs,
		"iniRateAppliedMs": ini.AppliedMs,
		"iniStatus":        ini.Status,
		"iniRateKey":       ini.Key,
		"iniRateKeySet":    ini.KeySet,
		"iniNote":          ini.Note,
		"iniKeyOptions":    ini.KeyOptions,
		"hostVersion":      hostVersion,
	}
	p.mu.RLock()
	diag := p.autoDiagnose
	contexts := make([][2]string, 0, len(p.piContexts))
	for ctx, action := range p.piContexts {
		contexts = append(contexts, [2]string{action, ctx})
	}
	p.mu.RUnlock()
	if diag != "" {
		payload["autoDiagnose"] = diag
		// One-shot: clear so a later push does not re-log the same line.
		p.mu.Lock()
		p.autoDiagnose = ""
		p.mu.Unlock()
	}
	for _, a := range contexts {
		if err := p.sd.SendToPropertyInspector(a[0], a[1], payload); err != nil {
			log.Printf("sendStatusToPIs %s %s: %v\n", a[0], a[1], err)
		}
	}
	p.updateAllSettingsTiles()
}

func drawCenteredText(dst *image.RGBA, face font.Face, clr *color.RGBA, text string, baselineY int) {
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(clr),
		Face: face,
	}
	textWidth := d.MeasureString(text).Round()
	x := (dst.Bounds().Dx() - textWidth) / 2
	if x < 0 {
		x = 0
	}
	d.Dot = fixed.P(x, baselineY)
	d.DrawString(text)
}

// setPollInterval changes the polling interval dynamically
func (p *Plugin) setPollInterval(intervalMs int) {
	if intervalMs < 250 {
		intervalMs = 250
	}
	if intervalMs > 10000 {
		intervalMs = 10000
	}

	interval := time.Duration(intervalMs) * time.Millisecond

	// Update action manager ticker
	p.am.SetInterval(interval)

	// Update cache TTL and global settings under lock
	p.mu.Lock()
	p.pollTimeCacheTTL = pollTimeCacheTTLForInterval(interval)
	p.globalSettings.PollInterval = intervalMs
	p.mu.Unlock()

	// Update all settings tiles (reads globalSettings under its own lock)
	p.sendStatusToPIs()

	// Persist the full global blob so unrelated fields survive.
	p.persistGlobalSettings()

	log.Printf("Poll interval changed to %v\n", interval)
}
