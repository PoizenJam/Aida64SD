//go:build !windows
// +build !windows

package aida64plugin

import "errors"

// locateAida64Ini is a no-op off Windows (the plugin host ships for Windows
// only; this exists so the package builds and tests on non-Windows dev machines).
func locateAida64Ini() (string, error) {
	return "", errors.New("AIDA64 auto-detect is only available on Windows")
}
