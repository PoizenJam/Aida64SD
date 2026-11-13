package aida64plugin

// Auto refresh (command authority, INI key handling) and clamped-readout host tests.
//
// Auto refresh: the setPollInterval command is the single authoritative writer of
// AutoRefresh; a stale global-settings round-trip (the SDK store is a union
// that keeps the last manual autoRefresh:false) must not revert the user's
// choice. These are the exact "flash then revert" repros from the prompt.
// Clamped readout: piStatusSnapshot reports the CLAMPED rate the ticker runs at, not
// the raw pre-clamp INI value.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/poizenjam/aida64sd/pkg/streamdeck"
)

// newAutoRefreshTestPlugin builds a host with a temp AIDA64.INI (via the
// IniPath override, which the reader uses as-is on any OS) resolving to
// iniMs, plus the maps the status/persist paths iterate. p.sd stays nil so
// persistGlobalSettings/sendStatusToPIs are no-ops.
func newAutoRefreshTestPlugin(t *testing.T, iniMs int) *Plugin {
	t.Helper()
	dir := t.TempDir()
	iniPath := filepath.Join(dir, "AIDA64.INI")
	content := fmt.Sprintf("[General]\n[Generic]\nHWMonUpdateFreqExtApp=%d\n", iniMs)
	if err := os.WriteFile(iniPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp INI: %v", err)
	}
	return &Plugin{
		bridge:              &bridgeRuntime{hw: stubHardwareService{}},
		am:                  newActionManager(defaultPollInterval),
		piContexts:          map[string]string{},
		globalSettingsExtra: map[string]json.RawMessage{},
		globalSettings:      globalSettings{IniPath: iniPath, PollInterval: 1000},
	}
}

// sendSetPollInterval runs the exact PI command path (OnSendToPlugin with a
// setPollInterval payload). raw is the JSON value: `"auto"` or a number.
func sendSetPollInterval(p *Plugin, raw string) {
	body := []byte(fmt.Sprintf(`{"setPollInterval":%s}`, raw))
	pm := json.RawMessage(body)
	p.OnSendToPlugin(&streamdeck.EvSendToPlugin{Action: "com.poizenjam.aida64sd.reading", Context: "ctx", Payload: &pm})
}

// sendGlobalSettings runs the SDK round-trip path (OnDidReceiveGlobalSettings)
// with the given JSON blob.
func sendGlobalSettings(p *Plugin, blob string) {
	sm := json.RawMessage([]byte(blob))
	p.OnDidReceiveGlobalSettings(&streamdeck.EvDidReceiveGlobalSettings{
		Payload: streamdeck.EvDidReceiveGlobalSettingsPayload{Settings: &sm},
	})
}

func autoState(p *Plugin) (on bool, v *bool) {
	p.mu.RLock()
	v = p.globalSettings.AutoRefresh
	p.mu.RUnlock()
	return v == nil || *v, v
}

// TestBugAReEnableAutoStaysOn is the exact repro: auto OFF via the number
// command (manual dropdown), then re-enabled via the "auto" command (checkbox
// or the new Auto dropdown option). It asserts AutoRefresh is nil (auto on)
// and the ticker moves to the clamped INI rate (60s INI -> 10s).
func TestBugAReEnableAutoStaysOn(t *testing.T) {
	p := newAutoRefreshTestPlugin(t, 60000) // clamps to 10s

	// OFF via the number command (manual dropdown).
	sendSetPollInterval(p, "2000")
	if on, _ := autoState(p); on {
		t.Fatalf("after the number command auto is still on; want off (manual)")
	}
	if got := p.am.GetInterval(); got != 2000*time.Millisecond {
		t.Fatalf("after the number command ticker = %v, want 2s", got)
	}

	// Re-enable via the "auto" command (checkbox / Auto dropdown option).
	sendSetPollInterval(p, `"auto"`)
	on, v := autoState(p)
	if !on || v != nil {
		t.Fatalf("after the auto command AutoRefresh = %v, want nil (auto on)", v)
	}
	if got := p.am.GetInterval(); got != 10*time.Second {
		t.Fatalf("after the auto command ticker = %v, want the clamped 10s", got)
	}
}

