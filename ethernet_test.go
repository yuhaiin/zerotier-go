package zerotier

import (
	"bytes"
	"errors"
	"testing"
)

func TestEthernetLargeFrameAndControllerMTU(t *testing.T) {
	a, err := NewEthernetAdapter(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if a.MaxFrameSize() != DefaultMTU+14 {
		t.Fatalf("default frame buffer = %d", a.MaxFrameSize())
	}
	data := bytes.Repeat([]byte{0x47}, 2048) // > 1500-byte Ethernet payload
	f := Frame{NetworkID: 1, SourceMAC: 0x123456789abc, DestinationMAC: 0x9876543210ab, EtherType: 0x86dd, Data: data}
	encoded, err := a.Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != 2048+14 {
		t.Fatalf("length = %d", len(encoded))
	}
	back, err := a.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if back.SourceMAC != f.SourceMAC || back.DestinationMAC != f.DestinationMAC || back.EtherType != f.EtherType || !bytes.Equal(back.Data, data) {
		t.Fatal("round-trip changed Ethernet frame")
	}
	if err = a.UpdateNetworkConfig(NetworkConfig{ID: 1, MTU: 1700}); err != nil {
		t.Fatal(err)
	}
	if a.MaxFrameSize() != 1714 {
		t.Fatal("controller MTU not applied")
	}
	if _, err = a.Encode(f); !errors.Is(err, ErrOversizedFrame) {
		t.Fatalf("oversized frame returned %v", err)
	}
	if _, err = a.Decode(encoded); !errors.Is(err, ErrOversizedFrame) {
		t.Fatalf("oversized receive returned %v", err)
	}
	if a.OversizeDrops() != 2 {
		t.Fatalf("drop count = %d", a.OversizeDrops())
	}
	if _, err = a.Decode([]byte{0, 1, 2}); !errors.Is(err, ErrTruncatedFrame) {
		t.Fatalf("truncated: %v", err)
	}
	if a.TruncatedDrops() != 1 {
		t.Fatal("missing truncated counter")
	}
	if err = a.UpdateNetworkConfig(NetworkConfig{ID: 2, MTU: 2800}); err == nil {
		t.Fatal("wrong network accepted")
	}
}
func TestEthernetVLANRoundTrip(t *testing.T) {
	a, _ := NewEthernetAdapter(8, 0)
	f := Frame{NetworkID: 8, SourceMAC: 0xff00abcdefff, DestinationMAC: 0x010203040506, EtherType: 0x0800, VLAN: 101, Data: []byte{1, 2, 3}}
	encoded, err := a.Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != 21 {
		t.Fatalf("len %d", len(encoded))
	}
	got, err := a.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got.VLAN != f.VLAN || !bytes.Equal(got.Data, f.Data) {
		t.Fatalf("bad VLAN: %+v", got)
	}
}
func BenchmarkEthernetEncode2048(b *testing.B) {
	a, _ := NewEthernetAdapter(1, 0)
	f := Frame{NetworkID: 1, SourceMAC: 1, DestinationMAC: 2, EtherType: 0x800, Data: make([]byte, 2048)}
	b.SetBytes(int64(len(f.Data)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = a.Encode(f)
	}
}
func BenchmarkEthernetDecode2048(b *testing.B) {
	a, _ := NewEthernetAdapter(1, 0)
	f := Frame{NetworkID: 1, SourceMAC: 1, DestinationMAC: 2, EtherType: 0x800, Data: make([]byte, 2048)}
	frame, _ := a.Encode(f)
	b.SetBytes(int64(len(frame)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = a.Decode(frame)
	}
}
