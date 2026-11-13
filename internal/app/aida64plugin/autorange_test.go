package aida64plugin

import (
	"testing"
	"time"
)

func TestAutoRangeGrowsMaxOnly(t *testing.T) {
	// Observed value above max grows the max.
	if mn, mx := autoRange(0, 100, 120, false); mn != 0 || mx != 120 {
		t.Fatalf("autoRange(0,100,120) = %d/%d, want 0/120", mn, mx)
	}
	// A drop does not shrink the max (the high-water mark is the caller's job).
	if mn, mx := autoRange(0, 120, 10, false); mn != 0 || mx != 120 {
		t.Fatalf("autoRange(0,120,10) = %d/%d, want 0/120", mn, mx)
	}
	// maxOnly keeps the floor at 0 (usage can exceed 100 but never dips below 0).
	if mn, mx := autoRange(0, 100, -5, true); mn != 0 || mx != 100 {
		t.Fatalf("autoRange(0,100,-5,maxOnly) = %d/%d, want 0/100", mn, mx)
	}
	// Usage over 100 (GPU TDP% hit 101.2) still grows.
	if mn, mx := autoRange(0, 100, 101, true); mn != 0 || mx != 101 {
		t.Fatalf("autoRange(0,100,101,maxOnly) = %d/%d, want 0/101", mn, mx)
	}
}

func TestAutoRangeEnabledPointerSemantics(t *testing.T) {
	// nil and true both mean "auto"; only explicit false pins.
	if !autoRangeEnabled(nil) {
		t.Fatal("nil must mean auto (default)")
	}
	f := false
	tr := true
	if autoRangeEnabled(&f) {
		t.Fatal("explicit false must pin the range")
	}
	if !autoRangeEnabled(&tr) {
		t.Fatal("explicit true must be auto")
	}
}

func TestApplyAutoRangeHighWaterMark(t *testing.T) {
	p := &Plugin{autoRangeMax: make(map[string]int)}

	// First observation above the default grows the range.
	if _, _, changed := p.applyAutoRange("ctx", 0, 100, 120, false); !changed {
		t.Fatal("expected a change on first observation above max")
	}
	// Next tick: the stored high-water mark keeps the max even if the value
	// (and the settings min/max) regressed.
	if mn, mx, changed := p.applyAutoRange("ctx", 0, 100, 50, false); changed || mn != 0 || mx != 120 {
		t.Fatalf("applyAutoRange after spike = %d/%d changed=%v, want 0/120 changed=false", mn, mx, changed)
	}
	// A second, higher spike grows again.
	if _, mx, changed := p.applyAutoRange("ctx", 0, 120, 150, false); !changed || mx != 150 {
		t.Fatalf("applyAutoRange second spike = %d changed=%v, want 150 true", mx, changed)
	}
}

func TestDefaultRangeUnits(t *testing.T) {
	cases := []struct {
		unit string
		want [2]int
	}{
		{"C", [2]int{0, 100}}, // Tjmax 95; auto-range covers the overshoot
		{"V", [2]int{0, 16}},  // +12 V rails
		{"RPM", [2]int{0, 3000}},
		{"A", [2]int{0, 30}},
		{"W", [2]int{0, 400}}, // GPU1 hit 399.9 W in the reference log
		{"MHz", [2]int{0, 6000}},
		{"Hz", [2]int{0, 600}},
		{"FPS", [2]int{0, 60}},
		{"KB/s", [2]int{0, 130000}}, // 1 GbE
		{"Mbps", [2]int{0, 1000}},   // link speed
		{"MB/s", [2]int{0, 1000}},   // NVMe
		{"GB/s", [2]int{0, 100}},
		{"MB", [2]int{0, 65536}},
		{"GB", [2]int{0, 4096}},
	}
	for _, c := range cases {
		mn, mx := defaultRange(stubReading{id: "X", unit: c.unit})
		if got := [2]int{mn, mx}; got != c.want {
			t.Errorf("defaultRange(%q) = %v, want %v", c.unit, got, c.want)
		}
	}
	// No unit falls back to a sane 0..100 seed.
	if mn, mx := defaultRange(stubReading{id: "X", unit: ""}); mn != 0 || mx != 100 {
		t.Errorf("defaultRange(no unit) = %d/%d, want 0/100", mn, mx)
	}
}