// TestBugAAbsentKeyBlobDoesNotRevertAuto: after re-enabling auto, a
// global-settings round-trip that LACKS autoRefresh (the host's own persist
// echo when auto is on) must not revert it.
func TestBugAAbsentKeyBlobDoesNotRevertAuto(t *testing.T) {
	p := newAutoRefreshTestPlugin(t, 1000)
	sendSetPollInterval(p, "2000")   // off (manual)
	sendSetPollInterval(p, `"auto"`) // on again
	if on, _ := autoState(p); !on {
		t.Fatalf("setup: auto not on after the auto command")
	}

	// A blob lacking autoRefresh (poll interval + thresholds only).
	sendGlobalSettings(p, `{"pollInterval":2000}`)
	if on, v := autoState(p); !on || v != nil {
		t.Fatalf("an absent-key global-settings blob reverted auto: AutoRefresh = %v, want nil", v)
	}
}

// TestBugAStalePresentBlobDoesNotRevertAuto is the hard case: after
// re-enabling auto, a round-trip blob that PRESENTS a stale autoRefresh:false
// (the SDK store's union kept the last manual value) must not revert it. A
// partial fix that only drops the absent-key case fails here.
func TestBugAStalePresentBlobDoesNotRevertAuto(t *testing.T) {
	p := newAutoRefreshTestPlugin(t, 1000)
	sendSetPollInterval(p, "2000")   // off (manual) -> persists autoRefresh:false
	sendSetPollInterval(p, `"auto"`) // on again

	// The stale blob the SDK returns (it still carries the last manual value).
	sendGlobalSettings(p, `{"autoRefresh":false,"pollInterval":2000}`)
	on, v := autoState(p)
	if !on || v != nil {
		t.Fatalf("a stale autoRefresh:false blob reverted auto: AutoRefresh = %v, want nil", v)
	}
}

// TestBugAFreshOffCommandStillWorks: after auto is on, a FRESH number command
// (a genuine user action, not a round-trip) still turns auto off and applies
// the manual interval. The command path remains authoritative.
func TestBugAFreshOffCommandStillWorks(t *testing.T) {
	p := newAutoRefreshTestPlugin(t, 1000)
	sendSetPollInterval(p, `"auto"`) // on
	if on, _ := autoState(p); !on {
		t.Fatalf("setup: auto not on after the auto command")
	}

	sendSetPollInterval(p, "5000")
	on, v := autoState(p)
	if on || v == nil || *v {
		t.Fatalf("a fresh number command failed to turn auto off: AutoRefresh = %v", v)
	}
	if got := p.am.GetInterval(); got != 5000*time.Millisecond {
		t.Fatalf("a fresh number command ticker = %v, want 5s", got)
	}
}

// TestBugCStatusCarriesClampedIniRate: piStatusSnapshot reports the CLAMPED
// rate (iniRateAppliedMs) the ticker runs at. When the INI value is inside
// 250ms-10s it equals the raw value (readout unchanged); outside, the applied
// value is clamped and the raw value is still available for the readout.
func TestBugCStatusCarriesClampedIniRate(t *testing.T) {
	p := &Plugin{
		bridge:              &bridgeRuntime{hw: stubHardwareService{}},
		am:                  newActionManager(defaultPollInterval),
		piContexts:          map[string]string{},
		globalSettingsExtra: map[string]json.RawMessage{},
	}

	// Out of window: INI 60s -> applied 10s.
	p.iniRateMs = 60000
	p.iniStatus = iniStatusOK
	_, _, _, _, ini := p.piStatusSnapshot()
	if ini.RateMs != 60000 {
		t.Fatalf("iniRateMs (raw) = %d, want 60000 (source of truth)", ini.RateMs)
	}
	if ini.AppliedMs != 10000 {
		t.Fatalf("iniRateAppliedMs = %d, want 10000 (clamped)", ini.AppliedMs)
	}

	// In window: INI 1s -> applied 1s (readout unchanged).
	p.iniRateMs = 1000
	_, _, _, _, ini = p.piStatusSnapshot()
	if ini.AppliedMs != 1000 {
		t.Fatalf("iniRateAppliedMs (in-window) = %d, want 1000 (== raw)", ini.AppliedMs)
	}
	if ini.RateMs != 1000 {
		t.Fatalf("iniRateMs (in-window) = %d, want 1000", ini.RateMs)
	}
}

