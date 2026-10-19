package zerotier

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

// UDPNode is an optional standalone physical UDP adapter. Applications with
// custom outbound routing, including yuhaiin, should instead provide SendWire
// and call ProcessWirePacket with their own sockets.
type UDPNode struct {
	Node   *Node
	Socket net.PacketConn
	once   sync.Once
	wg     sync.WaitGroup
}

func ListenUDP(cfg Config, network, address string) (*UDPNode, error) {
	pc, err := net.ListenPacket(network, address)
	if err != nil {
		return nil, err
	}
	// The adapter owns this transport; do not silently override custom underlays.
	if cfg.SendWire != nil {
		_ = pc.Close()
		return nil, errors.New("zerotier: ListenUDP requires nil SendWire")
	}
	cfg.SendWire = func(w WirePacket) error {
		_, err := pc.WriteTo(w.Data, net.UDPAddrFromAddrPort(w.To))
		return err
	}
	n, err := New(cfg)
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	u := &UDPNode{Node: n, Socket: pc}
	u.wg.Add(1)
	go u.readLoop()
	return u, nil
}
func (u *UDPNode) readLoop() {
	defer u.wg.Done()
	buf := make([]byte, 65535)
	for {
		size, remote, err := u.Socket.ReadFrom(buf)
		if err != nil {
			return
		}
		a, ok := remote.(*net.UDPAddr)
		if !ok {
			continue
		}
		if size > 0 {
			_ = u.Node.ProcessWirePacket(a.AddrPort(), buf[:size])
		}
	}
}
func (u *UDPNode) Close() error {
	var err error
	u.once.Do(func() {
		_ = u.Socket.Close()
		u.wg.Wait()
		err = u.Node.Close()
	})
	return err
}

// WaitForNetwork blocks until the controller reports an authorized network
// and its managed addresses. It does not imply peer-to-peer reachability.
func (n *Node) WaitForNetwork(ctx context.Context, id NetworkID) (NetworkConfig, error) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		cfg, ok := n.Network(id)
		if ok {
			switch cfg.Status {
			case NetworkOK:
				if (cfg.Operation == NetworkUp || cfg.Operation == NetworkUpdated) && len(cfg.Addresses) > 0 {
					return cfg, nil
				}
			case NetworkAccessDenied, NetworkNotFound, NetworkPortError, NetworkAuthenticationRequired:
				return cfg, errors.New("zerotier: network controller rejected or requires authorization")
			}
		}
		select {
		case <-ctx.Done():
			return NetworkConfig{}, ctx.Err()
		case <-n.done:
			return NetworkConfig{}, ErrClosed
		case <-ticker.C:
		}
	}
}