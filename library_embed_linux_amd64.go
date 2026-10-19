//go:build linux && amd64 && !android

package zerotier

import _ "embed"

//go:embed native/assets/libztgo_linux_amd64.so
var bundledBytes []byte
