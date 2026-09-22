package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testBinary string

func TestMain(m *testing.M) {
	path := os.Getenv("TLS_ECH_TESTER_BINARY")
	if path == "" {
		fmt.Fprintln(os.Stderr, "TLS_ECH_TESTER_BINARY must name the release executable under test")
		os.Exit(2)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve TLS_ECH_TESTER_BINARY: %v\n", err)
		os.Exit(2)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		fmt.Fprintf(os.Stderr, "TLS_ECH_TESTER_BINARY must be a regular file: %s\n", absolute)
		os.Exit(2)
	}
	testBinary = absolute
	os.Exit(m.Run())
}

func runTester(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	command := exec.CommandContext(ctx, testBinary, args...)
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("tls-ech-tester timed out: args=%q stdout=%q stderr=%q", args, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String(), err
}

type testerServer struct {
	cancel context.CancelFunc
	cmd    *exec.Cmd
	done   chan struct{}
	err    error
	stdout bytes.Buffer
	stderr bytes.Buffer
}

func startTesterServer(t *testing.T) (string, *testerServer) {
	t.Helper()
	directory := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	server := &testerServer{cancel: cancel, done: make(chan struct{})}
	server.cmd = exec.CommandContext(ctx, testBinary, directory)
	server.cmd.Stdout = &server.stdout
	server.cmd.Stderr = &server.stderr
	if err := server.cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start tls-ech-tester server: %v", err)
	}
	go func() {
		server.err = server.cmd.Wait()
		close(server.done)
	}()
	t.Cleanup(func() { server.stop(t) })

	ready := filepath.Join(directory, "ready")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-server.done:
			t.Fatalf("tls-ech-tester server exited before readiness: %v, stdout=%q stderr=%q", server.err, server.stdout.String(), server.stderr.String())
		default:
		}
		address, err := os.ReadFile(ready)
		if err == nil && len(address) != 0 {
			endpoint, parseErr := netip.ParseAddrPort(string(address))
			if parseErr != nil || !endpoint.Addr().IsLoopback() || endpoint.Port() == 0 {
				server.stop(t)
				t.Fatalf("invalid ready address %q: %v", address, parseErr)
			}
			return directory, server
		}
		time.Sleep(25 * time.Millisecond)
	}
	server.stop(t)
	t.Fatalf("tls-ech-tester server did not become ready: stdout=%q stderr=%q", server.stdout.String(), server.stderr.String())
	return "", nil
}

func (server *testerServer) stop(t *testing.T) {
	t.Helper()
	server.cancel()
	select {
	case <-server.done:
		return
	case <-time.After(5 * time.Second):
	}
	if err := server.cmd.Process.Kill(); err != nil {
		t.Errorf("kill tls-ech-tester server: %v", err)
	}
	select {
	case <-server.done:
	case <-time.After(5 * time.Second):
		t.Errorf("tls-ech-tester server did not exit after kill")
	}
}

func TestAcceptedClientServer(t *testing.T) {
	directory, _ := startTesterServer(t)
	address := readTextFile(t, filepath.Join(directory, "ready"))
	stdout, stderr, err := runTester(t, "client", address, directory, filepath.Join(directory, "cert.der"))
	if err != nil {
		t.Fatalf("accepted client failed: %v, stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stdout != "ECH_CLIENT_RESULT status=accepted ech_accepted=true\n" || stderr != "" {
		t.Fatalf("unexpected accepted result: stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCertificateUntrusted(t *testing.T) {
	directory, _ := startTesterServer(t)
	_, wrongRoot := newCertificate(t, "override.example.test")
	rootPath := filepath.Join(t.TempDir(), "wrong-root.der")
	if err := os.WriteFile(rootPath, wrongRoot, 0600); err != nil {
		t.Fatal(err)
	}
	address := readTextFile(t, filepath.Join(directory, "ready"))
	stdout, stderr, err := runTester(t, "client", address, directory, rootPath)
	if err != nil {
		t.Fatalf("untrusted client failed: %v, stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stdout != "ECH_CLIENT_RESULT status=certificate-untrusted\n" || stderr != "" {
		t.Fatalf("unexpected untrusted result: stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestAuthenticatedECHRejection(t *testing.T) {
	directory, generatedServer := startTesterServer(t)
	generatedServer.stop(t)

	certificate, rootDER := newCertificate(t, "example.test")
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	done := make(chan struct{})
	go func() {
		defer close(done)
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
		server := tls.Server(connection, &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{certificate},
		})
		_ = server.Handshake()
	}()
	t.Cleanup(func() {
		listener.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("rejection server did not exit")
		}
	})

	rootPath := filepath.Join(t.TempDir(), "public-name-root.der")
	if err := os.WriteFile(rootPath, rootDER, 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runTester(t, "client", listener.Addr().String(), directory, rootPath)
	if err != nil {
		t.Fatalf("rejected client failed: %v, stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stdout != "ECH_CLIENT_RESULT status=rejected retry_config_bytes=0\n" || stderr != "" {
		t.Fatalf("unexpected rejected result: stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestInvalidInputs(t *testing.T) {
	t.Run("usage", func(t *testing.T) {
		_, stderr, err := runTester(t)
		if err == nil || !strings.Contains(stderr, "usage: tls-ech-tester ") {
			t.Fatalf("expected usage error, got err=%v stderr=%q", err, stderr)
		}
	})

	t.Run("DNS address", func(t *testing.T) {
		_, stderr, err := runTester(t, "client", "example.test:443", t.TempDir(), "missing-root.der")
		if err == nil || !strings.Contains(stderr, "explicit unicast IP:port") {
			t.Fatalf("expected numeric-address error, got err=%v stderr=%q", err, stderr)
		}
	})

	for _, fixture := range []struct {
		name string
		size int
	}{{"empty fixture", 0}, {"oversized fixture", 65537}} {
		t.Run(fixture.name, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, "ech.bin"), make([]byte, fixture.size), 0600); err != nil {
				t.Fatal(err)
			}
			_, stderr, err := runTester(t, "client", "127.0.0.1:1", directory, "missing-root.der")
			if err == nil || !strings.Contains(stderr, "fixture must be a nonempty regular file of at most 65536 bytes") {
				t.Fatalf("expected fixture bound error, got err=%v stderr=%q", err, stderr)
			}
		})
	}
}

func newCertificate(t *testing.T, name string) (tls.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		DNSNames:              []string{name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, der
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}
