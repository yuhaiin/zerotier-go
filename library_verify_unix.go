//go:build (linux && !android) || darwin

package zerotier

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func verifyNativePath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("library path must be absolute")
	}
	cleaned := filepath.Clean(path)
	volume := string(os.PathSeparator)
	part := cleaned
	for part != volume {
		info, err := os.Lstat(part)
		if err != nil {
			return fmt.Errorf("lstat %q: %w", part, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic-link component %q rejected", part)
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("cannot verify owner of %q", part)
		}
		if st.Uid != uint32(os.Getuid()) && st.Uid != 0 {
			return fmt.Errorf("unexpected owner %d on %q", st.Uid, part)
		}
		if part == cleaned {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("native library must be a regular file")
			}
			if info.Mode().Perm()&0022 != 0 {
				return fmt.Errorf("native library is writable by another user")
			}
		} else if !info.IsDir() {
			return fmt.Errorf("non-directory ancestor %q", part)
		}
		part = filepath.Dir(part)
	}
	return nil
}
func preparePrivateNativeDir(dir string) error {
	// Never trust a symlinked cache directory. Privately-owned nested dirs
	// shield the library even when ~/.cache itself is readable by other users.
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if err = verifySecureDirectory(home, false); err != nil {
		return err
	}
	base := filepath.Join(home, ".cache")
	if err = os.MkdirAll(base, 0700); err != nil {
		return err
	}
	if err = verifySecureDirectory(base, false); err != nil {
		return err
	}
	for _, p := range []string{filepath.Join(base, "yuhaiin"), filepath.Join(base, "yuhaiin", "zerotier"), dir} {
		if err = os.Mkdir(p, 0700); err != nil && !os.IsExist(err) {
			return err
		}
		if err = verifySecureDirectory(p, true); err != nil {
			return err
		}
	}
	return nil
}
func verifySecureDirectory(path string, private bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("unsafe native directory %q", path)
	}
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok || s.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("directory not owned by current user: %q", path)
	}
	if info.Mode().Perm()&0022 != 0 || (private && info.Mode().Perm()&0077 != 0) {
		return fmt.Errorf("unsafe permissions %v on %q", info.Mode().Perm(), path)
	}
	return nil
}
