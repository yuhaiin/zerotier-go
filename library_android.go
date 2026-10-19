//go:build android

package zerotier

import "errors"

func resolvePlatformLibrary() (string, func(), error) {
	// Android's dynamic loader resolves this from APK jniLibs. Never extract an
	// executable library into app-writable cache or tmp.
	if bundledName == "" {
		return "", nil, ErrUnsupportedPlatform
	}
	return "libztgo.so", func() {}, nil
}
func verifyNativePath(path string) error {
	if path == "libztgo.so" {
		return nil
	}
	return errors.New("Android requires packaged jniLibs rather than a writable/external native library path")
}
