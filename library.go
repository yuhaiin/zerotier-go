package zerotier

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// assets contains generated binaries ONLY after the build workflow succeeds;
// README.txt is retained so source-only module builds remain possible.
//
//go:embed native/assets/*
var bundled embed.FS

func libraryName() string {
	switch runtime.GOOS {
	case "darwin":
		return "libztgo_darwin_" + runtime.GOARCH + ".dylib"
	case "windows":
		return "ztgo_windows_" + runtime.GOARCH + ".dll"
	default:
		return "libztgo_" + runtime.GOOS + "_" + runtime.GOARCH + ".so"
	}
}
func resolveLibrary(explicit string) (string, func(), error) {
	if explicit != "" {
		return explicit, func() {}, nil
	}
	name := libraryName()
	blob, err := bundled.ReadFile("native/assets/" + name)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %s", ErrNoNativeLibrary, name)
	}
	dir, err := os.MkdirTemp("", "zerotier-native-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, name)
	if err = os.WriteFile(path, blob, 0700); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}