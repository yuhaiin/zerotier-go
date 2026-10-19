//go:build darwin && amd64

package zerotier

import "embed"

// An actual verified native library is included automatically once built.
// The NOTICE makes source-only go builds valid, without fake executable bytes.
//
//go:embed native/assets/darwin_amd64/*
//go:embed native/assets/libztgo_darwin_amd64.dylib
var platformAssets embed.FS

var bundledBytes, _ = platformAssets.ReadFile("native/assets/libztgo_darwin_amd64.dylib")

const bundledName = "libztgo_darwin_amd64.dylib"
