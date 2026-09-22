// Controlled TLS peer for the optional real-ECH regression. No DNS, CA-store,
// firewall or resolver settings are changed. All keys are ephemeral in memory.
package main

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"time"
)

const innerName = "override.example.test"

func main() {
	if err := run(); err != nil {
		message := err.Error()
		if len(message) > 1024 {
			message = message[:1024] + " (truncated)"
		}
		fmt.Fprintln(os.Stderr, message)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) == 5 && os.Args[1] == "client" {
		return runClient(os.Args[2], os.Args[3], os.Args[4])
	}
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: ech-peer EXISTING_TEST_DIRECTORY | ech-peer client ADDRESS DIRECTORY ROOT_DER_PATH")
	}
	dir := os.Args[1]
	write := func(name string, data []byte) error {
		return os.WriteFile(filepath.Join(dir, name), data, 0600)
	}
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	// RFC 9849 ECHConfig: X25519 / HKDF-SHA256 / AES-128-GCM.
	contents := []byte{7, 0, 0x20, 0, 32}
	contents = append(contents, key.PublicKey().Bytes()...)
	contents = append(contents, 0, 4, 0, 1, 0, 1, 0)
	publicName := "example.test"
	contents = append(contents, byte(len(publicName)))
	contents = append(contents, publicName...)
	contents = append(contents, 0, 0) // no ECHConfig extensions
	config := binary.BigEndian.AppendUint16([]byte{0xfe, 0x0d}, uint16(len(contents)))
	config = append(config, contents...)
	list := binary.BigEndian.AppendUint16(nil, uint16(len(config)))
	list = append(list, config...)
	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: publicName},
		DNSNames:  []string{publicName, innerName},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &certKey.PublicKey, certKey)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	// A lost Rust owner cannot leave a permanent listener behind.
	time.AfterFunc(60*time.Second, func() { listener.Close() })
	for name, data := range map[string][]byte{"cert.der": der, "ech.bin": list, "count": []byte("0")} {
		if err := write(name, data); err != nil {
			return err
		}
	}
	if err := write("ready", []byte(listener.Addr().String())); err != nil {
		return err
	}
	tlsConfig := &tls.Config{
		MinVersion:               tls.VersionTLS13,
		Certificates:             []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: certKey}},
		EncryptedClientHelloKeys: []tls.EncryptedClientHelloKey{{Config: config, PrivateKey: key.Bytes()}},
		SessionTicketsDisabled:   true,
	}
	// Serial, bounded test protocol. Count TCP accepts, not merely successful TLS.
	for count := 1; ; count++ {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		if err := write("count", []byte(fmt.Sprint(count))); err != nil {
			conn.Close()
			return err
		}
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		tlsConn := tls.Server(conn, tlsConfig)
		if err := tlsConn.Handshake(); err == nil {
			var token [4]byte
			if _, err := io.ReadFull(tlsConn, token[:]); err == nil && string(token[:]) == "PING" {
				state := tlsConn.ConnectionState()
				fmt.Fprintf(tlsConn, "ECH accepted=%t name=%s\n", state.ECHAccepted, state.ServerName)
			}
		}
		tlsConn.Close()
	}
}

func readFixture(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	const maximum = 65536
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maximum {
		return nil, fmt.Errorf("fixture must be a nonempty regular file of at most %d bytes", maximum)
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maximum {
		return nil, fmt.Errorf("fixture changed outside byte bound")
	}
	return data, nil
}

// Independent standards-based rejection oracle. Go's nil ECH rejection verifier
// authenticates the public name using RootCAs; never replace that verification
// with an insecure callback or attempt application data after rejection.
func runClient(address, directory, rootPath string) error {
	endpoint, err := netip.ParseAddrPort(address)
	if err != nil || endpoint.Port() == 0 || endpoint.Addr().IsUnspecified() || endpoint.Addr().IsMulticast() {
		return fmt.Errorf("client ADDRESS must be an explicit unicast IP:port, not a DNS name")
	}
	ech, err := readFixture(filepath.Join(directory, "ech.bin"))
	if err != nil {
		return err
	}
	peerDER, err := readFixture(filepath.Join(directory, "cert.der"))
	if err != nil {
		return err
	}
	if _, err := x509.ParseCertificate(peerDER); err != nil {
		return err
	}
	rootDER, err := readFixture(rootPath)
	if err != nil {
		return err
	}
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	config := &tls.Config{
		MinVersion:                     tls.VersionTLS13,
		RootCAs:                        roots,
		ServerName:                     innerName,
		EncryptedClientHelloConfigList: ech,
		NextProtos:                     []string{"http/1.1"},
	}
	deadline := time.Now().Add(5 * time.Second)
	dialer := net.Dialer{Deadline: deadline}
	conn, err := dialer.Dial("tcp", endpoint.String())
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	client := tls.Client(conn, config)
	if err := client.Handshake(); err != nil {
		var rejected *tls.ECHRejectionError
		var untrusted x509.UnknownAuthorityError
		switch {
		case errors.As(err, &rejected):
			if len(rejected.RetryConfigList) != 0 {
				return fmt.Errorf("unexpected ECH retry configuration: %d bytes", len(rejected.RetryConfigList))
			}
			_, err := fmt.Fprintln(os.Stdout, "ECH_CLIENT_RESULT status=rejected retry_config_bytes=0")
			return err
		case errors.As(err, &untrusted):
			_, err := fmt.Fprintln(os.Stdout, "ECH_CLIENT_RESULT status=certificate-untrusted")
			return err
		default:
			return err
		}
	}
	state := client.ConnectionState()
	if !state.ECHAccepted || len(state.PeerCertificates) == 0 || !bytes.Equal(state.PeerCertificates[0].Raw, peerDER) {
		return fmt.Errorf("accepted control requires ECH acceptance and the exact independent peer certificate")
	}
	if n, err := client.Write([]byte("PING")); err != nil {
		return err
	} else if n != 4 {
		return io.ErrShortWrite
	}
	expected := []byte(fmt.Sprintf("ECH accepted=true name=%s\n", innerName))
	response, err := io.ReadAll(io.LimitReader(client, int64(len(expected)+1)))
	if err != nil {
		return err
	}
	if !bytes.Equal(response, expected) {
		return fmt.Errorf("accepted control response did not match the exact bounded peer transcript")
	}
	_, err = fmt.Fprintln(os.Stdout, "ECH_CLIENT_RESULT status=accepted ech_accepted=true")
	return err
}