// The non-destructive OnDidReceiveGlobalSettings rebuild.
//
// Guarding autoRefresh alone is not enough. The fresh-gs rebuild
// (var gs globalSettings) also clobbered any field absent from the incoming
// merged blob to its zero value: iniRateKey -> "" (the INI key selector
// snapped back to default, repro 3) and pollInterval -> the 1 s clamp (the
// re-enabled auto readout flashed a manual value, repro 1). Seeding gs from a
// copy of the in-memory settings means an absent key keeps its value. These
// tests drive the exact command-then-stale-blob timing from the live repro.

// sendSetIniRateKey runs the exact PI command path for setIniRateKey.
func sendSetIniRateKey(p *Plugin, key string) {
	body := []byte(fmt.Sprintf(`{"setIniRateKey":%q}`, key))
	pm := json.RawMessage(body)
	p.OnSendToPlugin(&streamdeck.EvSendToPlugin{Action: "com.poizenjam.aida64sd.reading", Context: "ctx", Payload: &pm})
}

// iniSettings returns the stored INI override + manual poll interval under the
// host lock (readout source of truth for the PI Connection block).
func iniSettings(p *Plugin) (key, path string, interval int) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.globalSettings.IniRateKey, p.globalSettings.IniPath, p.globalSettings.PollInterval
}

// TestBugAIniRateKeySurvivesStaleBlob is the missing test for repro 3: after
// the user picks a non-default rate key (setIniRateKey), a global-settings
// round-trip whose blob LACKS iniRateKey must not clobber it back to "". The
// auto-refresh tests above only covered autoRefresh; this pins the key.
func TestBugAIniRateKeySurvivesStaleBlob(t *testing.T) {
	p := newAutoRefreshTestPlugin(t, 1000)

	// User picks a non-default key (the exact repro-3 action).
	sendSetIniRateKey(p, "HWMonUpdateFreqSensorPanel")
	if key, _, _ := iniSettings(p); key != "HWMonUpdateFreqSensorPanel" {
		t.Fatalf("after setIniRateKey, stored key = %q, want HWMonUpdateFreqSensorPanel", key)
	}

	// A PI reconnect / startup restore returns a blob lacking iniRateKey.
	sendGlobalSettings(p, `{"pollInterval":1000}`)
	if key, _, _ := iniSettings(p); key != "HWMonUpdateFreqSensorPanel" {
		t.Fatalf("a stale blob lacking iniRateKey clobbered the key: stored = %q, want HWMonUpdateFreqSensorPanel", key)
	}
}

