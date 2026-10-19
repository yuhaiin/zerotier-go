//go:build windows

package zerotier

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"
)

var dllRegistry = struct {
	sync.Mutex
	entries map[uintptr]*syscall.DLL
}{entries: make(map[uintptr]*syscall.DLL)}

func openLibrary(path string) (uintptr, func() error, error) {
	d, err := syscall.LoadDLL(path)
	if err != nil {
		return 0, nil, err
	}
	key := uintptr(d.Handle)
	dllRegistry.Lock()
	dllRegistry.entries[key] = d
	dllRegistry.Unlock()
	return key, func() error {
		dllRegistry.Lock()
		delete(dllRegistry.entries, key)
		dllRegistry.Unlock()
		return d.Release()
	}, nil
}
func findSymbol(lib uintptr, name string) (uintptr, error) {
	dllRegistry.Lock()
	d := dllRegistry.entries[lib]
	dllRegistry.Unlock()
	if d == nil {
		return 0, syscall.EINVAL
	}
	p, err := d.FindProc(name)
	if err != nil {
		return 0, err
	}
	return p.Addr(), nil
}

func resolvePlatformLibrary() (string, func(), error) {
	if len(bundledBytes) == 0 {
		return "", nil, fmt.Errorf("%w: %s", ErrNoNativeLibrary, bundledName)
	}
	root := os.Getenv("LOCALAPPDATA")
	if !filepath.IsAbs(root) {
		return "", nil, fmt.Errorf("zerotier: invalid LOCALAPPDATA %q", root)
	}
	// Versioned immutable installation. Never overwrite an existing library.
	dir := filepath.Join(root, "yuhaiin", "zerotier", nativeVersion)
	for _, p := range []string{filepath.Join(root, "yuhaiin"), filepath.Join(root, "yuhaiin", "zerotier"), dir} {
		if err := os.Mkdir(p, 0700); err != nil && !os.IsExist(err) {
			return "", nil, err
		}
		if err := verifyPrivateWindowsDirectory(p); err != nil {
			return "", nil, err
		}
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", nil, err
	}
	path := filepath.Join(dir, "ztgo-"+hex.EncodeToString(id[:])+".dll")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.Remove(path) }
	if _, err = f.Write(bundledBytes); err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		cleanup()
		return "", nil, err
	}
	if err = verifyNativePath(path); err != nil {
		cleanup()
		return "", nil, &LibraryResolutionError{path, err}
	}
	// Pin the inode against concurrent writer/reparse replacement; the loader
	// can still open it for READ, but other processes cannot open for WRITE.
	wide, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	handle, err := syscall.CreateFile(wide, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return path, func() { _ = syscall.CloseHandle(handle); cleanup() }, nil
}

const nativeVersion = "v1" // bump whenever native bridge ABI or ZeroTier upstream changes
var procGetNamedSecurityInfo = syscall.NewLazyDLL("advapi32.dll").NewProc("GetNamedSecurityInfoW")

func ownerSID(path string) (string, error) {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	var sid *syscall.SID
	var securityDescriptor uintptr
	ret, _, _ := procGetNamedSecurityInfo.Call(uintptr(unsafe.Pointer(ptr)), 1, 1, uintptr(unsafe.Pointer(&sid)), 0, 0, 0, uintptr(unsafe.Pointer(&securityDescriptor)))
	if ret != 0 {
		return "", syscall.Errno(ret)
	}
	defer syscall.LocalFree(syscall.Handle(securityDescriptor))
	if sid == nil {
		return "", fmt.Errorf("no owner SID")
	}
	return sid.String()
}
func currentSID() (string, error) {
	tok, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return "", err
	}
	defer tok.Close()
	user, err := tok.GetTokenUser()
	if err != nil {
		return "", err
	}
	return user.User.Sid.String()
}
func verifyPrivateWindowsDirectory(path string) error {
	if err := verifyNativePath(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %q", path)
	}
	owner, err := ownerSID(path)
	if err != nil {
		return err
	}
	current, err := currentSID()
	if err != nil {
		return err
	}
	if owner != current {
		return fmt.Errorf("directory %q is not owned by current user", path)
	}
	return nil
}
func verifyNativePath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("library path is not absolute")
	}
	part := filepath.Clean(path)
	for {
		info, err := os.Lstat(part)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link %q rejected", part)
		}
		p, err := syscall.UTF16PtrFromString(part)
		if err != nil {
			return err
		}
		attr, err := syscall.GetFileAttributes(p)
		if err != nil {
			return err
		}
		if attr&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fmt.Errorf("reparse-point path %q rejected", part)
		}
		// The volume root is managed by Windows / the volume provisioner,
		// not necessarily by the process user or a privileged SID. GitHub's
		// Windows runner, for example, gives D:\\ to NETWORK SERVICE.
		// Keep the reparse-point checks above, but do not infer whether the
		// volume is trustworthy from its owner SID. Files and non-root
		// directories still require a trusted owner.
		parent := filepath.Dir(part)
		if parent == part {
			break
		}
		// System and Administrators can own trusted installed directories; native
		// library files must be owned by current user or a privileged principal.
		owner, err := ownerSID(part)
		if err != nil {
			return err
		}
		current, err := currentSID()
		if err != nil {
			return err
		}
		if owner != current && owner != "S-1-5-18" && owner != "S-1-5-32-544" {
			return fmt.Errorf("untrusted owner %s on %q", owner, part)
		}
		part = parent
	}
	return nil
}
