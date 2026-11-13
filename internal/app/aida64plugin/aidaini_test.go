package aida64plugin

import (
	"strings"
	"testing"
	"time"
)

// aida64IniFixture mirrors the [Generic] rate block of a real AIDA64.INI
// (verified key set, values in milliseconds) plus the parser edge cases:
// BOM, CRLF, comments, non-rate keys, and a second section.
const aida64IniFixture = "\ufeff[Generic]\r\n" +
	"LangID=9\r\n" +
	"; comment line\r\n" +
	"# hash comment\r\n" +
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
	"MainWndX=100\r\n"

func TestParseIniSectionsAndKeys(t *testing.T) {
	sections := parseIni(aida64IniFixture)

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
	if got := generic["hwmonenablesensoricons"]; got != "0" {
		t.Fatalf("HWMonEnableSensorIcons = %q, want 0", got)
	}
	// Comments must not leak into the map.
	if _, ok := generic["; comment line"]; ok {
		t.Fatal("comment line parsed as a key")
	}
	layout, ok := sections["layout"]
	if !ok {
		t.Fatal("missing [layout] section")
	}
	if got := layout["mainwndx"]; got != "100" {
		t.Fatalf("MainWndX = %q, want 100", got)
	}
}

func TestIniRateMsValueAndErrors(t *testing.T) {
	sections := parseIni(aida64IniFixture)

	ms, ok := iniRateMs(sections, "HWMonUpdateFreqExtApp")
	if !ok || ms != 1000 {
		t.Fatalf("iniRateMs = %d ok=%v, want 1000 ok", ms, ok)
	}
	if _, ok := iniRateMs(sections, "NoSuchKey"); ok {
		t.Fatal("missing key must not be ok")
	}
	bad := parseIni("[Generic]\r\nKey=abc\r\n")
	if _, ok := iniRateMs(bad, "Key"); ok {
		t.Fatal("non-numeric value must not be ok")
	}
	zero := parseIni("[Generic]\r\nKey=0\r\n")
	if _, ok := iniRateMs(zero, "Key"); ok {
		t.Fatal("zero value must not be ok")
	}
}

func TestIniRateResolutionPriority(t *testing.T) {
	sections := parseIni(aida64IniFixture)

	// Explicit override wins when present.
	key, ms, note := iniRateResolution(sections, "HWMonUpdateFreqSensorPage")
	if key != "HWMonUpdateFreqSensorPage" || ms != 1000 || note != "override key" {
		t.Fatalf("override resolution = %q %d %q", key, ms, note)
	}

	// Empty override falls to the default key.
	key, ms, note = iniRateResolution(sections, "")
	if key != iniRateKeyDefault || ms != 1000 || note != "default key" {
		t.Fatalf("default resolution = %q %d %q", key, ms, note)
	}

	// Default key absent from the file falls back to the first known key.
	absent := strings.Replace(aida64IniFixture, "HWMonUpdateFreqExtApp=1000\r\n", "", 1)
	sections2 := parseIni(absent)
	key, ms, note = iniRateResolution(sections2, "")
	if key != "HWMonUpdateFreqSensorPage" || ms != 1000 || note != "fallback key (default key missing)" {
		t.Fatalf("missing-default resolution = %q %d %q", key, ms, note)
	}

	// No rate key at all: nothing, and the note says why.
	empty := parseIni("[Generic]\r\nLangID=9\r\n")
	key, ms, note = iniRateResolution(empty, "")
	if key != "" || ms != 0 || note != "no HWMonUpdateFreq* key found" {
		t.Fatalf("empty resolution = %q %d %q", key, ms, note)
	}
}

func TestClampIniRateMs(t *testing.T) {
	if d := clampIniRateMs(100); d != 250*time.Millisecond {
		t.Fatalf("clamp(100) = %v, want 250ms floor", d)
	}
	if d := clampIniRateMs(1000); d != time.Second {
		t.Fatalf("clamp(1000) = %v, want 1s", d)
	}
	if d := clampIniRateMs(60000); d != 10*time.Second {
		t.Fatalf("clamp(60000) = %v, want 10s ceiling", d)
	}
}

func TestIniRateKeyNamesList(t *testing.T) {
	names := iniRateKeyNames()
	if len(names) != 11 {
		t.Fatalf("iniRateKeyNames = %d entries, want 11", len(names))
	}
	if names[0] != "HWMonUpdateFreqSensorPage" || names[10] != "HWMonUpdateFreqAlert" {
		t.Fatalf("iniRateKeyNames order wrong: first=%q last=%q", names[0], names[10])
	}
	found := false
	for _, n := range names {
		if n == iniRateKeyDefault {
			found = true
		}
	}
	if !found {
		t.Fatalf("default key %s not in selector list", iniRateKeyDefault)
	}
}
