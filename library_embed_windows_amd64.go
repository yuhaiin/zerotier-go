//go:build windows && amd64

package zerotier

import _ "embed"

//go:embed native/assets/ztgo_windows_amd64.dll
var bundledBytes []byte
