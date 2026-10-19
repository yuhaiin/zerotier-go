//go:build integration

// The real-link test is intentionally separate from hermetic tests. CI starts
// an OFFICIAL zerotier-one controller in Docker and runs two of THIS library's
// official-core nodes, transferring ICMP Echo packets over native L2 frames.
package integration

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	zt "github.com/yuhaiin/zerotier-go"
)

func ipFromConfig(t *testing.T, c zt.NetworkConfig) netip.Addr {
	t.Helper()
	for _, p := range c.Addresses {
		if p.Addr().Is4() {
			return p.Addr()
		}
	}
	t.Fatalf("controller did not assign IPv4: %+v", c)
	return netip.Addr{}
}
func authorize(t *testing.T, client *http.Client, base, token string, id zt.NetworkID, addr uint64, ip string) {
	t.Helper()
	body := fmt.Sprintf(`{"authorized":true,"ipAssignments":[%q]}`, ip)
	url := fmt.Sprintf("%s/controller/network/%s/member/%010x", base, id, addr)
	request, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-ZT1-AUTH", token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("member authorization %s: %s", url, response.Status)
	}
	var payload map[string]any
	if err = json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if a, ok := payload["authorized"].(bool); ok && !a {
		t.Fatalf("controller rejected member %x", addr)
	}
}
func checksum(b []byte) uint16 {
	var n uint32
	for i := 0; i+1 < len(b); i += 2 {
		n += uint32(binary.BigEndian.Uint16(b[i : i+2]))
	}
	if len(b)%2 == 1 {
		n += uint32(b[len(b)-1]) << 8
	}
	for n>>16 != 0 {
		n = (n & 0xffff) + (n >> 16)
	}
	return ^uint16(n)
}
func makePing(src, dst netip.Addr, sequence uint16, payloadLen int) []byte {
	b := make([]byte, 20+8+payloadLen)
	b[0] = 0x45
	b[8] = 64
	b[9] = 1
	binary.BigEndian.PutUint16(b[2:4], uint16(len(b)))
	binary.BigEndian.PutUint16(b[4:6], sequence)
	s := src.As4()
	d := dst.As4()
	copy(b[12:16], s[:])
	copy(b[16:20], d[:])
	binary.BigEndian.PutUint16(b[10:12], checksum(b[:20]))
	b[20] = 8
	binary.BigEndian.PutUint16(b[24:26], 0x7ac9)
	binary.BigEndian.PutUint16(b[26:28], sequence)
	for i := 28; i < len(b); i++ {
		b[i] = byte(i * 37)
	}
	binary.BigEndian.PutUint16(b[22:24], checksum(b[20:]))
	return b
}
func replyPing(in []byte) ([]byte, bool) {
	if len(in) < 28 || in[0]>>4 != 4 || in[9] != 1 || in[20] != 8 {
		return nil, false
	}
	h := int(in[0]&0x0f) * 4
	if h != 20 || int(binary.BigEndian.Uint16(in[2:4])) != len(in) {
		return nil, false
	}
	out := append([]byte(nil), in...)
	copy(out[12:16], in[16:20])
	copy(out[16:20], in[12:16])
	out[8] = 64
	out[10], out[11] = 0, 0
	binary.BigEndian.PutUint16(out[10:12], checksum(out[:20]))
	out[20] = 0
	out[22], out[23] = 0, 0
	binary.BigEndian.PutUint16(out[22:24], checksum(out[20:]))
	return out, true
}
func TestOfficialControllerTwoGoNodesPingReconnect(t *testing.T) {
	nwidText := os.Getenv("ZTGO_CI_NETWORK")
	if nwidText == "" {
		t.Skip("run through integration/run-controller.sh; CI always supplies real local controller")
	}
	token := os.Getenv("ZTGO_CI_TOKEN")
	if token == "" {
		t.Fatal("CI controller API token is required")
	}
	base := os.Getenv("ZTGO_CI_CONTROLLER")
	if base == "" {
		base = "http://127.0.0.1:9993"
	}
	id, err := zt.ParseNetworkID(nwidText)
	if err != nil {
		t.Fatal(err)
	}
	lib := os.Getenv("ZEROTIER_GO_TEST_LIBRARY")
	if lib == "" {
		t.Fatal("native library path required in true-link CI")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	first := time.Now()
	storeA, storeB := zt.NewMemoryStore(), zt.NewMemoryStore()
	received := make(chan zt.Frame, 128)
	a, err := zt.ListenUDP(zt.Config{LibraryPath: lib, Store: storeA, OnFrame: func(f zt.Frame) {
		select {
		case received <- f:
		default:
		}
	}}, "udp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var b *zt.UDPNode
	var bRef atomic.Pointer[zt.UDPNode]
	responder := func(f zt.Frame) {
		if f.NetworkID != id || f.EtherType != 0x0800 {
			return
		}
		data, ok := replyPing(f.Data)
		if !ok {
			return
		}
		if active := bRef.Load(); active != nil {
			_ = active.Node.ProcessFrame(zt.Frame{NetworkID: id, SourceMAC: f.DestinationMAC, DestinationMAC: f.SourceMAC, EtherType: 0x0800, Data: data})
		}
	}
	startB := func() {
		t.Helper()
		var e error
		b, e = zt.ListenUDP(zt.Config{LibraryPath: lib, Store: storeB, OnFrame: responder}, "udp4", "0.0.0.0:0")
		if e != nil {
			t.Fatal(e)
		}
		bRef.Store(b)
	}
	startB()
	defer func() {
		if b != nil {
			_ = b.Close()
		}
	}()
	addrA, err := a.Node.Address()
	if err != nil {
		t.Fatal(err)
	}
	addrB, err := b.Node.Address()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("real protocol engines: A=%010x B=%010x; controller=%s", addrA, addrB, nwidText)
	if err = a.Node.Join(id); err != nil {
		t.Fatal(err)
	}
	if err = b.Node.Join(id); err != nil {
		t.Fatal(err)
	}
	authorize(t, client, base, token, id, addrA, "10.147.20.2")
	authorize(t, client, base, token, id, addrB, "10.147.20.3")
	configA, err := a.Node.WaitForNetwork(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	configB, err := b.Node.WaitForNetwork(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("first Join + controller configuration %s (A=%v B=%v)", time.Since(first), configA.Addresses, configB.Addresses)
	if ipFromConfig(t, configA) != netip.MustParseAddr("10.147.20.2") || ipFromConfig(t, configB) != netip.MustParseAddr("10.147.20.3") {
		t.Fatal("controller assigned unexpected addresses")
	}
	// Ensure actual L2 transfer, not just the controller's status/Join API.
	exchange := func(seq uint16, size int) {
		t.Helper()
		payload := makePing(ipFromConfig(t, configA), ipFromConfig(t, configB), seq, size)
		if len(payload) <= 1500 {
			t.Fatal("integration ICMP payload must exercise MTU > 1500")
		}
		started := time.Now()
		ticker := time.NewTicker(650 * time.Millisecond)
		defer ticker.Stop()
		timeout := time.NewTimer(45 * time.Second)
		defer timeout.Stop()
		for {
			err := a.Node.ProcessFrame(zt.Frame{NetworkID: id, SourceMAC: configA.MAC, DestinationMAC: configB.MAC, EtherType: 0x0800, Data: payload})
			if err != nil {
				t.Fatalf("native send: %v", err)
			}
			select {
			case f := <-received:
				if f.NetworkID == id && f.EtherType == 0x800 && len(f.Data) == len(payload) && f.Data[20] == 0 && binary.BigEndian.Uint16(f.Data[26:28]) == seq && bytes.Equal(f.Data[28:], payload[28:]) {
					t.Logf("real L2 IPv4 ICMP Echo round-trip: %d bytes, %s", len(payload), time.Since(started))
					return
				}
			case <-ticker.C:
			case <-timeout.C:
				t.Fatalf("ICMP echo did not return after 45s (seq %d); A frame drops=%d B frame drops=%d", seq, a.Node.DroppedFrames(), b.Node.DroppedFrames())
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}
	exchange(1, 1800)
	// Disconnect one node entirely, including the socket and protocol engine;
	// persistence MUST give the node the same identity and IP on reconnect.
	bRef.Store(nil)
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	b = nil
	reconnectStart := time.Now()
	startB()
	secondAddress, err := b.Node.Address()
	if err != nil {
		t.Fatal(err)
	}
	if secondAddress != addrB {
		t.Fatalf("identity lost on reconnect %010x != %010x", secondAddress, addrB)
	}
	if err = b.Node.Join(id); err != nil {
		t.Fatal(err)
	}
	configB, err = b.Node.WaitForNetwork(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("full disconnect/rejoin completed in %s", time.Since(reconnectStart))
	exchange(2, 1800)
	t.Logf("real-link test completed in %s", time.Since(first))
}
