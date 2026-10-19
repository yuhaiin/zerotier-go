//go:build !windows

package zerotier

import "github.com/ebitengine/purego"

func makeNativeCallbacks() cCallbacks {
	return cCallbacks{
		Put: purego.NewCallback(onStatePut), Get: purego.NewCallback(onStateGet),
		Wire: purego.NewCallback(onWireSend), Frame: purego.NewCallback(onFrame),
		Config: purego.NewCallback(onConfig), Event: purego.NewCallback(onEvent),
	}
}
