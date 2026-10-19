//go:build windows

package zerotier

import "github.com/ebitengine/purego"

// Windows' purego.NewCallback requires exactly one pointer-width result;
// sign-extend INT32 results via uint32 and return the low 32 C ABI bits.
// For void C callbacks, the native side simply ignores the return register.
func makeNativeCallbacks() cCallbacks {
	return cCallbacks{
		Put: purego.NewCallback(func(user uintptr, typ int32, id0, id1 uint64, ptr uintptr, length int32) uintptr {
			onStatePut(user, typ, id0, id1, ptr, length)
			return 0
		}),
		Get: purego.NewCallback(func(user uintptr, typ int32, id0, id1 uint64, ptr uintptr, size uint32) uintptr {
			return uintptr(uint32(onStateGet(user, typ, id0, id1, ptr, size)))
		}),
		Wire: purego.NewCallback(func(user, endpoint, ptr uintptr, size, ttl uint32) uintptr {
			return uintptr(uint32(onWireSend(user, endpoint, ptr, size, ttl)))
		}),
		Frame:  purego.NewCallback(func(user, frame uintptr) uintptr { onFrame(user, frame); return 0 }),
		Config: purego.NewCallback(func(user, view uintptr) uintptr { return uintptr(uint32(onConfig(user, view))) }),
		Event:  purego.NewCallback(func(user uintptr, code int32) uintptr { onEvent(user, code); return 0 }),
	}
}
