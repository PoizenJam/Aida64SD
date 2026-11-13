//go:build windows
// +build windows

package aida64plugin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// locateAida64Ini finds AIDA64.INI. It gathers every AIDA64 install the
// auto-detect can see (Uninstall registry, native then WOW64 views, before a
// Program Files\FinalWire scan) and returns the one most likely to be the
// running AIDA64: the first whose INI holds a [Generic] HWMonUpdateFreq* key.
// The caller treats "not found" as auto-detect failure and falls back to the
// manual poll interval.
//
// Ranking by rate-key presence guards against a leftover or older install
// whose AIDA64.INI lacks the update-frequency keys being picked over the live
// one. A user-set path bypasses detection entirely (readIniRate uses it as-is).
func locateAida64Ini() (string, error) {
	best := bestIniPath(locateAida64IniCandidates())
	if best == "" {
		return "", errors.New("AIDA64.INI not found (no Uninstall key, no Program Files folder)")
	}
	return best, nil
}

// locateAida64IniCandidates enumerates every AIDA64 install the auto-detect
// can see, in preference order: the Uninstall registry (native view, then the
// WOW64 32-bit view, where AIDA64 registers as a 32-bit app) before the
// Program Files scan. Each entry is a folder path whose AIDA64.INI exists.
// Duplicates are dropped keeping the higher-preference source, so the ranking
// (bestIniPath) sees each install once.
func locateAida64IniCandidates() []string {
	var out []string
	for _, access := range []uint32{uint32(registry.READ), uint32(registry.READ | registry.WOW64_32KEY)} {
		out = append(out, locateAida64IniRegistryCandidates(access)...)
	}
	out = append(out, locateAida64IniProgramFilesCandidates()...)
	seen := map[string]bool{}
	uniq := make([]string, 0, len(out))
	for _, p := range out {
		if !seen[p] {
			seen[p] = true
			uniq = append(uniq, p)
		}
	}
	return uniq
}

// locateAida64IniRegistryCandidates returns the INI paths of every AIDA64*
// Uninstall entry (one registry view) whose install folder holds AIDA64.INI.
// Subkey names are GUIDs, so the product name must be read per entry; a few
// hundred open/read/close cycles is acceptable at the 60 s watch cadence (the
// common case is a cached path that is never re-detected).
func locateAida64IniRegistryCandidates(access uint32) []string {
	const uninstallPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, uninstallPath, access)
	if err != nil {
		return nil
	}
	subKeys, err := k.ReadSubKeyNames(-1)
	k.Close()
	if err != nil {
		return nil
	}
	var out []string
	for _, sub := range subKeys {
		entry, err := registry.OpenKey(registry.LOCAL_MACHINE, uninstallPath+`\`+sub, access)
		if err != nil {
			continue
		}
		name, _, _ := entry.GetStringValue("DisplayName")
		if !strings.HasPrefix(strings.ToLower(name), "aida64") {
			entry.Close()
			continue
		}
		for _, valName := range []string{"InstallLocation", "DisplayIcon"} {
			val, _, verr := entry.GetStringValue(valName)
			if verr != nil {
				continue
			}
			val = strings.Trim(val, `"`)
			if val == "" {
				continue
			}
			// InstallLocation is a folder; DisplayIcon is the aida64.exe
			// path, so take its parent.
			if filepath.Ext(val) != "" {
				val = filepath.Dir(val)
			}
			if st, err := os.Stat(filepath.Join(val, "AIDA64.INI")); err == nil && !st.IsDir() {
				out = append(out, filepath.Join(val, "AIDA64.INI"))
				break
			}
		}
		entry.Close()
	}
	return out
}

// locateAida64IniProgramFilesCandidates scans the FinalWire product folders
// under Program Files (both 64-bit and x86) for AIDA64* folders that hold
// AIDA64.INI.
func locateAida64IniProgramFilesCandidates() []string {
	var out []string
	roots := []string{}
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		roots = append(roots, pf)
		if pf86 := os.Getenv("ProgramFiles(x86)"); pf86 != "" {
			roots = append(roots, pf86)
		}
	}
	for _, root := range roots {
		fw := filepath.Join(root, "FinalWire")
		entries, err := os.ReadDir(fw)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(strings.ToLower(e.Name()), "aida64") {
				continue
			}
			candidate := filepath.Join(fw, e.Name(), "AIDA64.INI")
			if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
				out = append(out, candidate)
			}
		}
	}
	return out
}
