//go:build linux && !android

package zerotier

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

func resolvePlatformLibrary() (string, func(), error) {
	if len(bundledBytes) == 0 {
		return "", nil, fmt.Errorf("%w: %s", ErrNoNativeLibrary, bundledName)
	}
	if p, cleanup, err := sealedMemfd(bundledBytes); err == nil {
		return p, cleanup, nil
	}
	return privateNativeFile(bundledBytes)
}

func sealedMemfd(data []byte) (string, func(), error) {
	// Linux amd64 and arm64 system-call numbers; do not use a writable /tmp file.
	number := uintptr(319)
	if linuxMemfdNumber != 0 {
		number = linuxMemfdNumber
	}
	name := []byte("zerotier-core\x00")
	fd, _, errno := syscall.Syscall(number, uintptr(unsafe.Pointer(&name[0])), 3, 0) // CLOEXEC | ALLOW_SEALING
	if errno != 0 {
		return "", nil, errno
	}
	f := os.NewFile(fd, "zerotier-core")
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return "", nil, err
	}
	if _, _, errno = syscall.Syscall(syscall.SYS_FCNTL, fd, 1033, 15); errno != 0 {
		_ = f.Close()
		return "", nil, errno
	} // F_ADD_SEALS: WRITE,GROW,SHRINK,SEAL
	return fmt.Sprintf("/proc/self/fd/%d", fd), func() { _ = f.Close() }, nil
}

func privateNativeFile(data []byte) (string, func(), error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", nil, err
	}
	dir := filepath.Join(home, ".cache", "yuhaiin", "zerotier", "native")
	if err = preparePrivateNativeDir(dir); err != nil {
		return "", nil, err
	}
	var suffix [16]byte
	if _, err = rand.Read(suffix[:]); err != nil {
		return "", nil, err
	}
	path := filepath.Join(dir, "ztgo-"+hex.EncodeToString(suffix[:])+".so")
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0700)
	if err != nil {
		return "", nil, &LibraryResolutionError{path, err}
	}
	f := os.NewFile(uintptr(fd), path)
	cleanup := func() { _ = os.Remove(path) }
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		cleanup()
		return "", nil, err
	}
	if err = verifyNativePath(path); err != nil {
		cleanup()
		return "", nil, &LibraryResolutionError{path, err}
	}
	return path, cleanup, nil
}
