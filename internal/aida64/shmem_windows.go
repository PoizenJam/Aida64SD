//go:build windows

package aida64

/*
#include <windows.h>
// Shim so Go need not juggle the LPVOID/LPCVOID distinction between the two APIs.
static BOOL aida_unmap(LPVOID p) { return UnmapViewOfFile((LPCVOID)p); }
*/
import "C"

import (
	"fmt"
	"syscall"
	"unsafe"
)

// MappingName is the AIDA64 shared-memory block. AIDA64 creates it in both
// local and global space; the bare name resolves in either.
const MappingName = "AIDA64_SensorValues"

// ReadBlock copies the NUL-terminated sensor string out of shared memory.
// It returns a fresh slice so the view and handle are released before return.
// No defer wraps the C pointer: the cgo pointer checker rejects a Go closure
// (defer) that captures a C pointer, so cleanup is done explicitly.
func ReadBlock() ([]byte, error) {
	lpName := C.CString(MappingName)
	defer C.free(unsafe.Pointer(lpName))

	hnd := C.OpenFileMapping(C.FILE_MAP_READ, 0, lpName)
	if hnd == nil {
		return nil, fmt.Errorf("OpenFileMapping %q: %w", MappingName, lastError())
	}

	addr := C.MapViewOfFile(hnd, C.FILE_MAP_READ, 0, 0, 0)
	if addr == nil {
		syscall.CloseHandle(syscall.Handle(unsafe.Pointer(hnd)))
		return nil, fmt.Errorf("MapViewOfFile: %w", lastError())
	}

	// Scan for the NUL terminator up to maxBuf instead of trusting the
	// mapping size; the block may be larger than the payload.
	buf := unsafe.Slice((*byte)(unsafe.Pointer(addr)), maxBuf)
	n := 0
	for n < maxBuf && buf[n] != 0 {
		n++
	}
	out := make([]byte, n)
	copy(out, buf[:n])

	C.aida_unmap(addr)
	syscall.CloseHandle(syscall.Handle(unsafe.Pointer(hnd)))
	return out, nil
}

func lastError() error {
	return fmt.Errorf("win32 error %d", uint32(C.GetLastError()))
}
