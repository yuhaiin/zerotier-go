//go:build linux && !android

package zerotier

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSealedMemfd(t *testing.T) {
	data := []byte("temporary ELF bytes used for loader unit test")
	path, closeFn, err := sealedMemfd(data)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			t.Skipf("memfd blocked by sandbox: %v", err)
		}
		t.Fatal(err)
	}
	defer closeFn()
	if !strings.HasPrefix(path, "/proc/self/fd/") {
		t.Fatal(path)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != string(data) {
		t.Fatalf("memfd %v: %q", err, b)
	}
}
func TestPrivateLibraryAndSymlinkRejected(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.so")
	if err := os.WriteFile(real, []byte("sample"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyNativePath(real); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.so")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := verifyNativePath(link); err == nil {
		t.Fatal("symbolic link incorrectly trusted")
	}
	if err := verifyNativePath("relative.so"); err == nil {
		t.Fatal("relative path incorrectly trusted")
	}
}
func TestPrivateFileIsExclusiveAndOwned(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, cleanup, err := privateNativeFile([]byte("ELF"))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if err = verifyNativePath(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatalf("not private: %v", info.Mode())
	}
	if !strings.Contains(path, "/yuhaiin/zerotier/native/") {
		t.Fatal(path)
	}
}
