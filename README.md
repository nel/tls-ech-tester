# ech-peer

`ech-peer` is a small test-only Go standard-library TLS server and client used
by Hoplum's encrypted ClientHello integration tests. It is not a production
dependency, DNS resolver, browser automation tool, or replacement TLS
implementation.

The peer accepts loopback TCP connections on a random port and writes only its
public certificate, ECH configuration, readiness address, and accepted
connection count to the caller-owned directory. Its client mode uses stock Go
TLS certificate and ECH rejection verification. See `main.go` for the fixed
command protocol.

## Release builds

Release `ech-peer-v1` was imported byte-for-byte from the private
`nel/hoplum-agent` release of the same name. Its source was originally committed
as `agent/tests/ech-peer/main.go` at Hoplum commit
`5d6cb480ae78755682f73cdf81f00fb091a190e4`. The imported `main.go` SHA-256 is
`6938d73dd946b835a0d719aeb9a5cfb83bb5bd5c01293c076cbd22d00d9be1d5`.

The six v1 binaries were built locally with Go 1.27.1, `CGO_ENABLED=0`,
`-trimpath`, `-buildvcs=false`, and `-ldflags=-s -w -buildid=`. macOS ARM64 and
Windows ARM64 were runtime-qualified in Hoplum; the other four targets were
cross-compiled only. `SHA256SUMS` pins both the source and every released asset.

To reproduce a future release with the pinned toolchain:

```sh
bash build-release.sh /path/to/go output-directory
```

Choose a new immutable release tag for any source or toolchain change. Never
replace assets already consumed by another repository.