func TestRePickClearsAutoRangeWatermark(t *testing.T) {
	p := &Plugin{autoRangeMax: make(map[string]int)}
	// A spike grows the context's high-water mark.
	if _, _, changed := p.applyAutoRange("ctx", 0, 100, 150, false); !changed {
		t.Fatal("expected growth on spike")
	}
	// Re-picking a reading resets the scale to the default range and drops the
	// mark, so the old spike cannot pin the new scale on the next tick.
	p.mu.Lock()
	delete(p.autoRangeMax, "ctx")
	p.mu.Unlock()
	if mn, mx, changed := p.applyAutoRange("ctx", 0, 100, 50, false); changed || mn != 0 || mx != 100 {
		t.Fatalf("after re-pick = %d/%d changed=%v, want 0/100 changed=false", mn, mx, changed)
	}
}

// TestApplyAutoRefreshAppliesIniRate pins the INI-based design: auto mode
// follows the rate read from AIDA64.INI (iniRateMs), clamped to the refresh
// window, and re-applies only when the ticker is not already at that rate.
func TestApplyAutoRefreshAppliesIniRate(t *testing.T) {
	f := true
	p := &Plugin{
		bridge:         &bridgeRuntime{hw: stubHardwareService{}},
		am:             newActionManager(5 * time.Second), // manual 5 s ticker
		globalSettings: globalSettings{AutoRefresh: &f},
	}
	p.iniRateMs = 1000 // AIDA64.INI says 1 s
	p.applyAutoRefresh()
	if got := p.am.GetInterval(); got != time.Second {
		t.Fatalf("ticker = %v, want 1s (the INI rate)", got)
	}
	if p.globalSettings.PollInterval != 1000 {
		t.Fatalf("persisted PollInterval = %d, want 1000", p.globalSettings.PollInterval)
	}
	// Steady state: the ticker is already at the INI rate, so a re-apply is a
	// no-op (no ticker reset, no persist churn).
	p.applyAutoRefresh()
	if got := p.am.GetInterval(); got != time.Second {
		t.Fatalf("steady-state ticker = %v, want 1s", got)
	}
}

// TestApplyAutoRefreshFallsBackToManual pins the no-estimator contract: with
// auto on but no INI rate resolved (iniRateMs = 0) the manual PollInterval
// stays in force, untouched.
func TestApplyAutoRefreshFallsBackToManual(t *testing.T) {
	f := true
	manual := 2 * time.Second
	p := &Plugin{
		bridge:         &bridgeRuntime{hw: stubHardwareService{}},
		am:             newActionManager(manual),
		globalSettings: globalSettings{AutoRefresh: &f, PollInterval: 2000},
	}
	p.iniRateMs = 0 // INI not detected / no rate key
	p.applyAutoRefresh()
	if got := p.am.GetInterval(); got != manual {
		t.Fatalf("ticker = %v, want 2s (the manual interval, unchanged)", got)
	}
	if p.globalSettings.PollInterval != 2000 {
		t.Fatalf("persisted PollInterval = %d, want 2000 (manual, unchanged)", p.globalSettings.PollInterval)
	}
}

// TestApplyAutoRefreshClampsIniRate pins the clamp: an INI rate above the
// 10 s refresh window applies at the ceiling.
func TestApplyAutoRefreshClampsIniRate(t *testing.T) {
	f := true
	p := &Plugin{
		bridge:         &bridgeRuntime{hw: stubHardwareService{}},
		am:             newActionManager(500 * time.Millisecond),
		globalSettings: globalSettings{AutoRefresh: &f},
	}
	p.iniRateMs = 60000 // AIDA64's own max (60 s) is outside the plugin's window
	p.applyAutoRefresh()
	if got := p.am.GetInterval(); got != 10*time.Second {
		t.Fatalf("ticker = %v, want 10s (clamped)", got)
	}
}
