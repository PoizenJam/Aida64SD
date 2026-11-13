package aida64plugin

import (
	"errors"
	"testing"
	"time"
)

// A bridge process that is alive but not answering is never restarted by the
// watchdog (it only sees exited processes). The liveness sampler must detach
// it after bridgeRestartAfterFailures consecutive failed calls so the
// watchdog/restart path brings up a fresh one.
func TestWedgedBridgeIsRestarted(t *testing.T) {
	rt := &bridgeRuntime{hw: stubHardwareService{sourcesErr: errors.New("context deadline exceeded")}}
	p := &Plugin{bridge: rt, am: newActionManager(defaultPollInterval)}

	for i := 0; i < bridgeRestartAfterFailures-1; i++ {
		p.updateBridgeLiveness()
	}
	rt.mu.RLock()
	stillAttached := rt.hw != nil
	rt.mu.RUnlock()
	if !stillAttached {
		t.Fatalf("bridge detached after %d failures, want %d before restart", bridgeRestartAfterFailures-1, bridgeRestartAfterFailures)
	}

	p.updateBridgeLiveness()
	deadline := time.Now().Add(5 * time.Second)
	for {
		rt.mu.RLock()
		detached := rt.hw == nil
		rt.mu.RUnlock()
		if detached {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("wedged bridge was not detached for restart")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The restart itself fails here (no bridge binary in the test dir); wait
	// for it to finish so it cannot race the next test.
	for {
		rt.mu.RLock()
		busy := rt.starting
		rt.mu.RUnlock()
		if !busy {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// AIDA64 being closed is not a bridge failure: the bridge answers with
// available=false. That must read Disconnected without ever restarting.
func TestClosedAida64DoesNotRestartBridge(t *testing.T) {
	off := false
	rt := &bridgeRuntime{hw: stubHardwareService{available: &off}}
	p := &Plugin{bridge: rt, am: newActionManager(defaultPollInterval)}
	for i := 0; i < 3*bridgeRestartAfterFailures; i++ {
		p.updateBridgeLiveness()
	}
	rt.mu.RLock()
	attached, failures := rt.hw != nil, rt.failures
	rt.mu.RUnlock()
	if !attached || failures != 0 {
		t.Fatalf("closed AIDA64: attached=%v failures=%d, want attached with 0 failures", attached, failures)
	}
	if status, _, _, _, _ := p.piStatusSnapshot(); status != "Disconnected" {
		t.Fatalf("closed AIDA64: status = %q, want Disconnected", status)
	}
}

// A failure streak shorter than the threshold resets on the next success.
func TestBridgeFailureStreakResets(t *testing.T) {
	rt := &bridgeRuntime{hw: stubHardwareService{sourcesErr: errors.New("timeout")}}
	p := &Plugin{bridge: rt, am: newActionManager(defaultPollInterval)}
	p.updateBridgeLiveness()
	p.updateBridgeLiveness()
	rt.mu.Lock()
	rt.hw = stubHardwareService{}
	rt.mu.Unlock()
	p.updateBridgeLiveness()
	rt.mu.RLock()
	failures := rt.failures
	rt.mu.RUnlock()
	if failures != 0 {
		t.Fatalf("failures after a successful call = %d, want 0", failures)
	}
}
