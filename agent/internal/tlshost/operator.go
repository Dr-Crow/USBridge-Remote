package tlshost

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"
	"time"
)

// LoadOperatorCertificate reads an operator-managed pair without copying it or
// contacting an issuer. Browser trust and renewal remain the operator's job.
// Configuring one path without the other is an error, never a silent fallback.
func (m *Manager) LoadOperatorCertificate(certPath, keyPath string) error {
	certPath, keyPath = strings.TrimSpace(certPath), strings.TrimSpace(keyPath)
	if certPath == "" && keyPath == "" {
		return nil
	}
	if certPath == "" || keyPath == "" {
		return fmt.Errorf("both local TLS certificate and key paths are required")
	}
	cert, leaf, err := loadCertKeyPairFiles(certPath, keyPath)
	if err != nil {
		return fmt.Errorf("load operator TLS pair: %w", err)
	}
	if err := operatorCertUsable(leaf, time.Now()); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.operator, m.operatorLeaf = cert, leaf
	return nil
}

func operatorCertUsable(leaf *x509.Certificate, now time.Time) error {
	if leaf == nil || now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return fmt.Errorf("operator TLS certificate is outside its validity period")
	}
	if leaf.IsCA || len(leaf.DNSNames)+len(leaf.IPAddresses) == 0 {
		return fmt.Errorf("operator TLS certificate must be a leaf with DNS/IP SANs")
	}
	if len(leaf.ExtKeyUsage) > 0 {
		server := false
		for _, usage := range leaf.ExtKeyUsage {
			if usage == x509.ExtKeyUsageServerAuth || usage == x509.ExtKeyUsageAny {
				server = true
			}
		}
		if !server {
			return fmt.Errorf("operator TLS certificate is not valid for server authentication")
		}
	}
	return nil
}

// Caller holds m.mu. Client TLS verification remains fully enabled; the server
// never claims that loading a pair made its issuer trusted on another machine.
func (m *Manager) operatorCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if err := operatorCertUsable(m.operatorLeaf, time.Now()); err != nil {
		return nil, err
	}
	if hello != nil && hello.ServerName != "" {
		if err := m.operatorLeaf.VerifyHostname(hello.ServerName); err != nil {
			return nil, fmt.Errorf("operator TLS certificate does not cover requested server name")
		}
	}
	return m.operator, nil
}
