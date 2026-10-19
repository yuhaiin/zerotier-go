//go:build !linux && !darwin && !windows && !android

package zerotier

func resolvePlatformLibrary() (string, func(), error) { return "", nil, ErrUnsupportedPlatform }
func verifyNativePath(string) error                   { return ErrUnsupportedPlatform }
