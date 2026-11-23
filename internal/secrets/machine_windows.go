//go:build windows

package secrets

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// MachineGuid is not a password. It is stable for this Windows install and
// is not part of Tako's config directory, so a copied secrets file does not
// decrypt elsewhere. Reading it does not touch Credential Manager.

var (
	modadvapi32          = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyExW    = modadvapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW = modadvapi32.NewProc("RegQueryValueExW")
	procRegCloseKey      = modadvapi32.NewProc("RegCloseKey")
)

const (
	hkeyLocalMachine = 0x80000002
	keyRead          = 0x20019
	regSZ            = 1
)

func platformBinding() ([]byte, error) {
	path, err := syscall.UTF16PtrFromString(`SOFTWARE\Microsoft\Cryptography`)
	if err != nil {
		return nil, fmt.Errorf("reading MachineGuid: %w", err)
	}
	name, err := syscall.UTF16PtrFromString("MachineGuid")
	if err != nil {
		return nil, fmt.Errorf("reading MachineGuid: %w", err)
	}
	var handle syscall.Handle
	status, _, _ := procRegOpenKeyExW.Call(
		uintptr(hkeyLocalMachine),
		uintptr(unsafe.Pointer(path)),
		0,
		uintptr(keyRead),
		uintptr(unsafe.Pointer(&handle)),
	)
	runtime.KeepAlive(path)
	if status != 0 {
		return nil, fmt.Errorf("reading MachineGuid: %w", syscall.Errno(status))
	}
	defer func() { _, _, _ = procRegCloseKey.Call(uintptr(handle)) }()

	buf := make([]uint16, 128)
	var typ uint32
	n := uint32(len(buf) * 2)
	status, _, _ = procRegQueryValueExW.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(name)),
		0,
		uintptr(unsafe.Pointer(&typ)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&n)),
	)
	runtime.KeepAlive(name)
	runtime.KeepAlive(buf)
	if status != 0 {
		return nil, fmt.Errorf("reading MachineGuid: %w", syscall.Errno(status))
	}
	if typ != regSZ {
		return nil, errors.New("reading MachineGuid: unexpected type")
	}
	id, err := normalizeMachineID([]byte(syscall.UTF16ToString(buf)))
	if err != nil {
		return nil, fmt.Errorf("reading MachineGuid: %w", err)
	}
	return []byte("windows:" + id), nil
}