// TestBugAReEnablePushesAutoImmediately is repro 1: toggle auto OFF then ON,
// then a stale autoRefresh:false + partial blob arrives. The pushed state
// (piStatusSnapshot, the source of sendStatusToPIs) must report auto ON so the
// readout shows "auto (1000ms, <key>)", not a plain manual value. Asserts on
// the pushed snapshot, not just the stored flag.
func TestBugAReEnablePushesAutoImmediately(t *testing.T) {
	p := newAutoRefreshTestPlugin(t, 1000)
	sendSetPollInterval(p, "2000")   // off (manual)
	sendSetPollInterval(p, `"auto"`) // on again

	// The stale blob the SDK union returns: it still carries the last manual
	// autoRefresh:false, plus a poll interval.
	sendGlobalSettings(p, `{"autoRefresh":false,"pollInterval":2000}`)

	_, _, _, autoRefresh, ini := p.piStatusSnapshot()
	if !autoRefresh {
		t.Fatalf("after re-enable + stale blob, pushed autoRefresh = false, want true (the readout would show a manual value)")
	}
	if ini.RateMs != 1000 {
		t.Fatalf("pushed iniRateMs = %d, want 1000 (INI block must stay intact for the auto readout)", ini.RateMs)
	}
	if ini.Key != "HWMonUpdateFreqExtApp" {
		t.Fatalf("pushed iniRateKey = %q, want the default HWMonUpdateFreqExtApp", ini.Key)
	}
}

// TestBugAPartialRoundTripPreservesIniPathAndPollInterval covers DoD #3: a
// round-trip blob carrying neither iniPath nor pollInterval must keep both.
// Before the fix pollInterval clamped to the 1 s default and iniPath to "".
func TestBugAPartialRoundTripPreservesIniPathAndPollInterval(t *testing.T) {
	p := newAutoRefreshTestPlugin(t, 1000)
	path0 := p.globalSettings.IniPath

	// A manual poll interval (distinct from the 1 s clamp).
	sendSetPollInterval(p, "3000")
	_, _, interval0 := iniSettings(p)
	if interval0 != 3000 {
		t.Fatalf("setup: pollInterval = %d, want 3000", interval0)
	}

	// A blob carrying neither iniPath nor pollInterval (e.g. a PI saving a
	// threshold) must not reset them.
	sendGlobalSettings(p, `{"autoRefresh":false}`)

	_, path, interval := iniSettings(p)
	if path != path0 {
		t.Fatalf("iniPath clobbered by a partial round-trip: got %q want %q", path, path0)
	}
	if interval != interval0 {
		t.Fatalf("pollInterval clobbered by a partial round-trip: got %d want %d", interval, interval0)
	}
}

// TestIniRateKeyChangeResolvesNewKeyOnUnmovedFile pins the fix for
// a reported state (INI key selector = HWMonUpdateFreqSensorPanel
// but Current = "auto (1000ms, HWMonUpdateFreqExtApp)"). The INI has already
// been read once (mtime cached), then the user picks another key without the
// file changing. handleIniRateKeyChange must invalidate the mtime cache so
// readIniRate re-parses under the new override instead of re-applying the
// key/rate cached under the old one.
func TestIniRateKeyChangeResolvesNewKeyOnUnmovedFile(t *testing.T) {
	p := newAutoRefreshTestPlugin(t, 1000)
	content := "[Generic]\nHWMonUpdateFreqExtApp=1000\nHWMonUpdateFreqSensorPanel=2000\n"
	if err := os.WriteFile(p.globalSettings.IniPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp INI: %v", err)
	}

	p.readIniRate(true) // prime the mtime cache under the default key
	if _, _, _, _, ini := p.piStatusSnapshot(); ini.Key != "HWMonUpdateFreqExtApp" || ini.RateMs != 1000 {
		t.Fatalf("setup: key=%q rate=%d, want HWMonUpdateFreqExtApp/1000", ini.Key, ini.RateMs)
	}

	sendSetIniRateKey(p, "HWMonUpdateFreqSensorPanel")
	_, _, _, _, ini := p.piStatusSnapshot()
	if ini.Key != "HWMonUpdateFreqSensorPanel" || ini.RateMs != 2000 {
		t.Fatalf("after picking SensorPanel on an unmoved INI: key=%q rate=%d, want HWMonUpdateFreqSensorPanel/2000 (stale cache re-applied)", ini.Key, ini.RateMs)
	}
	if ini.KeySet != "HWMonUpdateFreqSensorPanel" {
		t.Fatalf("KeySet = %q, want HWMonUpdateFreqSensorPanel", ini.KeySet)
	}
}
