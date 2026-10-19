//go:build darwin

package zerotier

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func resolvePlatformLibrary() (string, func(), error) {
	exe, err := os.Executable()
	if err != nil {
		return "", nil, err
	}
	dir := filepath.Dir(exe)
	candidates := []string{
		filepath.Join(dir, bundledName),
		filepath.Join(dir, "..", "Frameworks", bundledName),
		filepath.Join(dir, "..", "..", "Contents", "Frameworks", bundledName),
		filepath.Join(dir, "Contents", "Frameworks", bundledName),
	}
	var failures []error
	for _, p := range candidates {
		if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := verifyNativePath(p); err != nil {
			failures = append(failures, &LibraryResolutionError{p, err})
			continue
		}
		return p, func() {}, nil
	}
	if len(failures) > 0 {
		return "", nil, errors.Join(failures...)
	}
	return "", nil, fmt.Errorf("%w: macOS requires an installed signed dylib beside the executable or in Contents/Frameworks (%s)", ErrNoNativeLibrary, bundledName)
}
