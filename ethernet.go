package zerotier

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// DefaultMTU is ZeroTier's usual L3 MTU when a controller has not supplied one.
const DefaultMTU = 2800
const EthernetHeaderSize = 14

var (
	ErrOversizedFrame = errors.New("zerotier: Ethernet payload exceeds negotiated MTU")
	ErrTruncatedFrame = errors.New("zerotier: truncated Ethernet frame")
)

// EthernetAdapter translates between complete Ethernet frames and the native
// ZeroTier core's separated L2 fields. No TUN or system NIC is required.
// Each adapter is scoped to one network and updated atomically on netconf.
type EthernetAdapter struct {
	network   NetworkID
	mtu       atomic.Int32
	oversize  atomic.Uint64
	truncated atomic.Uint64
	mu        sync.Mutex
}

func NewEthernetAdapter(id NetworkID, mtu int) (*EthernetAdapter, error) {
	if id == 0 {
		return nil, errors.New("zerotier: Ethernet adapter needs a network ID")
	}
	a := &EthernetAdapter{network: id}
	if err := a.SetMTU(mtu); err != nil {
		return nil, err
	}
	return a, nil
}
func (a *EthernetAdapter) SetMTU(mtu int) error {
	if mtu == 0 {
		mtu = DefaultMTU
	}
	if mtu < 576 || mtu > 65535 {
		return fmt.Errorf("zerotier: invalid MTU %d", mtu)
	}
	a.mtu.Store(int32(mtu))
	return nil
}
func (a *EthernetAdapter) UpdateNetworkConfig(config NetworkConfig) error {
	if config.ID != a.network {
		return fmt.Errorf("zerotier: config belongs to network %s, adapter is %s", config.ID, a.network)
	}
	return a.SetMTU(config.MTU)
}
func (a *EthernetAdapter) MTU() int               { return int(a.mtu.Load()) }
func (a *EthernetAdapter) MaxFrameSize() int      { return a.MTU() + EthernetHeaderSize }
func (a *EthernetAdapter) OversizeDrops() uint64  { return a.oversize.Load() }
func (a *EthernetAdapter) TruncatedDrops() uint64 { return a.truncated.Load() }

// Encode returns the complete wire Ethernet frame. Source and destination MAC
// are encoded as 48-bit addresses. Data is the L3 payload without L2 header.
func (a *EthernetAdapter) Encode(frame Frame) ([]byte, error) {
	if frame.NetworkID != a.network {
		return nil, errors.New("zerotier: wrong Ethernet network")
	}
	if err := frame.validate(); err != nil {
		return nil, err
	}
	mtu := a.MTU()
	if len(frame.Data) > mtu {
		a.oversize.Add(1)
		return nil, ErrOversizedFrame
	}
	header := EthernetHeaderSize
	if frame.VLAN != 0 {
		header += 4
	}
	result := make([]byte, header+len(frame.Data))
	writeMAC(result[:6], frame.DestinationMAC)
	writeMAC(result[6:12], frame.SourceMAC)
	if frame.VLAN != 0 {
		binary.BigEndian.PutUint16(result[12:14], 0x8100)
		binary.BigEndian.PutUint16(result[14:16], frame.VLAN&0x0fff)
		binary.BigEndian.PutUint16(result[16:18], frame.EtherType)
	} else {
		binary.BigEndian.PutUint16(result[12:14], frame.EtherType)
	}
	copy(result[header:], frame.Data)
	return result, nil
}

// Decode parses a complete L2 Ethernet frame. Oversized and truncated packets
// are dropped, never silently shortened; drop counters are observable.
func (a *EthernetAdapter) Decode(ethernet []byte) (Frame, error) {
	if len(ethernet) < EthernetHeaderSize {
		a.truncated.Add(1)
		return Frame{}, ErrTruncatedFrame
	}
	header := EthernetHeaderSize
	etherType := binary.BigEndian.Uint16(ethernet[12:14])
	var vlan uint16
	if etherType == 0x8100 || etherType == 0x88a8 {
		if len(ethernet) < 18 {
			a.truncated.Add(1)
			return Frame{}, ErrTruncatedFrame
		}
		header = 18
		vlan = binary.BigEndian.Uint16(ethernet[14:16]) & 0x0fff
		etherType = binary.BigEndian.Uint16(ethernet[16:18])
	}
	if len(ethernet)-header > a.MTU() {
		a.oversize.Add(1)
		return Frame{}, ErrOversizedFrame
	}
	return Frame{NetworkID: a.network, DestinationMAC: readMAC(ethernet[:6]), SourceMAC: readMAC(ethernet[6:12]), EtherType: etherType, VLAN: vlan, Data: append([]byte(nil), ethernet[header:]...)}, nil
}
func writeMAC(out []byte, mac uint64) {
	for i := 5; i >= 0; i-- {
		out[i] = byte(mac)
		mac >>= 8
	}
}
func readMAC(input []byte) uint64 {
	var v uint64
	for _, b := range input[:6] {
		v = (v << 8) | uint64(b)
	}
	return v
}
