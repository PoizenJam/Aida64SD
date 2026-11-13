//go:build windows

package plugin

import "github.com/poizenjam/aida64sd/internal/aida64"

// Reader is the cgo reader of the AIDA64_SensorValues block.
var Reader = aida64.ReadBlock
