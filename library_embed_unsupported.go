//go:build !(linux && (amd64 || arm64)) && !(darwin && (amd64 || arm64)) && !(windows && amd64) && !(android && arm64)

package zerotier

func readBundledLibrary() ([]byte, error) { return nil, ErrUnsupportedPlatform }

const bundledName = ""
