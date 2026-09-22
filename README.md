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

## Build and test locally

The Go version is pinned in `.go-version`. Build all six release executables with
that toolchain:

```sh
bash build-release.sh /path/to/go dist
```

The script uses `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`, and
`-ldflags=-s -w -buildid=`, and generates `dist/SHA256SUMS` for the source and
binaries. Checksums are generated for each build and published with its release.

Run the integration tests against the executable for your machine. For example,
on macOS ARM64:

```sh
TLS_ECH_TESTER_BINARY="$PWD/dist/tls-ech-tester-darwin-arm64" \
  /path/to/go test -v -count=1 -timeout=2m main.go main_test.go
```

## Release

The [Build, test, and release workflow](https://github.com/nel/tls-ech-tester/actions/workflows/release.yml)
runs on pull requests and pushes to `main`. It builds all six binaries with the
pinned Go toolchain, then tests those exact binaries on native Linux, macOS, and
Windows runners for both AMD64 and ARM64. Tests cover ECH acceptance,
authenticated rejection, certificate trust, and invalid inputs.

To publish a version:

1. Merge the desired changes into `main`. Update `.go-version` when changing the
   compiler version.
2. Open the workflow, select **Run workflow**, choose **main**, and enter a new
   tag such as `tls-ech-tester-v2`. You can also start it from the CLI:

   ```sh
   gh workflow run release.yml --repo nel/tls-ech-tester --ref main \
     -f tag=tls-ech-tester-v2
   ```

3. Wait for the workflow to finish. It publishes the six tested executables and
   `SHA256SUMS`, and creates the tag at the exact commit tested by that run.

Leaving the tag empty runs build and tests without publishing. A failed build
or test prevents publication. The publish job uses GitHub's built-in token;
no extra release secret is required.

Assets are attached to a draft before publication. Published releases are
immutable, so use a new tag for subsequent changes. Existing tags are refused
to keep the release tied to the tested commit. If publication fails after tag
creation, fix the failure and dispatch again with a new unused tag; delete any
incomplete draft from the Releases page. Published releases are never replaced.
