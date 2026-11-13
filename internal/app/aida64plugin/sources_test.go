package aida64plugin

import (
	"testing"
	"time"

	"github.com/poizenjam/aida64sd/pkg/streamdeck"
)

// TestStalenessWindow pins the 60 s content-token window that the tile-level
// staleness gate uses. It must stay far larger than HWiNFO's 5 s: an idle box
// has AIDA64 rewrite byte-identical content for many seconds, so the
// freshness token goes quiet while everything is healthy.
func TestStalenessWindow(t *testing.T) {
	if stalenessWindow != 60*time.Second {
		t.Fatalf("stalenessWindow = %v, want 60s", stalenessWindow)
	}
}

// TestBugBLivenessDrivesConnectionStatus (primary liveness path): the bridge
// process stays alive after AIDA64 closes and keeps returning the stale
// content-change token (PollTime never errors on a read failure). So the
// Connection status must be driven by the bridge's "block opened and parsed on
// the last tick" flag, NOT the token: available=true => Connected,
// available=false (block gone) => Disconnected even while PollTime still serves
// a non-zero stale token. This is the exact defect an earlier PollTime-error
// probe could not catch (the token is structurally non-zero).
func TestBugBLivenessDrivesConnectionStatus(t *testing.T) {
	on := true
	off := false
	p := &Plugin{
		bridge: &bridgeRuntime{hw: stubHardwareService{pollTime: 12345, available: &on}},
		am:     newActionManager(defaultPollInterval),
	}

	// Block available, non-zero token: Connected.
	p.updateBridgeLiveness()
	if status, _, _, _, _ := p.piStatusSnapshot(); status != "Connected" {
		t.Fatalf("available=true: status = %q, want Connected", status)
	}

	// AIDA64 closed: the bridge still serves the SAME stale token (PollTime
	// non-zero, no error) but the block is gone. This is the reported bug: the
	// old code kept reading Connected because the token never went to 0.
	p.bridge.hw = stubHardwareService{pollTime: 12345, available: &off}
	p.updateBridgeLiveness()
	if status, _, _, _, _ := p.piStatusSnapshot(); status != "Disconnected" {
		t.Fatalf("available=false (stale token still served): status = %q, want Disconnected", status)
	}

	// AIDA64 reopens the block: available=true again, same token: Connected.
	p.bridge.hw = stubHardwareService{pollTime: 12345, available: &on}
	p.updateBridgeLiveness()
	if status, _, _, _, _ := p.piStatusSnapshot(); status != "Connected" {
		t.Fatalf("available=true again: status = %q, want Connected (reconnected)", status)
	}
}

// TestBugBUpdateBridgeLivenessRefreshesFlag pins the sampler helper: it reads
// Sources()[0].Available into bridgeAvailable and reports only a real
// transition (so the sampler pushes a status only on a flip). hw=nil leaves the
// flag alone and reports no change.
func TestBugBUpdateBridgeLivenessRefreshesFlag(t *testing.T) {
	on := true
	off := false
	p := &Plugin{
		bridge: &bridgeRuntime{hw: stubHardwareService{available: &on}},
		am:     newActionManager(defaultPollInterval),
	}

	// First read flips the default false to true: a change.
	if !p.updateBridgeLiveness() {
		t.Fatal("first read (available=true) should report a change")
	}
	if !p.bridgeAvailable {
		t.Fatal("bridgeAvailable not set true after an available=true read")
	}
	// Unchanged: no transition.
	if p.updateBridgeLiveness() {
		t.Fatal("second read (still available=true) should report no change")
	}
	// Block gone: a change back to false.
	p.bridge.hw = stubHardwareService{available: &off}
	if !p.updateBridgeLiveness() {
		t.Fatal("available=false read should report a change")
	}
	if p.bridgeAvailable {
		t.Fatal("bridgeAvailable not cleared after an available=false read")
	}
	// No bridge: returns false and leaves the flag alone.
	p.bridge.hw = nil
	if p.updateBridgeLiveness() {
		t.Fatal("hw=nil should report no change")
	}
	if p.bridgeAvailable {
		t.Fatal("hw=nil must not change the flag")
	}
}

