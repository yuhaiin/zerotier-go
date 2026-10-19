package zerotier

import (
	"errors"
	"net/netip"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type task struct {
	wire   *WirePacket
	frame  *Frame
	config *NetworkConfig
	event  *Event
}

// Node is one official ZeroTier protocol engine. It does not own OS routing
// or a virtual TUN device. All C entry points are serialized by gate.
type Node struct {
	cfg        Config
	api        *nativeAPI
	handle     uintptr
	token      uintptr
	gate       sync.Mutex
	closed     bool
	closeOnce  sync.Once
	done       chan struct{}
	tasks      chan task
	wake       chan struct{}
	wg         sync.WaitGroup
	drops      atomic.Uint64
	netMu      sync.RWMutex
	networks   map[NetworkID]NetworkConfig
	stateErrMu sync.Mutex
	stateErr   error
	deadline   atomic.Int64
}

// New loads an embedded or explicit platform library, creates a native node,
// and starts its background protocol maintenance and callback dispatcher.
func New(cfg Config) (*Node, error) {
	if cfg.SendWire == nil {
		return nil, errors.New("zerotier: SendWire required")
	}
	if cfg.QueueSize == 0 {
		cfg.QueueSize = 256
	}
	if cfg.QueueSize < 1 || cfg.QueueSize > 65536 {
		return nil, errors.New("zerotier: invalid QueueSize")
	}
	if cfg.Store == nil {
		cfg.Store = NewMemoryStore()
	}
	api, err := loadAPI(cfg.LibraryPath)
	if err != nil {
		return nil, err
	}
	n := &Node{cfg: cfg, api: api, done: make(chan struct{}), tasks: make(chan task, cfg.QueueSize), wake: make(chan struct{}, 1), networks: make(map[NetworkID]NetworkConfig)}
	registerCallbacks()
	n.token = uintptr(nextToken.Add(1))
	activeNodes.Store(n.token, n)
	var flags uint32
	if cfg.LowBandwidth {
		flags |= 1
	}
	if cfg.EncryptedHello {
		flags |= 2
	}
	code := api.newNode(&n.handle, n.token, &callbacks, flags, nowMillis())
	runtime.KeepAlive(&callbacks)
	if code != 0 || n.handle == 0 || n.getStoreError() != nil {
		if n.handle != 0 {
			api.deleteNode(n.handle)
		}
		activeNodes.Delete(n.token)
		_ = api.closeHandle()
		return nil, errors.Join(nativeCallError("new", code), n.getStoreError())
	}
	n.wg.Add(2)
	go n.dispatch()
	go n.maintain()
	return n, nil
}
func (n *Node) setStoreError(err error) {
	n.stateErrMu.Lock()
	n.stateErr = errors.Join(n.stateErr, err)
	n.stateErrMu.Unlock()
}
func (n *Node) getStoreError() error {
	n.stateErrMu.Lock()
	defer n.stateErrMu.Unlock()
	return n.stateErr
}
func (n *Node) offer(t task) bool {
	select {
	case <-n.done:
		return false
	default:
	}
	select {
	case n.tasks <- t:
		return true
	default:
		n.drops.Add(1)
		return false
	}
}
func (n *Node) dispatch() {
	defer n.wg.Done()
	for {
		select {
		case <-n.done:
			return
		case t := <-n.tasks:
			// User callbacks intentionally run outside native library locks.
			if t.wire != nil {
				_ = n.cfg.SendWire(*t.wire)
			}
			if t.frame != nil && n.cfg.OnFrame != nil {
				n.cfg.OnFrame(*t.frame)
			}
			if t.config != nil && n.cfg.OnNetworkConfig != nil {
				n.cfg.OnNetworkConfig(*t.config)
			}
			if t.event != nil && n.cfg.OnEvent != nil {
				n.cfg.OnEvent(*t.event)
			}
		}
	}
}
func (n *Node) withHandle(op string, f func(uintptr) (int32, int64)) error {
	n.gate.Lock()
	defer n.gate.Unlock()
	if n.closed {
		return ErrClosed
	}
	rc, deadline := f(n.handle)
	if deadline > 0 {
		n.deadline.Store(deadline)
		select {
		case n.wake <- struct{}{}:
		default:
		}
	}
	return nativeCallError(op, rc)
}
func (n *Node) maintain() {
	defer n.wg.Done()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-n.done:
			return
		case <-n.wake:
		case <-timer.C:
		}
		// Background deadlines are wall-clock milliseconds supplied by ZeroTier.
		deadline := n.deadline.Load()
		if deadline <= nowMillis() {
			err := n.withHandle("background", func(h uintptr) (int32, int64) { var due int64; return n.api.tick(h, nowMillis(), &due), due })
			if err != nil {
				if errors.Is(err, ErrClosed) {
					return
				}
				deadline = nowMillis() + 1000
			} else {
				deadline = n.deadline.Load()
			}
		}
		delay := time.Until(time.UnixMilli(deadline))
		if delay < 10*time.Millisecond {
			delay = 10 * time.Millisecond
		}
		if delay > time.Minute {
			delay = time.Minute
		}
		timer.Reset(delay)
	}
}
func (n *Node) Address() (uint64, error) {
	n.gate.Lock()
	defer n.gate.Unlock()
	if n.closed {
		return 0, ErrClosed
	}
	return n.api.address(n.handle), nil
}
func (n *Node) Join(id NetworkID) error {
	if id == 0 {
		return errors.New("zerotier: empty network ID")
	}
	return n.withHandle("join", func(h uintptr) (int32, int64) { return n.api.join(h, uint64(id)), 0 })
}
func (n *Node) Leave(id NetworkID) error {
	if id == 0 {
		return errors.New("zerotier: empty network ID")
	}
	return n.withHandle("leave", func(h uintptr) (int32, int64) { return n.api.leave(h, uint64(id)), 0 })
}

