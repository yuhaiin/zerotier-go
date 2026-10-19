//go:build android && arm64

package zerotier

import "embed"

// An actual verified native library is included automatically once built.
// The NOTICE makes source-only go builds valid, without fake executable bytes.
//
//go:embed native/assets/android_arm64/*
var platformAssets embed.FS

var bundledBytes, _ = platformAssets.ReadFile("native/assets/android_arm64/libztgo_android_arm64.so")

const bundledName = "libztgo_android_arm64.so"
