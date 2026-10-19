//go:build darwin && arm64

package zerotier

import _ "embed"

//go:embed native/assets/libztgo_darwin_arm64.dylib
var bundledBytes []byte
