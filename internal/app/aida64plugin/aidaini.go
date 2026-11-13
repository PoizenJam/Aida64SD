package aida64plugin

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// iniRateKeyDefault is the AIDA64.INI [Generic] key whose value drives the
// external-application destinations (shared memory block, registry, WMI),
// which is what this plugin reads. AIDA64 stores one update-frequency key per
// display destination; the key names are stable across the 6.x and 7.x lines
// (verified against a shipped AIDA64.INI). The INI has no documented schema,
// so a wrong guess is possible: the PI shows the key used and offers the full
// list as an override.
const iniRateKeyDefault = "HWMonUpdateFreqExtApp"

// iniRateKeys are the known [Generic] HWMonUpdateFreq* keys, in the order the
// AIDA64 Update Frequency dialog lists them. The PI selector and the key-
// missing fallback draw from this list.
var iniRateKeys = []string{
	"HWMonUpdateFreqSensorPage",
	"HWMonUpdateFreqGPUPage",
	"HWMonUpdateFreqSensorIcons",
	"HWMonUpdateFreqOSD",
	"HWMonUpdateFreqVSB",
	"HWMonUpdateFreqLCD",
	"HWMonUpdateFreqSensorPanel",
	"HWMonUpdateFreqRGBLED",
	"HWMonUpdateFreqLogFile",
	"HWMonUpdateFreqExtApp",
	"HWMonUpdateFreqAlert",
}

// parseIni parses a Windows-style INI file into map[section]map[key]string
// with both maps keyed in lower case (AIDA64's INI is case-insensitive in
// practice, and so is the GetPrivateProfileValue API it is written by). The
// parser only handles what AIDA64 actually emits: [section] headers,
// key=value lines, CRLF, a UTF-8 BOM, and ; / # comments. Values keep their
// surrounding content verbatim except for the line endings.
func parseIni(text string) map[string]map[string]string {
	// A Windows INI written by AIDA64 can start with a UTF-8 BOM; TrimSpace
	// will not remove it (U+FEFF is a format char, not whitespace), so strip
	// it here or the first section header is missed.
	text = strings.TrimPrefix(text, "\ufeff")
	out := map[string]map[string]string{}
	section := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(strings.TrimSpace(line), "\r")
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, ";"), strings.HasPrefix(line, "#"):
			continue
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if _, ok := out[section]; !ok {
				out[section] = map[string]string{}
			}
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 || section == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:eq]))
		val := strings.TrimSpace(line[eq+1:])
		if key == "" {
			continue
		}
		if _, ok := out[section]; !ok {
			out[section] = map[string]string{}
		}
		out[section][key] = val
	}
	return out
}

// iniRateMs reads one update-frequency key from parsed INI content. The first
// section that contains the key wins (AIDA64 keeps them in [Generic]); the
// value must be an integer of milliseconds. ok is false for a missing key,
// empty value, or non-numeric value.
func iniRateMs(sections map[string]map[string]string, key string) (ms int, ok bool) {
	k := strings.ToLower(key)
	for _, kv := range sections {
		if v, found := kv[k]; found {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n <= 0 {
				return 0, false
			}
			return n, true
		}
	}
	return 0, false
}

// iniRateResolution is how the host resolves which [Generic] rate key to
// apply. Order:
//  1. the explicit override when it is present in the INI;
//  2. the default (HWMonUpdateFreqExtApp) when present;
//  3. the first known key present in file order (covers an AIDA64 build that
//     renamed or dropped the default key);
//  4. nothing (the plugin falls back to the manual poll interval).
//
// Returns the key used, its millisecond value, and a human-readable note
// describing the resolution (surfaced in the PI readout).
func iniRateResolution(sections map[string]map[string]string, override string) (key string, ms int, note string) {
	try := func(k string) (int, bool) {
		if strings.TrimSpace(k) == "" {
			return 0, false
		}
		return iniRateMs(sections, k)
	}
	if m, ok := try(override); ok {
		return override, m, "override key"
	}
	if m, ok := try(iniRateKeyDefault); ok {
		return iniRateKeyDefault, m, "default key"
	}
	for _, k := range iniRateKeys {
		if m, ok := try(k); ok {
			return k, m, "fallback key (default key missing)"
		}
	}
	return "", 0, "no HWMonUpdateFreq* key found"
}

// iniHasRateKey reports whether the parsed INI holds at least one usable
// [Generic] HWMonUpdateFreq* key (any section; the keys live in [Generic]). It
// is the presence signal the auto-detector uses to prefer an install whose INI
// actually carries rate settings over a leftover install whose INI lacks them.
func iniHasRateKey(sections map[string]map[string]string) bool {
	for _, k := range iniRateKeys {
		if ms, ok := iniRateMs(sections, k); ok && ms > 0 {
			return true
		}
	}
	return false
}

// bestIniPath picks the INI most likely to belong to the running AIDA64: the
// first candidate whose INI holds a [Generic] HWMonUpdateFreq* key (the live
// install always has them; a leftover/orphan install often does not). When
// none carry the keys it falls back to the first candidate, preserving the
// original "first found wins" order (registry before Program Files). It is
// cross-platform (it only reads files) so the ranking is unit-testable
// without Windows.
func bestIniPath(candidates []string) string {
	for _, p := range candidates {
		if iniFileHasRateKey(p) {
			return p
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}

// iniFileHasRateKey reads and parses one INI and reports whether it holds a
// [Generic] HWMonUpdateFreq* key (the live-install signal). A read error
// counts as "no keys" so a missing or corrupt file never wins the ranking.
func iniFileHasRateKey(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return iniHasRateKey(parseIni(string(data)))
}

// clampIniRateMs bounds an INI rate to the plugin's refresh window (the same
// 250 ms..10 s as manual poll intervals, so the applied ticker never
// saturates or stalls). AIDA64's own UI offers 250 ms..60 s, so only the top
// of the range is ever hit in practice.
func clampIniRateMs(ms int) time.Duration {
	d := time.Duration(ms) * time.Millisecond
	if d < 250*time.Millisecond {
		d = 250 * time.Millisecond
	}
	if d > 10*time.Second {
		d = 10 * time.Second
	}
	return d
}

// iniRateKeyNames is the label list the PI selector renders (the raw key
// names, which are short and self-explanatory).
func iniRateKeyNames() []string {
	out := make([]string, len(iniRateKeys))
	copy(out, iniRateKeys)
	return out
}

// fmtIniRateNote renders the resolution note for the PI readout.
func fmtIniRateNote(key, note string) string {
	if key == "" {
		return note
	}
	return key + " (" + note + ")"
}

// errIniKeyMissing is the sentinel message shown when the override key is
// absent from the file; the resolution already fell back, so this only
// annotates, it never blocks.
const errIniKeyMissing = "override key not in file"

func noteWithOverrideWarning(note, override string) string {
	if strings.TrimSpace(override) != "" && !strings.Contains(note, "override key") {
		return fmt.Sprintf("%s; %s", note, errIniKeyMissing)
	}
	return note
}
