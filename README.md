# zerotier-go

Embedded **official ZeroTierOne Core** using [purego](https://github.com/ebitengine/purego) with a small **native C ABI bridge**. Unlike the standalone daemon or `libzt`, the package leaves virtual network interfaces, L2/L3 adaptation, IP stack, routing, and DNS to its host application (e.g. **yuhaiin**).

> **Status:** Initial integration. The Go APIs, state store, ABI and callback tests are covered; the native build / official protocol interoperability must pass in CI and on a real ZeroTier network before claiming production readiness. The checkout initially includes only source. The native workflow compiles the pinned official core into shared libraries and commits verified platform artifacts to `native/assets/`.

## Architecture

```
App (yuhaiin) TCP/UDP socket API
  -> app's L3 TCP/IP stack
  -> app's Ethernet/ARP/NDP adapter
  -> Node.ProcessFrame/OnFrame (L2 frames)
  -> native ztgo bridge (purego)
  -> official ZeroTierOne core (P2P, crypto, network controller)
  -> Node.ProcessWirePacket/SendWire (encrypted physical UDP)
  -> app-defined outbound underlay
```

This is **not** a turnkey TCP/IP stack: the library purposely exports **raw Ethernet frames** to reuse a caller's stack. ZeroTier network authorization, assigned addresses, MTU, and managed routes arrive via `OnNetworkConfig` and `Node.Network`. You must configure your IP and L2 link accordingly; the library NEVER creates TUN devices or OS routes.

## Install

```sh
go get github.com/yuhaiin/zerotier-go
```

`New` embeds **only the current GOOS/GOARCH's native artifact**. For source builds the
corresponding binary must already exist in `native/assets/`, otherwise the Go
compiler intentionally fails. The supported release targets are Linux
AMD64/ARM64, macOS AMD64/ARM64, Windows AMD64, and Android ARM64. Unsupported
platforms return `ErrUnsupportedPlatform`. There are no placeholder ELF files.

Native loading obeys platform security restrictions:

- **macOS:** no temp extraction. Install the signed/notarized dylib beside the
  executable or in the app's `Frameworks` / `Contents/Frameworks` folder.
  Paths and each ancestor must not be symlinks and must have a trusted owner.
- **Linux:** prefer a sealed, anonymous `memfd_create` buffer via
  `/proc/self/fd/<fd>` (the fd link is an intentional kernel-managed
  exception). If unavailable, create an exclusive `O_NOFOLLOW` file in a
  private `~/.cache/yuhaiin/zerotier/native` directory, never in `/tmp`.
- **Windows:** use `%LOCALAPPDATA%\yuhaiin\zerotier\<bridge version>`, an
  unpredictable unique filename, exclusive creation and a read-only pinning
  handle with no write sharing. Reject reparse points and untrusted owners.
- **Android:** package `native/assets/libztgo_android_arm64.so` as
  `jniLibs/arm64-v8a/libztgo.so` before signing. Never extract a .so to the
  app's writable cache. gomobile may enable CGO; the shared API stays purego.

The project currently ships **Android arm64-v8a only**. Android's NDK linker
is passed `-Wl,-z,max-page-size=16384` and the CI checks every ELF `LOAD`
segment is 16 KiB aligned using `llvm-readelf`. Do not publish other
`abiFilters` until their binaries have been built and verified.

## Basic raw protocol example

```go
store, err := zerotier.NewFileStore("/var/lib/myapp/zerotier")
if err != nil { panic(err) }
node, err := zerotier.New(zerotier.Config{
    Store: store,
    SendWire: func(w zerotier.WirePacket) error {
        // Write encrypted UDP payload to w.To via your EXISTING underlay.
        return physicalTransport.Send(w.To, w.Data)
    },
    OnFrame: func(f zerotier.Frame) {
        // Deliver f into your virtual Ethernet/L3 bridge.
        ethernetBridge.Deliver(f)
    },
    OnNetworkConfig: func(c zerotier.NetworkConfig) {
        // Inspect Status, Addresses, Routes, MAC and MTU.
        // Do NOT synchronously reenter Node from this handler.
        applyNetworkConfiguration(c)
    },
})
if err != nil { panic(err) }
defer node.Close()
networkID, err := zerotier.ParseNetworkID("8056c2e21c000001")
if err != nil { panic(err) }
if err := node.Join(networkID); err != nil { panic(err) }

// On a physical UDP receive loop, supply the sender's actual IP:port:
// _ = node.ProcessWirePacket(from, encryptedUDPDatagram)
// On an L2 transmit loop:
// _ = node.ProcessFrame(outgoingFrame)
```

For standalone transport, `ListenUDP(cfg, "udp", ":0")` opens a host UDP socket, binds the sender, starts a receive loop and returns a `UDPNode` with `Node`, `Socket`, and `Close`.

Use `Orbit(moonID, seed)`/`Deorbit(moonID)` for private roots. Use `Subscribe`/`Unsubscribe` for multicast group management (the application L2 adapter manages ARP/NDP and membership). Configure private planets via official state objects, not by connecting to an arbitrary untrusted root.

### Identity and lifecycle

- `NewFileStore(dir)` persists **all** official state objects atomically with `0600` permissions, including the node secret identity. Do not run two nodes concurrently with the same directory.
- `NewMemoryStore()` is explicitly ephemeral, intended only for tests.
- Callback payloads are copied to Go-owned memory. A bounded queue prevents blocking native callbacks; an overflowing queue fails a network-config callback and increments `Node.DroppedCallbacks()` (other event/frame callbacks are dropped).
- `Node.Close()` stops native entry points and background operations, then frees the native node and library. Do not call `Close` synchronously from a user handler; ensure sender callbacks cannot hang forever.
- A physical underlay must not use this node to reach its own roots/peers. Recursive dial chains can deadlock or loop indefinitely.

## Testing and benchmarks

```sh
go test -race ./...
go test -run '^$' -bench . -benchmem
```

There are source-only tests for Network ID, IP+ABI structs, state copies and persistence, controller configuration, frame validation, cancellation, and error handling. `TestNativeSmokeWhenAvailable` additionally exercises actual native creation, Join/Leave, Close, and identity persistence when `ZEROTIER_GO_TEST_LIBRARY` points to a built bridge; `BenchmarkNativeFrame` measures native packet injection. The required `real-link` CI job starts an official ZeroTierOne controller in
Docker, creates a private network, authorizes TWO instances of this Go library,
checks an actual >1500-byte IPv4/ICMP Echo round trip, fully closes one node,
restarts it with the same identity, joins again, and repeats the ping. The
job cannot pass merely from Join/API status or a simulated UDP packet. It emits
per-phase timings and uploads logs. Public/private NAT, IPv6 and Moon
interoperability need separate test coverage.

A previous local source-only benchmark run using an explicitly substituted purego stub for offline compilation (Linux AMD64; 100ms calibration) measured:

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| MemoryStore Put+Get (256 B) | 168 | 512 | 2 |
| IPv6 address conversion | 38 | 0 | 0 |
| Network config snapshot (2 addresses, 4 routes) | 342 | 544 | 5 |

**These are Go-only microbenchmarks, not ZeroTier throughput or native FFI performance.** Run them on your target platform, and benchmark native packet throughput separately.

## Native build

The native workflow pins the upstream ZeroTierOne Git revision, explicitly disables `ZT_NONFREE`, compiles the official `zerotier-core` target and links `native/bridge/ztgo.cpp` as a shared library. Only the official **MPL-2.0 Agent Core** should be distributed; **never include the nonfree controller code**. Preserve the official MPL notices and comply with corresponding-source obligations. Review runtime dependencies of generated libraries (C++ ABI, libc/NDK, code-signing).

Upstream API: [ZeroTierOne.h](https://github.com/zerotier/ZeroTierOne/blob/dev/include/ZeroTierOne.h).

`EthernetAdapter` provides `Encode`/`Decode`, MTU-driven capacity (`MTU+14`
for normal L2 frames, `MTU+18` for 802.1Q), and oversized/truncated packet
drop counters. The default MTU is 2800, updated from `NetworkConfig.MTU`.
The core `Node` also rejects payloads exceeding the negotiated MTU and reports
`DroppedFrames()`. TCP/IP, ARP/NDP, routing and DNS remain host-owned.

`native/bridge/ztgo.h` defines the fixed-width ABI. Go's `native.go` mirrors it, with layout tests. Increment the ABI version on breaking changes. If the shared library is absent or built for another ABI version, initialization returns a descriptive error rather than attempting to call incompatible symbols.
