# tls-ech-tester

A command-line client and server for testing TLS Encrypted ClientHello (ECH).
It provides a local ECH-capable endpoint and reports whether a connection accepted
ECH, rejected it with an authenticated response, or failed certificate trust
verification.

It uses only Go's standard library. The server generates fresh certificate and
ECH keys on each launch, keeps private keys in memory, and listens on loopback.
The fixed names and bounded protocol make it useful for automated TLS, proxy,
and network-filter tests.

## Download

Download the executable for your platform from [Releases](https://github.com/nel/tls-ech-tester/releases):

| Platform | AMD64 | ARM64 |
| --- | --- | --- |
| macOS | `tls-ech-tester-darwin-amd64` | `tls-ech-tester-darwin-arm64` |
| Linux | `tls-ech-tester-linux-amd64` | `tls-ech-tester-linux-arm64` |
| Windows | `tls-ech-tester-windows-amd64.exe` | `tls-ech-tester-windows-arm64.exe` |

Verify the download against the release's `SHA256SUMS`. On macOS and Linux,
make the file executable with `chmod +x`. The examples below assume the binary
is named `tls-ech-tester` and is on your `PATH`.

## Server

Create an empty directory and start the server:

```sh
mkdir test-state
tls-ech-tester test-state
```

The server writes these files into the directory:

| File | Contents |
| --- | --- |
| `cert.der` | Self-signed DER certificate for `example.test` and `override.example.test` |
| `ech.bin` | Binary ECHConfigList for the generated key |
| `ready` | Listening address, in `127.0.0.1:PORT` form |
| `count` | Number of accepted TCP connections, including failed TLS handshakes |

Wait for `ready` before connecting. The server handles connections serially,
with a three-second deadline per connection and a 60-second listener lifetime.
Test harnesses should terminate it when finished; expiration closes the
listener and exits with an error.

After a successful TLS handshake, sending `PING` returns a line such as:

```text
ECH accepted=true name=override.example.test
```

## Client

While the server is running, use another terminal to connect:

```sh
tls-ech-tester client "$(cat test-state/ready)" test-state test-state/cert.der
```

The command syntax is:

```text
tls-ech-tester client IP:PORT DIRECTORY ROOT_DER_PATH
```

The client reads `ech.bin` and `cert.der` from `DIRECTORY`, uses
`override.example.test` as the inner server name, and trusts only the DER
certificate at `ROOT_DER_PATH`. It connects to a numeric IP address without DNS,
with a five-second deadline covering the connection and exchange. Input files
must be nonempty regular files no larger than 64 KiB.

The client prints one of these results:

```text
ECH_CLIENT_RESULT status=accepted ech_accepted=true
ECH_CLIENT_RESULT status=rejected retry_config_bytes=0
ECH_CLIENT_RESULT status=certificate-untrusted
```

All three recognized outcomes exit zero so a test harness can assert the
expected result. Acceptance requires the exact server certificate and expected
`PING` response. Rejection uses Go's certificate verification for the public
name. Other failures, including rejection with a nonempty retry configuration,
exit nonzero and write a diagnostic to stderr.

The client does not retry after ECH rejection. The names, cipher configuration,
and application exchange are fixed to keep test results deterministic.

## Build

Release builds use Go 1.27.1 with no third-party dependencies:

```sh
bash build-release.sh /path/to/go output-directory
```

The script builds all six targets with `CGO_ENABLED=0`, `-trimpath`,
`-buildvcs=false`, and `-ldflags=-s -w -buildid=`, and writes a `SHA256SUMS` file
covering the source and binaries.

Publish source or toolchain changes under a new release tag so existing checksum
pins remain valid.
