//go:build windows

package zerotier

import (
	"sync"
	"syscall"
)

var dllRegistry = struct {
	sync.Mutex
	entries map[uintptr]*syscall.DLL
}{entries: make(map[uintptr]*syscall.DLL)}

func openLibrary(path string) (uintptr, func() error, error) {
	d, err := syscall.LoadDLL(path)
	if err != nil {
		return 0, nil, err
	}
	key := uintptr(d.Handle)
	dllRegistry.Lock()
	dllRegistry.entries[key] = d
	dllRegistry.Unlock()
	return key, func() error {
		dllRegistry.Lock()
		delete(dllRegistry.entries, key)
		dllRegistry.Unlock()
		return d.Release()
	}, nil
}
func findSymbol(lib uintptr, name string) (uintptr, error) {
	dllRegistry.Lock()
	d := dllRegistry.entries[lib]
	dllRegistry.Unlock()
	if d == nil {
		return 0, syscall.EINVAL
	}
	p, err := d.FindProc(name)
	if err != nil {
		return 0, err
	}
	return p.Addr(), nil
}