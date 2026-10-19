//go:build windows && amd64

package zerotier

import "embed"

// An actual verified native library is included automatically once built.
// The NOTICE makes source-only go builds valid, without fake executable bytes.
//
//go:embed native/assets/windows_amd64/*
var platformAssets embed.FS

var bundledBytes, _ = platformAssets.ReadFile("native/assets/windows_amd64/ztgo_windows_amd64.dll")

const bundledName = "ztgo_windows_amd64.dll"
