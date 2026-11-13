//go:build !windows

package plugin

import "errors"

// Reader is a no-op on non-Windows so the service logic (ticker, snapshot
// cache, freshness token) stays buildable and testable off Windows. The
// bridge binary itself only ships for Windows.
var Reader = func() ([]byte, error) {
	return nil, errors.New("AIDA64 shared memory requires Windows")
}
