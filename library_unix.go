//go:build !windows

package zerotier

import "github.com/ebitengine/purego"

func openLibrary(path string) (uintptr, func() error, error) {
	lib, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	return lib, func() error { return purego.Dlclose(lib) }, err
}
func findSymbol(lib uintptr, name string) (uintptr, error) { return purego.Dlsym(lib, name) }