// Orbit joins a moon (private root). Seed is the designated root node address.
func (n *Node) Orbit(moonID, seed uint64) error {
	if moonID == 0 || seed == 0 {
		return errors.New("zerotier: moon ID and seed required")
	}
	return n.withHandle("orbit", func(h uintptr) (int32, int64) { return n.api.orbit(h, moonID, seed), 0 })
}
func (n *Node) Deorbit(moonID uint64) error {
	if moonID == 0 {
		return errors.New("zerotier: moon ID required")
	}
	return n.withHandle("deorbit", func(h uintptr) (int32, int64) { return n.api.deorbit(h, moonID), 0 })
}

// Subscribe and Unsubscribe are used by L2 adapters to manage virtual multicast.
func (n *Node) Subscribe(id NetworkID, mac uint64, adi uint32) error {
	if id == 0 || mac>>48 != 0 {
		return errors.New("zerotier: invalid multicast group")
	}
	return n.withHandle("subscribe", func(h uintptr) (int32, int64) { return n.api.subscribe(h, uint64(id), mac, adi), 0 })
}
func (n *Node) Unsubscribe(id NetworkID, mac uint64, adi uint32) error {
	if id == 0 || mac>>48 != 0 {
		return errors.New("zerotier: invalid multicast group")
	}
	return n.withHandle("unsubscribe", func(h uintptr) (int32, int64) { return n.api.unsubscribe(h, uint64(id), mac, adi), 0 })
}

// ProcessWirePacket injects a received physical ZeroTier UDP datagram.
func (n *Node) ProcessWirePacket(from netip.AddrPort, payload []byte) error {
	if len(payload) == 0 || len(payload) > 1<<20 {
		return errors.New("zerotier: invalid wire packet size")
	}
	a, err := fromIP(from)
	if err != nil {
		return err
	}
	return n.withHandle("wire", func(h uintptr) (int32, int64) {
		var due int64
		rc := n.api.processWire(h, nowMillis(), &a, nativePacketPointer(payload), uint32(len(payload)), &due)
		keepNativeAlive(payload)
		return rc, due
	})
}

// ProcessFrame transmits an L2 frame through an already joined network.
// ARP/NDP and route selection are responsibilities of the embedding L2 adapter.
func (n *Node) ProcessFrame(f Frame) error {
	if err := f.validate(); err != nil {
		return err
	}
	c := cFrame{NetworkID: uint64(f.NetworkID), SourceMAC: f.SourceMAC, DestinationMAC: f.DestinationMAC, EtherType: uint32(f.EtherType), VLAN: uint32(f.VLAN), Data: nativePacketPointer(f.Data), Length: uint32(len(f.Data))}
	return n.withHandle("frame", func(h uintptr) (int32, int64) {
		var due int64
		rc := n.api.processFrame(h, nowMillis(), &c, &due)
		keepNativeAlive(f.Data)
		return rc, due
	})
}

// Network returns the latest received controller configuration snapshot.
func (n *Node) Network(id NetworkID) (NetworkConfig, bool) {
	n.netMu.RLock()
	defer n.netMu.RUnlock()
	c, ok := n.networks[id]
	if !ok {
		return c, false
	}
	c.Addresses = append([]netip.Prefix(nil), c.Addresses...)
	c.Routes = append([]Route(nil), c.Routes...)
	return c, true
}
func (n *Node) DroppedCallbacks() uint64 { return n.drops.Load() }
func (n *Node) Close() error {
	var err error
	n.closeOnce.Do(func() {
		n.gate.Lock()
		n.closed = true
		close(n.done)
		// No concurrent native calls remain after the gate is acquired.
		n.api.deleteNode(n.handle)
		n.gate.Unlock()
		n.wg.Wait()
		activeNodes.Delete(n.token)
		err = errors.Join(n.getStoreError(), n.api.closeHandle())
	})
	return err
}