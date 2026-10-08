package main

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedChainHasIdentifiersAndVerifiesIP(t *testing.T) {
	dir := t.TempDir()
	if err := generate(dir); err != nil {
		t.Fatal(err)
	}
	read := func(name string) *x509.Certificate {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		block, _ := pem.Decode(raw)
		if block == nil {
			t.Fatal("missing PEM")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		return cert
	}
	ca, leaf := read("ca.pem"), read("server.pem")
	if len(ca.SubjectKeyId) == 0 || !bytes.Equal(leaf.AuthorityKeyId, ca.SubjectKeyId) {
		t.Fatal("missing/mismatched authority key identifier")
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "127.0.0.1", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Fatal(err)
	}
	if leaf.IsCA {
		t.Fatal("server certificate is a CA")
	}
}
