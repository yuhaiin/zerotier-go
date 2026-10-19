package zerotier

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Each build embeds only its matching OS/architecture library. The native
// files are produced from the pinned official ZeroTierOne Core by CI.
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
	// Android's linker normally rejects executable mappings from writable
	// temporary directories. Supply jniLibs/arm64-v8a/libztgo.so to the APK.
	if runtime.GOOS == "android" {
		return "libztgo.so", func() {}, nil
	}
	if len(bundledBytes) == 0 {
		return "", nil, fmt.Errorf("%w: %s", ErrNoNativeLibrary, libraryName())
	}
	dir, err := os.MkdirTemp("", "zerotier-native-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, libraryName())
	if err = os.WriteFile(path, bundledBytes, 0700); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}

// EmbeddedLibrary returns an independent copy of the current platform's
// official native library; Android hosts should install it as libztgo.so.
func EmbeddedLibrary() (string, []byte, error) {
	if len(bundledBytes) == 0 {
		return "", nil, fmt.Errorf("%w: %s", ErrNoNativeLibrary, libraryName())
	}
	return libraryName(), bytes.Clone(bundledBytes), nil
}
