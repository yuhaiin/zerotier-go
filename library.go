package zerotier

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
)

var ErrUnsupportedPlatform = errors.New("zerotier: unsupported native platform")

// LibraryResolutionError preserves the unsafe path or missing platform detail.
type LibraryResolutionError struct {
	Path string
	Err  error
}

func (e *LibraryResolutionError) Error() string {
	return fmt.Sprintf("zerotier: native library %q: %v", e.Path, e.Err)
}
func (e *LibraryResolutionError) Unwrap() error { return e.Err }

func libraryName() string {
	switch runtime.GOOS {
	case "darwin":
		return "libztgo_darwin_" + runtime.GOARCH + ".dylib"
	case "windows":
		return "ztgo_windows_" + runtime.GOARCH + ".dll"
	default:
		return "libztgo_" + runtime.GOOS + "_" + runtime.GOARCH + ".so"
	}
}

func resolveLibrary(explicit string) (string, func(), error) {
	if bundledName == "" {
		return "", nil, fmt.Errorf("%w: %s/%s", ErrUnsupportedPlatform, runtime.GOOS, runtime.GOARCH)
	}
	if explicit != "" {
		if err := verifyNativePath(explicit); err != nil {
			return "", nil, &LibraryResolutionError{explicit, err}
		}
		return explicit, func() {}, nil
	}
	return resolvePlatformLibrary()
}

// EmbeddedLibrary returns only the current GOOS/GOARCH's official native core.
// On Android, the caller must package the bytes as jniLibs/arm64-v8a/libztgo.so.
func EmbeddedLibrary() (string, []byte, error) {
	if bundledName == "" {
		return "", nil, fmt.Errorf("%w: %s/%s", ErrUnsupportedPlatform, runtime.GOOS, runtime.GOARCH)
	}
	if len(bundledBytes) == 0 {
		return "", nil, fmt.Errorf("%w: %s", ErrNoNativeLibrary, bundledName)
	}
	return bundledName, bytes.Clone(bundledBytes), nil
}
