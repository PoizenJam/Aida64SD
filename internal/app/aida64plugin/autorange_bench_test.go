package aida64plugin

import (
	"sync"
	"testing"
	"time"
)

// benchPlugin builds a minimal Plugin for the hot-path auto features. Only the
// fields touched by the per-tick hot path are populated; the rest stay zero.
func benchPlugin(tiles int) *Plugin {
	p := &Plugin{}
	p.mu = sync.RWMutex{}
	p.autoRangeMax = make(map[string]int, tiles)
	for i := 0; i < tiles; i++ {
		p.autoRangeMax[ctxFor(i)] = 100
	}
	return p
}

func ctxFor(i int) string {
	if i == 0 {
		return "ctx0"
	}
	d := ""
	for i > 0 {
		d = string(rune('0'+i%10)) + d
		i /= 10
	}
	return "ctx" + d
}

// Steady state: observed value sits inside the already-grown range, so
// applyAutoRange must do only a map read + two compares and report no change.
func BenchmarkApplyAutoRangeSteady40(b *testing.B) {
	p := benchPlugin(40)
	vals := make([]int, 40)
	for i := range vals {
		vals[i] = 50 + i // all below the 100+ high-water marks
	}
	b.ResetTimer()
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		for i := 0; i < 40; i++ {
			p.applyAutoRange(ctxFor(i), 0, 120, vals[i], false)
		}
	}
}

// Growth state: every tile reports a new high-water mark (a load ramp). This is
// the worst case for auto-range; it drives a SetSettings + render per tile.
func BenchmarkApplyAutoRangeGrowth40(b *testing.B) {
	p := benchPlugin(40)
	b.ResetTimer()
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		base := n % 1000
		for i := 0; i < 40; i++ {
			p.applyAutoRange(ctxFor(i), 0, base+50, base+60+i, false)
		}
	}
}

// The global auto-refresh path runs once per auxiliary sample (NOT per
// tile). This is the steady state: the INI rate is resolved and the ticker
// already sits at the clamped target, so applyAutoRefresh is a single
// GetInterval compare and bails (no SetInterval / persist / status push).
func BenchmarkAutoRefreshSteadyOncePerTick(b *testing.B) {
	p := benchPlugin(40)
	f := true
	p.globalSettings = globalSettings{AutoRefresh: &f} // auto on
	p.am = newActionManager(time.Second)               // ticker already at the INI rate
	p.iniRateMs = 1000
	b.ResetTimer()
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		p.applyAutoRefresh()
	}
}

// The per-read cost of the INI path: parse the fixture and resolve the rate
// key. Runs at most once per minute (60 s mtime-gated watch) plus on manual
// refresh, so this is not a hot path, but it must stay cheap.
func BenchmarkIniRateRead(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		sections := parseIni(aida64IniFixture)
		_, _, _ = iniRateResolution(sections, "")
	}
}
