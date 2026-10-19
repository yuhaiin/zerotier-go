//go:build android && arm64

package zerotier

import _ "embed"

//go:embed native/assets/libztgo_android_arm64.so
var bundledBytes []byte
