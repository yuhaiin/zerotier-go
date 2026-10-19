//go:build linux && arm64 && !android

package zerotier

import _ "embed"

//go:embed native/assets/libztgo_linux_arm64.so
var bundledBytes []byte
