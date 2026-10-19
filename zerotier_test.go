package zerotier

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"
)

func TestNetworkID(t *testing.T) {
	for _, s := range []string{"0000000000000001", "8056c2e21c000001", "ffffffffffffffff"} {
		id, err := ParseNetworkID(s)
		if err != nil || id.String() != s {
			t.Fatalf("round trip %q: %v", s, err)
		}
	}
	for _, s := range []string{"", "0", "0000000000000000", "invalid-zerotier", "10000000000000000"} {
		if _, err := ParseNetworkID(s); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
func TestMemoryStoreCopiesAndConcurrent(t *testing.T) {
	s := NewMemoryStore()
	k := StateKey{Type: 1, ID: [2]uint64{2, 3}}
	if _, err := s.Get(k); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	payload := []byte("secret")
	if err := s.Put(k, payload); err != nil {
		t.Fatal(err)
	}
	payload[0] = 'x'
	b, err := s.Get(k)
	if err != nil || string(b) != "secret" {
		t.Fatalf("got %s %v", b, err)
	}
	b[0] = 'y'
	b, err = s.Get(k)
	if err != nil || string(b) != "secret" {
		t.Fatalf("got %s %v", b, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = s.Get(k); _ = s.Put(k, []byte("value")) }()
	}
	wg.Wait()
	if err = s.Delete(k); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(k); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
func TestFileStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := StateKey{Type: 11, ID: [2]uint64{1234, 12345}}
	if _, err = store.Get(key); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	for _, value := range []string{"one", "two"} {
		if err = store.Put(key, []byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	data, err := store.Get(key)
	if err != nil || string(data) != "two" {
		t.Fatalf("got %q %v", data, err)
	}
	f, err := os.Stat(store.path(key))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && f.Mode().Perm() != 0600 {
		t.Fatalf("unsafe permissions %v", f.Mode())
	}
	if err = store.Delete(key); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Get(key); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	names, err := os.ReadDir(dir)
	if err != nil || len(names) != 0 {
		t.Fatalf("leftovers %v: %v", names, err)
	}
}
func TestAddressABIAndRoundtrip(t *testing.T) {
	if unsafe.Sizeof(cAddr{}) != 28 || unsafe.Sizeof(cRoute{}) != 60 || unsafe.Sizeof(cFrame{}) != 48 {
		t.Fatal("ABI layout drift")
	}
	for _, s := range []string{"127.0.0.1:9993", "[2001:db8::1]:9993", "[fe80::1%42]:9993"} {
		a := netip.MustParseAddrPort(s)
		native, err := fromIP(a)
		if err != nil {
			t.Fatal(err)
		}
		if got := native.addrPort(); got != a {
			t.Fatalf("%s -> %s", a, got)
		}
	}
	for _, s := range []string{"0.0.0.0:1", "[ff02::1]:2", "[fe80::1%eth0]:1"} {
		a := netip.MustParseAddrPort(s)
		if _, err := fromIP(a); err == nil {
			t.Errorf("accepted %s", s)
		}
	}
}
func TestConfigSnapshot(t *testing.T) {
	c := cConfig{NetworkID: 0x123, MAC: 0x987, Operation: uint32(NetworkUp), Status: uint32(NetworkOK), MTU: 2800, AddressCount: 1, RouteCount: 1}
	copy(c.Name[:], "my-network")
	c.Addresses[0] = cAddr{Family: 4, Port: 24, IP: [16]byte{10, 1, 2, 3}}
	c.Routes[0] = cRoute{Target: cAddr{Family: 4, IP: [16]byte{10, 0, 0, 0}, Port: 8}, Via: cAddr{Family: 4, IP: [16]byte{10, 1, 2, 1}}}
	o := c.snapshot()
	if o.Name != "my-network" || o.ID != 0x123 || o.MTU != 2800 || len(o.Addresses) != 1 || len(o.Routes) != 1 {
		t.Fatalf("bad snapshot: %+v", o)
	}
	if o.Addresses[0].String() != "10.1.2.3/24" || o.Routes[0].Via.String() != "10.1.2.1" {
		t.Fatalf("bad IPs: %+v", o)
	}
}
func TestFrameValidation(t *testing.T) {
	if err := (Frame{}).validate(); err == nil {
		t.Fatal("zero network accepted")
	}
	if err := (Frame{NetworkID: 1, SourceMAC: 1 << 48}).validate(); err == nil {
		t.Fatal("bad MAC accepted")
	}
	if err := (Frame{NetworkID: 1, Data: make([]byte, (1<<20)+1)}).validate(); err == nil {
		t.Fatal("oversized frame accepted")
	}
	if err := (Frame{NetworkID: 1}).validate(); err != nil {
		t.Fatal(err)
	}
}
func TestSourceOnlyHasClearNativeError(t *testing.T) {
	name := libraryName()
	if !strings.Contains(name, "ztgo_") {
		t.Fatal(name)
	}
	if _, err := os.Stat(filepath.Join("native", "assets", name)); errors.Is(err, os.ErrNotExist) {
		_, err = New(Config{SendWire: func(WirePacket) error { return nil }})
		if !errors.Is(err, ErrNoNativeLibrary) {
			t.Fatalf("wanted library error, got %v", err)
		}
	}
}
func TestNativeSmokeWhenAvailable(t *testing.T) {
	path := os.Getenv("ZEROTIER_GO_TEST_LIBRARY")
	if path == "" {
		t.Skip("set ZEROTIER_GO_TEST_LIBRARY to test the native official core")
	}
	store := NewMemoryStore()
	n, err := New(Config{LibraryPath: path, Store: store, SendWire: func(WirePacket) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	address, err := n.Address()
	if err != nil || address == 0 {
		t.Fatalf("address %x: %v", address, err)
	}
	if err = n.Join(1); err != nil {
		t.Fatal(err)
	}
	if err = n.Leave(1); err != nil {
		t.Fatal(err)
	}
	if err = n.Close(); err != nil {
		t.Fatal(err)
	}
	if err = n.Join(1); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	// Persistence allows reuse of the same ZeroTier identity without a daemon.
	n2, err := New(Config{LibraryPath: path, Store: store, SendWire: func(WirePacket) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	addr2, err := n2.Address()
	if err != nil || addr2 != address {
		t.Fatalf("identity changed %x: %v", addr2, err)
	}
	if err = n2.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestWaitForNetworkCancelled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	n := &Node{done: make(chan struct{}), networks: make(map[NetworkID]NetworkConfig)}
	_, err := n.WaitForNetwork(ctx, 1)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
func BenchmarkMemoryStore(b *testing.B) {
	s := NewMemoryStore()
	k := StateKey{Type: 1}
	payload := bytes.Repeat([]byte("x"), 256)
	b.ReportAllocs()
	b.SetBytes(256)
	for i := 0; i < b.N; i++ {
		_ = s.Put(k, payload)
		_, _ = s.Get(k)
	}
}
func BenchmarkAddressConversion(b *testing.B) {
	a := netip.MustParseAddrPort("[2001:db8::1]:9993")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		v, _ := fromIP(a)
		_ = v.addrPort()
	}
}
func BenchmarkConfigSnapshot(b *testing.B) {
	var c cConfig
	c.AddressCount = 2
	c.RouteCount = 4
	for i := 0; i < 2; i++ {
		c.Addresses[i] = cAddr{Family: 4, Port: 24, IP: [16]byte{10, 0, 0, byte(i)}}
	}
	for i := 0; i < 4; i++ {
		c.Routes[i] = cRoute{Target: cAddr{Family: 4, Port: 16, IP: [16]byte{10, byte(i)}}}
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = c.snapshot()
	}
}
func BenchmarkNativeFrame(b *testing.B) {
	path := os.Getenv("ZEROTIER_GO_TEST_LIBRARY")
	if path == "" {
		b.Skip("native library not provided")
	}
	n, err := New(Config{LibraryPath: path, SendWire: func(WirePacket) error { return nil }})
	if err != nil {
		b.Fatal(err)
	}
	defer n.Close()
	if err = n.Join(1); err != nil {
		b.Fatal(err)
	}
	f := Frame{NetworkID: 1, SourceMAC: 1, DestinationMAC: 2, EtherType: 0x800, Data: make([]byte, 1400)}
	b.ReportAllocs()
	b.SetBytes(1400)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = n.ProcessFrame(f)
	}
}
func TestEmbeddedLibraryHasNativeFileSignature(t *testing.T) {
	name, data, err := EmbeddedLibrary()
	if err != nil {
		if errors.Is(err, ErrNoNativeLibrary) {
			t.Skip("platform's native asset has not yet been published")
		}
		t.Fatal(err)
	}
	if name != libraryName() || len(data) < 65536 {
		t.Fatalf("invalid embedded library: %s (%d bytes)", name, len(data))
	}
	var ok bool
	switch runtime.GOOS {
	case "linux", "android":
		ok = bytes.HasPrefix(data, []byte{0x7f, 'E', 'L', 'F'})
	case "windows":
		ok = bytes.HasPrefix(data, []byte{'M', 'Z'})
	case "darwin":
		ok = bytes.HasPrefix(data, []byte{0xcf, 0xfa, 0xed, 0xfe}) ||
			bytes.HasPrefix(data, []byte{0xca, 0xfe, 0xba, 0xbe})
	default:
		t.Skip("unsupported native platform")
	}
	if !ok {
		t.Fatalf("embedded library %s does not contain expected object format", name)
	}
	data[0] = 0 // Returned bytes must not mutate the actual embedded data.
	_, again, err := EmbeddedLibrary()
	if err != nil || !okNativeHeader(again) {
		t.Fatal("EmbeddedLibrary exposed mutable global backing memory")
	}
}
func okNativeHeader(data []byte) bool {
	if len(data) < 4 { return false }
	switch runtime.GOOS {
	case "linux", "android": return bytes.HasPrefix(data, []byte{0x7f, 'E', 'L', 'F'})
	case "windows": return bytes.HasPrefix(data, []byte{'M', 'Z'})
	case "darwin": return bytes.HasPrefix(data, []byte{0xcf, 0xfa, 0xed, 0xfe}) || bytes.HasPrefix(data, []byte{0xca, 0xfe, 0xba, 0xbe})
	}
	return false
}
func TestRejectedConfigDoesNotBecomeReady(t *testing.T) {
	n := &Node{done: make(chan struct{}), tasks: make(chan task), networks: make(map[NetworkID]NetworkConfig)}
	n.token = uintptr(nextToken.Add(1))
	activeNodes.Store(n.token, n)
	defer activeNodes.Delete(n.token)
	c := &cConfig{NetworkID: 123, Status: uint32(NetworkOK), Operation: uint32(NetworkUp), MTU: 2800}
	if result := onConfig(n.token, unsafe.Pointer(c)); result == 0 {
		t.Fatal("accepted a config despite a full callback queue")
	}
	if _, ok := n.Network(123); ok {
		t.Fatal("rejected network appeared ready")
	}
	if got := n.DroppedCallbacks(); got != 1 {
		t.Fatalf("expected one drop, got %d", got)
	}
}
func TestWaitForNetworkRejectsDownOperation(t *testing.T) {
	n := &Node{done: make(chan struct{}), networks: map[NetworkID]NetworkConfig{
		1: {ID: 1, Status: NetworkOK, Operation: NetworkDown, Addresses: []netip.Prefix{netip.MustParsePrefix("10.0.0.2/24")}},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err := n.WaitForNetwork(ctx, 1)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("down network incorrectly reported ready: %v", err)
	}
}
