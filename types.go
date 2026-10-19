// Package zerotier embeds the official ZeroTierOne protocol core using a native
// C ABI bridge and purego, without CGO in the Go binding. The library exposes
// packets rather than system TUN devices or an additional userspace TCP stack.
package zerotier

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

var (
	ErrClosed          = errors.New("zerotier: node closed")
	ErrNoNativeLibrary = errors.New("zerotier: native library unavailable; install a matching embedded bundle or set Config.LibraryPath")
	ErrQueueFull       = errors.New("zerotier: callback queue full")
	ErrNotFound        = errors.New("zerotier: state not found")
)

// NetworkID is the 64-bit identifier shown in ZeroTier Central. It is
// intentionally distinct from the decimal uint64 form: strings are hex.
type NetworkID uint64

func ParseNetworkID(s string) (NetworkID, error) {
	if len(s) != 16 {
		return 0, fmt.Errorf("zerotier: network ID must have exactly 16 hex digits")
	}
	n, err := strconv.ParseUint(s, 16, 64)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("zerotier: invalid network ID %q", s)
	}
	return NetworkID(n), nil
}
func (n NetworkID) String() string { return fmt.Sprintf("%016x", uint64(n)) }

// StateKey identifies a native core state object. Store implementations should
// persist all keys, not just the identity, to preserve planet and peer caches.
type StateKey struct {
	Type int32
	ID   [2]uint64
}

type StateStore interface {
	Get(StateKey) ([]byte, error)
	Put(StateKey, []byte) error
	Delete(StateKey) error
}

// WirePacket contains a complete encrypted ZeroTier physical UDP packet.
// Native callback data is copied before this value is passed to application code.
type WirePacket struct {
	To   netip.AddrPort
	Data []byte
	TTL  uint32
}

// Frame is a complete virtual Ethernet payload (without Ethernet header).
// MAC values occupy the lower 48 bits of the uint64 fields.
type Frame struct {
	NetworkID                 NetworkID
	SourceMAC, DestinationMAC uint64
	EtherType, VLAN           uint16
	Data                      []byte
}

type Route struct {
	Target        netip.Prefix
	Via           netip.Addr
	Flags, Metric uint16
}

type NetworkStatus uint32

const (
	NetworkRequestingConfiguration NetworkStatus = 0
	NetworkOK                      NetworkStatus = 1
	NetworkAccessDenied            NetworkStatus = 2
	NetworkNotFound                NetworkStatus = 3
	NetworkPortError               NetworkStatus = 4
	NetworkClientTooOld            NetworkStatus = 5
	NetworkAuthenticationRequired  NetworkStatus = 6
)

type NetworkOperation uint32

const (
	NetworkUp        NetworkOperation = 1
	NetworkUpdated   NetworkOperation = 2
	NetworkDown      NetworkOperation = 3
	NetworkDestroyed NetworkOperation = 4
)

// NetworkConfig is a snapshot of settings distributed by the network
// controller. Routes are informational: this library NEVER modifies OS routes.
type NetworkConfig struct {
	ID        NetworkID
	Name      string
	MAC       uint64
	Status    NetworkStatus
	Operation NetworkOperation
	MTU       int
	Addresses []netip.Prefix
	Routes    []Route
}

type Event struct{ Code int32 }

// Config accepts application-owned physical I/O. ZeroTier's wire packets must
// be sent through an underlay that does not recursively route through this node.
// All handlers run from a dedicated callback worker, not the native stack.
// The wire sender and frame receiver MUST be safe to call concurrently with Close.
type Config struct {
	LibraryPath     string
	Store           StateStore
	SendWire        func(WirePacket) error
	OnFrame         func(Frame)
	OnNetworkConfig func(NetworkConfig)
	OnEvent         func(Event)
	LowBandwidth    bool
	EncryptedHello  bool
	// QueueSize controls the bounded native-to-Go delivery queue (default 256).
	QueueSize int
}

func (f Frame) validate() error {
	if f.NetworkID == 0 {
		return errors.New("zerotier: zero network ID")
	}
	if f.SourceMAC>>48 != 0 || f.DestinationMAC>>48 != 0 {
		return errors.New("zerotier: invalid MAC")
	}
	if len(f.Data) > 1<<20 {
		return errors.New("zerotier: frame exceeds 1 MiB")
	}
	return nil
}

func trimCString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return strings.TrimSpace(string(b))
}