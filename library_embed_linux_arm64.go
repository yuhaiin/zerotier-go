//go:build linux && arm64 && !android

package zerotier

import "embed"

// An actual verified native library is included automatically once built.
// The NOTICE makes source-only go builds valid, without fake executable bytes.
//
//go:embed native/assets/linux_arm64/*
//go:embed native/assets/libztgo_linux_arm64.so
var platformAssets embed.FS

// readBundledLibrary is called only when a native node is started or
// EmbeddedLibrary is explicitly requested. Importing this package never
// allocates a heap copy of the embedded native library.
func readBundledLibrary() ([]byte, error) {
	return platformAssets.ReadFile("native/assets/libztgo_linux_arm64.so")
}

const bundledName = "libztgo_linux_arm64.so"
