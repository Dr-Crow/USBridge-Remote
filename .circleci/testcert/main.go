// Generates disposable TLS fixtures for a CI-only, loopback agent probe.
// No OS trust-store changes, external issuer, or persisted CA private key.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: testcert OUTPUT_DIRECTORY")
	}
	if err := generate(os.Args[1]); err != nil {
		log.Fatal(err)
	}
}
func generate(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Disposable CI CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	// Parse the issued CA so its generated SubjectKeyId is available when
	// CreateCertificate derives the leaf AuthorityKeyId. Python/OpenSSL strict
	// verification correctly rejects a non-root leaf missing this identifier.
	ca, err = x509.ParseCertificate(caDER)
	if err != nil {
		return err
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Disposable CI agent"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		return err
	}
	rootPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	if err := os.WriteFile(filepath.Join(dir, "ca.pem"), rootPEM, 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "server.pem"), append(leafPEM, rootPEM...), 0600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "server-key.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600)
}
