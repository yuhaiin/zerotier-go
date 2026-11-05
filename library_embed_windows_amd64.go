//go:build windows && amd64

package zerotier

import "embed"

// An actual verified native library is included automatically once built.
// The NOTICE makes source-only go builds valid, without fake executable bytes.
//
//go:embed native/assets/windows_amd64/*
//go:embed native/assets/ztgo_windows_amd64.dll
var platformAssets embed.FS

// readBundledLibrary is called only when a native node is started or
// EmbeddedLibrary is explicitly requested. Importing this package never
// allocates a heap copy of the embedded native library.
func readBundledLibrary() ([]byte, error) {
	return platformAssets.ReadFile("native/assets/ztgo_windows_amd64.dll")
}

const bundledName = "ztgo_windows_amd64.dll"
