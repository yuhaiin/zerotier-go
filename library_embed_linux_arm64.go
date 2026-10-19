//go:build linux && arm64 && !android

package zerotier

import "embed"

// An actual verified native library is included automatically once built.
// The NOTICE makes source-only go builds valid, without fake executable bytes.
//
//go:embed native/assets/linux_arm64/*
var platformAssets embed.FS

var bundledBytes, _ = platformAssets.ReadFile("native/assets/linux_arm64/libztgo_linux_arm64.so")

const bundledName = "libztgo_linux_arm64.so"
