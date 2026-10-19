package zerotier

import (
	"errors"
	"fmt"
	"net/netip"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// The bridge uses fixed-width C types. Compile-time checks catch ABI drift in
// Go layout, while the C library has its own independent static assertions.
type cAddr struct {
	IP       [16]byte
	Family   uint32
	Port     uint16
	Reserved uint16
	Scope    uint32
}
type cRoute struct {
	Target, Via   cAddr
	Flags, Metric uint16
}
type cConfig struct {
	NetworkID, MAC                                   uint64
	Operation, Status, MTU, AddressCount, RouteCount uint32
	Name                                             [128]byte
	Addresses                                        [32]cAddr
	Routes                                           [128]cRoute
}
type cFrame struct {
	NetworkID, SourceMAC, DestinationMAC uint64
	EtherType, VLAN                      uint32
	Data                                 unsafe.Pointer
	Length, Reserved                     uint32
}
type cCallbacks struct{ Put, Get, Wire, Frame, Config, Event uintptr }

var _ [28 - unsafe.Sizeof(cAddr{})]byte
var _ [unsafe.Sizeof(cAddr{}) - 28]byte
var _ [60 - unsafe.Sizeof(cRoute{})]byte
var _ [unsafe.Sizeof(cRoute{}) - 60]byte

func fromIP(a netip.AddrPort) (cAddr, error) {
	ip := a.Addr()
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() {
		return cAddr{}, fmt.Errorf("zerotier: invalid physical address %s", a)
	}
	var v cAddr
	if ip.Is4() || ip.Is4In6() {
		v.Family = 4
		ip4 := ip.Unmap().As4()
		copy(v.IP[:], ip4[:])
	} else {
		v.Family = 6
		v.IP = ip.As16()
	}
	v.Port = a.Port()
	if ip.Zone() != "" {
		scope, e := strconv.ParseUint(ip.Zone(), 10, 32)
		if e != nil {
			return cAddr{}, fmt.Errorf("zerotier: zone must be numeric interface index: %w", e)
		}
		v.Scope = uint32(scope)
	}
	return v, nil
}
func (v cAddr) addr() netip.Addr {
	switch v.Family {
	case 4:
		var b [4]byte
		copy(b[:], v.IP[:4])
		return netip.AddrFrom4(b)
	case 6:
		a := netip.AddrFrom16(v.IP)
		if v.Scope != 0 {
			a = a.WithZone(strconv.FormatUint(uint64(v.Scope), 10))
		}
		return a
	default:
		return netip.Addr{}
	}
}
func (v cAddr) addrPort() netip.AddrPort { return netip.AddrPortFrom(v.addr(), v.Port) }
func (c *cConfig) snapshot() NetworkConfig {
	n := NetworkConfig{ID: NetworkID(c.NetworkID), Name: trimCString(c.Name[:]), MAC: c.MAC, Status: NetworkStatus(c.Status), Operation: NetworkOperation(c.Operation), MTU: int(c.MTU)}
	for _, a := range c.Addresses[:min(int(c.AddressCount), len(c.Addresses))] {
		ip := a.addr()
		if !ip.IsValid() {
			continue
		}
		bits := int(a.Port)
		if bits > ip.BitLen() {
			continue
		}
		n.Addresses = append(n.Addresses, netip.PrefixFrom(ip, bits))
	}
	for _, r := range c.Routes[:min(int(c.RouteCount), len(c.Routes))] {
		ip := r.Target.addr()
		if !ip.IsValid() || int(r.Target.Port) > ip.BitLen() {
			continue
		}
		n.Routes = append(n.Routes, Route{Target: netip.PrefixFrom(ip, int(r.Target.Port)), Via: r.Via.addr(), Flags: r.Flags, Metric: r.Metric})
	}
	return n
}

var callbacksOnce sync.Once
var callbacks cCallbacks
var activeNodes sync.Map // uintptr token -> *Node; Go pointers NEVER cross C ABI.
var nextToken atomic.Uint64

func registerCallbacks() {
	callbacksOnce.Do(func() {
		callbacks = cCallbacks{
			Put: purego.NewCallback(onStatePut), Get: purego.NewCallback(onStateGet),
			Wire: purego.NewCallback(onWireSend), Frame: purego.NewCallback(onFrame),
			Config: purego.NewCallback(onConfig), Event: purego.NewCallback(onEvent),
		}
	})
}
func lookup(user uintptr) *Node {
	v, ok := activeNodes.Load(user)
	if !ok {
		return nil
	}
	return v.(*Node)
}
func ptrBytes(ptr uintptr, size uint32) []byte {
	if ptr == 0 || size == 0 {
		return nil
	}
	// Native input size is bounded before allocation, never retain native memory.
	return append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(ptr)), int(size))...)
}
func onStatePut(user uintptr, typ int32, id0, id1 uint64, ptr uintptr, length int32) {
	n := lookup(user)
	if n == nil {
		return
	}
	key := normalizeStateKey(StateKey{Type: typ, ID: [2]uint64{id0, id1}})
	var err error
	if length < 0 {
		err = n.cfg.Store.Delete(key)
	} else if length <= 8<<20 {
		err = n.cfg.Store.Put(key, ptrBytes(ptr, uint32(length)))
	} else {
		err = errors.New("zerotier: excessive state object size")
	}
	if err != nil {
		n.setStoreError(err)
	}
}
func onStateGet(user uintptr, typ int32, id0, id1 uint64, ptr uintptr, size uint32) int32 {
	n := lookup(user)
	if n == nil {
		return -1
	}
	b, err := n.cfg.Store.Get(normalizeStateKey(StateKey{Type: typ, ID: [2]uint64{id0, id1}}))
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			n.setStoreError(err)
		}
		return -1
	}
	if len(b) > int(size) || len(b) > 8<<20 || (len(b) > 0 && ptr == 0) {
		return -1
	}
	if len(b) > 0 {
		copy(unsafe.Slice((*byte)(unsafe.Pointer(ptr)), len(b)), b)
	}
	return int32(len(b))
}
func onWireSend(user, endpoint, ptr uintptr, size, ttl uint32) int32 {
	n := lookup(user)
	if n == nil || endpoint == 0 || size > 1<<20 {
		return -1
	}
	a := (*cAddr)(unsafe.Pointer(endpoint)).addrPort()
	if !a.IsValid() {
		return -1
	}
	if n.offer(task{wire: &WirePacket{To: a, Data: ptrBytes(ptr, size), TTL: ttl}}) {
		return 0
	}
	return -1
}
func onFrame(user, frame uintptr) {
	n := lookup(user)
	if n == nil || frame == 0 {
		return
	}
	f := (*cFrame)(unsafe.Pointer(frame))
	if f.Length > 1<<20 {
		return
	}
	_ = n.offer(task{frame: &Frame{NetworkID: NetworkID(f.NetworkID), SourceMAC: f.SourceMAC, DestinationMAC: f.DestinationMAC, EtherType: uint16(f.EtherType), VLAN: uint16(f.VLAN), Data: ptrBytes(uintptr(f.Data), f.Length)}})
}
func onConfig(user, view uintptr) int32 {
	n := lookup(user)
	if n == nil || view == 0 {
		return -1
	}
	cfg := (*cConfig)(unsafe.Pointer(view)).snapshot()
	n.netMu.Lock()
	if cfg.Operation == NetworkDestroyed {
		delete(n.networks, cfg.ID)
	} else {
		n.networks[cfg.ID] = cfg
	}
	n.netMu.Unlock()
	if n.offer(task{config: &cfg}) {
		return 0
	}
	return -1
}
func onEvent(user uintptr, code int32) {
	n := lookup(user)
	if n != nil {
		_ = n.offer(task{event: &Event{Code: code}})
	}
}

