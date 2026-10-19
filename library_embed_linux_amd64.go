//go:build linux && amd64 && !android

package zerotier

import "embed"

// An actual verified native library is included automatically once built.
// The NOTICE makes source-only go builds valid, without fake executable bytes.
//
//go:embed native/assets/linux_amd64/*
//go:embed native/assets/libztgo_linux_amd64.so
var platformAssets embed.FS

var bundledBytes, _ = platformAssets.ReadFile("native/assets/libztgo_linux_amd64.so")

const bundledName = "libztgo_linux_amd64.so"