// TestBugBAppliedAIDA64TerminateFastPath (secondary, fast path): the SDK
// application terminate/launch events are the fast path for an instant flip.
// Terminate clears the flag + cache and pushes Disconnected immediately; a
// non-AIDA64 app event is ignored; relaunch does not force Connected (it waits
// for the block to actually become available again via the liveness flag). This
// test does not rely on the terminate event being the only mechanism - the 1 s
// sampler carries the fix even if the SDK never fires these hooks.
func TestBugBAppliedAIDA64TerminateFastPath(t *testing.T) {
	on := true
	off := false
	p := &Plugin{
		bridge: &bridgeRuntime{hw: stubHardwareService{pollTime: 12345, available: &on}},
		am:     newActionManager(defaultPollInterval),
	}
	p.updateBridgeLiveness()
	if status, _, _, _, _ := p.piStatusSnapshot(); status != "Connected" {
		t.Fatalf("setup: status = %q, want Connected (block available)", status)
	}

	// Terminate is the fast path: flag cleared, cache cleared, Disconnected now.
	p.OnApplicationDidTerminate(&streamdeck.EvApplication{Payload: streamdeck.EvApplicationPayload{Application: "AIDA64.EXE"}})
	if p.bridgeAvailable {
		t.Fatal("bridgeAvailable not cleared after AIDA64 terminate")
	}
	if p.bridge.cachedPollTime != 0 {
		t.Fatalf("cache not cleared after AIDA64 terminate: cachedPollTime = %d, want 0", p.bridge.cachedPollTime)
	}
	if status, _, _, _, _ := p.piStatusSnapshot(); status != "Disconnected" {
		t.Fatalf("status = %q after AIDA64 terminate, want Disconnected (no restart required)", status)
	}

	// A non-AIDA64 app event must be ignored (state unchanged).
	p.OnApplicationDidTerminate(&streamdeck.EvApplication{Payload: streamdeck.EvApplicationPayload{Application: "chrome.exe"}})
	if status, _, _, _, _ := p.piStatusSnapshot(); status != "Disconnected" {
		t.Fatalf("status = %q after a non-AIDA64 terminate, want still Disconnected", status)
	}

	// Relaunch: the block is not open yet, so the flag stays false and the
	// status stays Disconnected; once the block reopens the sampler flips it to
	// Connected.
	p.OnApplicationDidLaunch(&streamdeck.EvApplication{Payload: streamdeck.EvApplicationPayload{Application: "AIDA64.exe"}})
	p.bridge.hw = stubHardwareService{pollTime: 12345, available: &off}
	p.updateBridgeLiveness()
	if status, _, _, _, _ := p.piStatusSnapshot(); status != "Disconnected" {
		t.Fatalf("status = %q while the block is still closing after relaunch, want Disconnected", status)
	}
	p.bridge.hw = stubHardwareService{pollTime: 12345, available: &on}
	p.updateBridgeLiveness()
	if status, _, _, _, _ := p.piStatusSnapshot(); status != "Connected" {
		t.Fatalf("status = %q after the block reopens, want Connected (reconnected)", status)
	}
}

// TestPollTimeForUIDSingleSource: AIDA64 has exactly one local source, so the
// poll time that gates every tile is the shared content-change token,
// independent of the (category) sensor id.
func TestPollTimeForUIDSingleSource(t *testing.T) {
	p := &Plugin{
		bridge: &bridgeRuntime{hw: stubHardwareService{}},
		am:     newActionManager(defaultPollInterval),
	}
	// Seed the cache with a known token so getCachedPollTime returns it.
	p.bridge.cachedPollTime = 12345
	p.bridge.cachedAt = time.Now()
	p.pollTimeCacheTTL = time.Second

	for _, uid := range []string{"temp", "volt", "fan", "pwr"} {
		pt, err := p.pollTimeForUID(uid)
		if err != nil {
			t.Fatalf("pollTimeForUID(%q) error = %v", uid, err)
		}
		if pt != 12345 {
			t.Fatalf("pollTimeForUID(%q) = %d, want 12345 (shared local token)", uid, pt)
		}
	}
}
