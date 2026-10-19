//go:build darwin && arm64

package zerotier

import "embed"

// An actual verified native library is included automatically once built.
// The NOTICE makes source-only go builds valid, without fake executable bytes.
//
//go:embed native/assets/darwin_arm64/*
var platformAssets embed.FS

var bundledBytes, _ = platformAssets.ReadFile("native/assets/darwin_arm64/libztgo_darwin_arm64.dylib")

const bundledName = "libztgo_darwin_arm64.dylib"
