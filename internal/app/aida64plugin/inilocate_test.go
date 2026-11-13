package aida64plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeIni writes path (creating its folder) with the given INI text.
func writeIni(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// aida64IniWithKeys is a realistic AIDA64.INI shape: a BOM, CRLF, the [Generic]
// section carrying all eleven HWMonUpdateFreq* keys, plus unrelated sections
// and keys before and after (the file AIDA64 actually emits has many sections).
// It is what "the live install" looks like.
const aida64IniWithKeys = "\ufeff[General]\r\n" +
	"Version=7.20.4000\r\n" +
	"LangID=9\r\n" +
	"[Generic]\r\n" +
	"HWMonUpdateFreqSensorPage=1000\r\n" +
	"HWMonUpdateFreqGPUPage=1000\r\n" +
	"HWMonUpdateFreqSensorIcons=1000\r\n" +
	"HWMonUpdateFreqOSD=1000\r\n" +
	"HWMonUpdateFreqVSB=1000\r\n" +
	"HWMonUpdateFreqLCD=1000\r\n" +
	"HWMonUpdateFreqSensorPanel=1000\r\n" +
	"HWMonUpdateFreqRGBLED=1000\r\n" +
	"HWMonUpdateFreqLogFile=1000\r\n" +
	"HWMonUpdateFreqExtApp=1000\r\n" +
	"HWMonUpdateFreqAlert=1000\r\n" +
	"HWMonEnableSensorIcons=0\r\n" +
	"[Layout]\r\n" +
	"MainWndX=120\r\n" +
	"MainWndY=80\r\n"

// aida64IniWithoutKeys is a leftover/orphan AIDA64.INI that has the [Generic]
// section and other settings but none of the HWMonUpdateFreq* keys - the shape
// that made the readout say "INI has no rate key" when it was located
// ahead of the live install.
const aida64IniWithoutKeys = "\ufeff[General]\r\n" +
	"Version=6.80.3000\r\n" +
	"LangID=9\r\n" +
	"NetUpdateFreq=0\r\n" +
	"[Generic]\r\n" +
	"HWMonEnableSensorIcons=1\r\n" +
	"NoRegistry=1\r\n" +
	"[Layout]\r\n" +
	"MainWndX=100\r\n"

// TestIniHasRateKey pins the presence signal on the realistic INI shapes: the
// live install's INI reports true, the orphan install's reports false.
func TestIniHasRateKey(t *testing.T) {
	if !iniHasRateKey(parseIni(aida64IniWithKeys)) {
		t.Fatal("live-install INI should report a rate key present")
	}
	if iniHasRateKey(parseIni(aida64IniWithoutKeys)) {
		t.Fatal("orphan-install INI (no HWMonUpdateFreq*) should report no rate key")
	}
}

// TestIniFileHasRateKey covers the on-disk variant the auto-detector uses,
// including a missing file (read error => no keys, never wins the ranking).
func TestIniFileHasRateKey(t *testing.T) {
	dir := t.TempDir()
	yes := filepath.Join(dir, "live", "AIDA64.INI")
	no := filepath.Join(dir, "orphan", "AIDA64.INI")
	missing := filepath.Join(dir, "gone", "AIDA64.INI")
	writeIni(t, yes, aida64IniWithKeys)
	writeIni(t, no, aida64IniWithoutKeys)

	if !iniFileHasRateKey(yes) {
		t.Fatal("iniFileHasRateKey(live INI) = false, want true")
	}
	if iniFileHasRateKey(no) {
		t.Fatal("iniFileHasRateKey(orphan INI) = true, want false")
	}
	if iniFileHasRateKey(missing) {
		t.Fatal("iniFileHasRateKey(missing file) = true, want false")
	}
}

// TestBestIniPathPrefersRateKeyInstall is the regression test: with a
// leftover/orphan install located FIRST (the order the auto-detect can produce)
// and the live install second, bestIniPath must return the live one (the INI
// that actually has the rate keys), not the first candidate. This is exactly
// the "INI has no rate key" repro: the old first-found-wins picked the orphan.
func TestBestIniPathPrefersRateKeyInstall(t *testing.T) {
	dir := t.TempDir()
	orphan := filepath.Join(dir, "orphan", "AIDA64.INI") // encountered first
	live := filepath.Join(dir, "live", "AIDA64.INI")     // the running AIDA64
	writeIni(t, orphan, aida64IniWithoutKeys)
	writeIni(t, live, aida64IniWithKeys)

	// Orphan first, as the registry/Program-Files scan can return it.
	got := bestIniPath([]string{orphan, live})
	if got != live {
		t.Fatalf("bestIniPath = %q, want the rate-key install %q (the orphan must not win)", got, live)
	}

	// And it is order-independent: live first still wins.
	if got := bestIniPath([]string{live, orphan}); got != live {
		t.Fatalf("bestIniPath (live first) = %q, want %q", got, live)
	}
}

// TestBestIniPathFallsBackToFirstWhenNoKeys: when none of the candidates have
// the rate keys, bestIniPath falls back to the first candidate (preserving the
// original first-found order) rather than failing outright.
func TestBestIniPathFallsBackToFirstWhenNoKeys(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a", "AIDA64.INI")
	b := filepath.Join(dir, "b", "AIDA64.INI")
	writeIni(t, a, aida64IniWithoutKeys)
	writeIni(t, b, aida64IniWithoutKeys)

	if got := bestIniPath([]string{a, b}); got != a {
		t.Fatalf("bestIniPath (no keys anywhere) = %q, want the first candidate %q", got, a)
	}
	if got := bestIniPath(nil); got != "" {
		t.Fatalf("bestIniPath(nil) = %q, want empty", got)
	}
}

// TestParseRealisticAida64Ini pins the parser against the realistic file shape
// (BOM + CRLF + multiple sections + non-rate keys) so a parser regression that
// would make the live install's keys unparseable is caught.
func TestParseRealisticAida64Ini(t *testing.T) {
	sections := parseIni(aida64IniWithKeys)
	generic, ok := sections["generic"]
	if !ok {
		t.Fatal("missing [generic] section")
	}
	if got := generic["hwmonupdatefreqextapp"]; got != "1000" {
		t.Fatalf("HWMonUpdateFreqExtApp = %q, want 1000", got)
	}
	if got := generic["hwmonupdatefreqsensorpage"]; got != "1000" {
		t.Fatalf("HWMonUpdateFreqSensorPage = %q, want 1000", got)
	}
	// Non-rate keys must not be mistaken for rate keys and must not break the
	// section.
	if got := generic["hwmonenablesensoricons"]; got != "0" {
		t.Fatalf("HWMonEnableSensorIcons = %q, want 0", got)
	}
	if _, ok := sections["layout"]; !ok {
		t.Fatal("missing [layout] section")
	}
}

// TestIniResolutionOnRealisticShapes drives the end-to-end resolution (the
// function readIniRate calls) on both realistic INI shapes: the live install
// resolves the default key at 1000ms, the orphan reports "no rate key". This is
// the resolution the PI readout surfaces, so it is the behavior the ranking fix
// depends on.
func TestIniResolutionOnRealisticShapes(t *testing.T) {
	key, ms, note := iniRateResolution(parseIni(aida64IniWithKeys), "")
	if key != iniRateKeyDefault || ms != 1000 || note != "default key" {
		t.Fatalf("live-install resolution = %q %d %q, want %s 1000 default key", key, ms, note, iniRateKeyDefault)
	}

	key, ms, note = iniRateResolution(parseIni(aida64IniWithoutKeys), "")
	if key != "" || ms != 0 || !strings.Contains(note, "no HWMonUpdateFreq* key found") {
		t.Fatalf("orphan-install resolution = %q %d %q, want empty key / 0 / no-rate-key note", key, ms, note)
	}
}
