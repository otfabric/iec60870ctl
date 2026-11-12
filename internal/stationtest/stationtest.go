// SPDX-License-Identifier: MIT

// Package stationtest starts the simulated station of package station for tests, so that
// nothing in the test suite needs a real device.
package stationtest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-iec104/apci"
	"github.com/otfabric/go-iec104/asdu"
	"github.com/otfabric/iec104ctl/internal/station"
	"github.com/otfabric/iec104ctl/internal/types"
)

// Model is the model the tests run against: a point of every type, a command for each, two
// files and uploads. Nothing in it changes by itself, so that results are exact.
const Model = `
station: {name: Test station, common_address: 1}
points:
  - {ioa: 100, type: M_SP_NA_1, value: true, group: 1}
  - {ioa: 101, type: M_SP_NA_1, value: false, quality: [NT, IV], group: 1}
  - {ioa: 200, type: M_DP_NA_1, value: "on", group: 1}
  - {ioa: 300, type: M_ST_NA_1, value: 3, group: 2}
  - {ioa: 400, type: M_BO_NA_1, value: 15, group: 2}
  - {ioa: 1000, type: M_ME_NC_1, value: 230.5, group: 3}
  - {ioa: 1002, type: M_ME_NB_1, value: 1250, group: 3}
  - {ioa: 1003, type: M_ME_NA_1, value: 0.25, group: 3}
  - {ioa: 2000, type: M_IT_NA_1, value: 123456, group: 1}
  - {ioa: 2001, type: M_IT_NA_1, value: 7, group: 2}
commands:
  - {ioa: 5000, type: C_SC_NA_1, target: 100}
  - {ioa: 5001, type: C_DC_NA_1, target: 200, select_required: true}
  - {ioa: 5002, type: C_RC_NA_1, target: 300}
  - {ioa: 5003, type: C_SE_NC_1, target: 1000}
  - {ioa: 5004, type: C_SE_NB_1, target: 1002}
  - {ioa: 5005, type: C_SE_NA_1, target: 1003}
  - {ioa: 5006, type: C_BO_NA_1, target: 400}
  - {ioa: 5007, type: C_SC_NA_1}
files:
  - {ioa: 30000, name: 1, size: 5000, section_size: 2048}
  - {ioa: 30001, name: 2, content: "hello from the station\n"}
uploads: {accept: true}
`

// Station is a station serving on a loopback port.
type Station struct {
	*station.Station
	// Host and Port are where the station listens.
	Host string
	Port uint16

	mu     sync.Mutex
	events []types.ServerEvent
}

// Addr returns host:port.
func (s *Station) Addr() string { return net.JoinHostPort(s.Host, strconv.Itoa(int(s.Port))) }

// Events returns the events the station has logged so far.
func (s *Station) Events() []types.ServerEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]types.ServerEvent(nil), s.events...)
}

// HasEvent reports whether an event of the given kind was logged.
func (s *Station) HasEvent(kind string) bool {
	for _, e := range s.Events() {
		if e.Event == kind {
			return true
		}
	}
	return false
}

// Params are protocol parameters with timers short enough for tests.
func Params() apci.Params {
	return apci.Params{K: 12, W: 8, T0: 2 * time.Second, T1: 2 * time.Second, T2: 100 * time.Millisecond, T3: 0}
}

// Start serves the model on a loopback port until the test ends. edit, when given, changes
// the options before the station is built.
func Start(t testing.TB, model string, edit ...func(*station.Options)) *Station {
	t.Helper()
	m, err := station.Parse([]byte(model))
	if err != nil {
		t.Fatalf("stationtest: model: %v", err)
	}
	out := &Station{Host: "127.0.0.1"}
	opts := station.Options{Link: Params(), ASDU: asdu.IEC104, Log: func(e types.ServerEvent) {
		out.mu.Lock()
		out.events = append(out.events, e)
		out.mu.Unlock()
	}}
	for _, f := range edit {
		f(&opts)
	}
	st, err := station.New(m, opts)
	if err != nil {
		t.Fatalf("stationtest: %v", err)
	}
	ln, err := st.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("stationtest: listen: %v", err)
	}
	out.Station = st
	out.Port = port(t, ln.Addr())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = st.Serve(ln)
	}()
	t.Cleanup(func() {
		_ = st.Close()
		<-done
	})
	return out
}

// FreePort returns a loopback TCP port that was free a moment ago.
func FreePort(t testing.TB) uint16 {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return port(t, ln.Addr())
}

// port is the TCP port of a listening address.
func port(t testing.TB, a net.Addr) uint16 {
	t.Helper()
	p := a.(*net.TCPAddr).Port
	if p > 0 && p <= math.MaxUint16 {
		return uint16(p)
	}
	t.Fatalf("stationtest: port %d", p)
	return 0
}

// Certs is a throw-away certificate authority with a server and a client certificate, as
// PEM files in a temporary directory.
type Certs struct {
	CA, ServerCert, ServerKey, ClientCert, ClientKey string
	// ServerTLS is a server configuration that requires a client certificate of the CA.
	ServerTLS *tls.Config
}

// NewCerts creates the certificates. The server certificate is valid for 127.0.0.1 and
// localhost.
func NewCerts(t testing.TB) Certs {
	t.Helper()
	dir := t.TempDir()
	write := func(name, kind string, der []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	key := func() *ecdsa.PrivateKey {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	template := func(serial int64, name string) *x509.Certificate {
		return &x509.Certificate{
			SerialNumber: big.NewInt(serial),
			Subject:      pkix.Name{CommonName: name},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
		}
	}
	caKey := key()
	caTmpl := template(1, "iec104ctl test CA")
	caTmpl.IsCA, caTmpl.BasicConstraintsValid, caTmpl.KeyUsage = true, true, x509.KeyUsageCertSign
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := x509.ParseCertificate(caDER)
	leaf := func(serial int64, name string, usage x509.ExtKeyUsage) (certPath, keyPath string) {
		k := key()
		tmpl := template(serial, name)
		tmpl.KeyUsage = x509.KeyUsageDigitalSignature
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{usage}
		tmpl.DNSNames = []string{"localhost"}
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &k.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		keyDER, err := x509.MarshalECPrivateKey(k)
		if err != nil {
			t.Fatal(err)
		}
		return write(name+".pem", "CERTIFICATE", der), write(name+".key", "EC PRIVATE KEY", keyDER)
	}
	c := Certs{CA: write("ca.pem", "CERTIFICATE", caDER)}
	c.ServerCert, c.ServerKey = leaf(2, "server", x509.ExtKeyUsageServerAuth)
	c.ClientCert, c.ClientKey = leaf(3, "client", x509.ExtKeyUsageClientAuth)

	pair, err := tls.LoadX509KeyPair(c.ServerCert, c.ServerKey)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	c.ServerTLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert}
	return c
}
