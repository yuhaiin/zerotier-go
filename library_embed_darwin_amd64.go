//go:build darwin && amd64

package zerotier

import _ "embed"

//go:embed native/assets/libztgo_darwin_amd64.dylib
var bundledBytes []byte
