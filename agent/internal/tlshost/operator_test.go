package tlshost

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func operatorFixture(t *testing.T) (string, string) {
	t.Helper()
	keyObject, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"agent.test"}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, leaf, &keyObject.PublicKey, keyObject)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(keyObject)
	if err != nil {
		t.Fatal(err)
	}
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	key := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	dir := t.TempDir()
	c, k := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(c, cert, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(k, key, 0600); err != nil {
		t.Fatal(err)
	}
	return c, k
}
func TestOperatorCertificateSelectionAndValidation(t *testing.T) {
	c, k := operatorFixture(t)
	m := NewManager(t.TempDir())
	if err := m.LoadOperatorCertificate(c, ""); err == nil {
		t.Fatal("partial pair accepted")
	}
	if err := m.LoadOperatorCertificate(c, k); err != nil {
		t.Fatal(err)
	}
	if st := m.CertStatus(); !st.OperatorProvided || st.LetsEncrypt || st.ExpiresAt.IsZero() {
		t.Fatal("misleading certificate status")
	}
	for _, name := range []string{"", "agent.test", "127.0.0.1"} {
		got, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: name})
		if err != nil || got != m.operator {
			t.Fatal("operator certificate not selected", name, err)
		}
	}
	if _, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: "wrong.test"}); err == nil {
		t.Fatal("wrong SNI silently fell back")
	}
	if err := m.EnsureSelfSigned([]net.IP{net.ParseIP("127.0.0.1")}, nil); err != nil {
		t.Fatal(err)
	}
	if m.self != nil {
		t.Fatal("operator pair caused unnecessary fallback key generation")
	}
	old := m.operator
	_, otherKey := operatorFixture(t)
	if err := m.LoadOperatorCertificate(c, otherKey); err == nil {
		t.Fatal("mismatched key accepted")
	}
	if m.operator != old {
		t.Fatal("failed load replaced working pair")
	}
	m.operatorLeaf.NotAfter = time.Now().Add(-time.Minute)
	if _, err := m.GetCertificate(&tls.ClientHelloInfo{}); err == nil {
		t.Fatal("expired pair accepted")
	}
	if m.CertStatus().LastError == "" {
		t.Fatal("expiry not surfaced")
	}
}
func TestOperatorTLSHandshakeWithExplicitClientTrust(t *testing.T) {
	c, k := operatorFixture(t)
	m := NewManager(t.TempDir())
	if err := m.LoadOperatorCertificate(c, k); err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(m.operatorLeaf)
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	deadline := time.Now().Add(5 * time.Second)
	left.SetDeadline(deadline)
	right.SetDeadline(deadline)
	server := tls.Server(left, &tls.Config{GetCertificate: m.GetCertificate, MinVersion: tls.VersionTLS12})
	client := tls.Client(right, &tls.Config{RootCAs: pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12})
	done := make(chan error, 1)
	go func() { done <- server.Handshake() }()
	if err := client.Handshake(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(client.ConnectionState().VerifiedChains) == 0 {
		t.Fatal("client did not verify certificate")
	}
}
func TestOperatorCertificateRejectsInvalidPurposeAndValidity(t *testing.T) {
	now := time.Now()
	base := x509.Certificate{NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), DNSNames: []string{"agent.test"}}
	for _, modify := range []func(*x509.Certificate){
		func(c *x509.Certificate) { c.IsCA = true },
		func(c *x509.Certificate) { c.DNSNames = nil },
		func(c *x509.Certificate) { c.NotBefore = now.Add(time.Hour) },
		func(c *x509.Certificate) { c.NotAfter = now.Add(-time.Hour) },
		func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth} },
	} {
		c := base
		modify(&c)
		if err := operatorCertUsable(&c, now); err == nil {
			t.Fatal("invalid operator certificate accepted")
		}
	}
}