// Native functions are deliberately bound to this package's own ABI bridge,
// not ZeroTierOne's platform-dependent sockaddr_storage or C long types.
type nativeAPI struct {
	closeHandle  func() error
	version      func() uint32
	newNode      func(*uintptr, uintptr, *cCallbacks, uint32, int64) int32
	deleteNode   func(uintptr)
	address      func(uintptr) uint64
	join         func(uintptr, uint64) int32
	leave        func(uintptr, uint64) int32
	orbit        func(uintptr, uint64, uint64) int32
	deorbit      func(uintptr, uint64) int32
	subscribe    func(uintptr, uint64, uint64, uint32) int32
	unsubscribe  func(uintptr, uint64, uint64, uint32) int32
	processWire  func(uintptr, int64, *cAddr, unsafe.Pointer, uint32, *int64) int32
	processFrame func(uintptr, int64, *cFrame, *int64) int32
	tick         func(uintptr, int64, *int64) int32
}

func loadAPI(path string) (*nativeAPI, error) {
	path, cleanup, err := resolveLibrary(path)
	if err != nil {
		return nil, err
	}
	lib, closeLib, err := openLibrary(path)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("zerotier: load %q: %w", path, err)
	}
	a := &nativeAPI{}
	a.closeHandle = func() error { err := closeLib(); cleanup(); return err }
	bind := func(dst any, symbol string) error {
		addr, e := findSymbol(lib, symbol)
		if e != nil {
			return fmt.Errorf("zerotier: symbol %s: %w", symbol, e)
		}
		purego.RegisterFunc(dst, addr)
		return nil
	}
	bindings := []struct {
		target any
		symbol string
	}{
		{&a.version, "ztgo_abi_version"}, {&a.newNode, "ztgo_new"}, {&a.deleteNode, "ztgo_delete"},
		{&a.address, "ztgo_address"}, {&a.join, "ztgo_join"}, {&a.leave, "ztgo_leave"},
		{&a.orbit, "ztgo_orbit"}, {&a.deorbit, "ztgo_deorbit"}, {&a.subscribe, "ztgo_subscribe"}, {&a.unsubscribe, "ztgo_unsubscribe"},
		{&a.processWire, "ztgo_process_wire"}, {&a.processFrame, "ztgo_process_frame"}, {&a.tick, "ztgo_tick"},
	}
	for _, b := range bindings {
		if err = bind(b.target, b.symbol); err != nil {
			_ = a.closeHandle()
			return nil, err
		}
	}
	if got := a.version(); got != 1 {
		_ = a.closeHandle()
		return nil, fmt.Errorf("zerotier: bridge ABI version %d, expected 1", got)
	}
	return a, nil
}
func nowMillis() int64 { return time.Now().UnixMilli() }
func nativePacketPointer(b []byte) unsafe.Pointer {
	if len(b) == 0 {
		return nil
	}
	return unsafe.Pointer(&b[0])
}
func nativeCallError(op string, code int32) error {
	if code == 0 {
		return nil
	}
	return fmt.Errorf("zerotier: %s returned native error %d", op, code)
}
func keepNativeAlive(v any) { runtime.KeepAlive(v) }

// Identity and planet are singleton state objects. The core first reads each
// with ID 0 but subsequently writes them with their actual node/world ID.
// Keying these stores by the raw ID silently regenerates the node identity.
func normalizeStateKey(k StateKey) StateKey {
	if k.Type == 1 || k.Type == 2 || k.Type == 3 {
		k.ID = [2]uint64{}
	}
	return k
}