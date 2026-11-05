package zerotier

import (
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

// bundledLibraryBytes reads an independent copy of the embedded file on demand.
// Nothing is copied to the heap merely because the package was imported.
func bundledLibraryBytes() ([]byte, error) {
	data, err := readBundledLibrary()
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrNoNativeLibrary, bundledName, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoNativeLibrary, bundledName)
	}
	return data, nil
}

// EmbeddedLibrary returns only the current GOOS/GOARCH's official native core.
// On Android, the caller must package the bytes as jniLibs/arm64-v8a/libztgo.so.
func EmbeddedLibrary() (string, []byte, error) {
	if bundledName == "" {
		return "", nil, fmt.Errorf("%w: %s/%s", ErrUnsupportedPlatform, runtime.GOOS, runtime.GOARCH)
	}
	data, err := bundledLibraryBytes()
	if err != nil {
		return "", nil, err
	}
	// embed.FS.ReadFile returns a fresh []byte; callers may mutate it safely.
	return bundledName, data, nil
}
