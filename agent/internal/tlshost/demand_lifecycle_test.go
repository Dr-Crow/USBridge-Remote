package tlshost

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCertificateDemandDoesNotWriteUntilOwnedPersistence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-created")
	m := NewManager(dir)
	for i := 0; i < 100; i++ {
		m.NoteDeviceCertDemand()
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("handshake demand created persistent work", err)
	}
	if !m.DeviceCertDemanded(time.Minute) {
		t.Fatal("in-memory demand absent")
	}
	select {
	case <-m.DemandC():
	default:
		t.Fatal("coalesced demand absent")
	}
	select {
	case <-m.DemandC():
		t.Fatal("demand was not coalesced")
	default:
	}
	if err := m.PersistDeviceCertDemand(); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(dir, deviceDemandFile))
	if err != nil || len(first) == 0 {
		t.Fatal("owned persistence absent", err)
	}
	// The explicit owner call is complete. Removal cannot race a detached writer.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	m.NoteDeviceCertDemand()
	if err := m.PersistDeviceCertDemand(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("hourly bound bypassed", err)
	}
}

func TestOperatorModeDoesNotPersistVendorDemand(t *testing.T) {
	cert, key := operatorFixture(t)
	dir := filepath.Join(t.TempDir(), "not-created")
	m := NewManager(dir)
	if err := m.LoadOperatorCertificate(cert, key); err != nil {
		t.Fatal(err)
	}
	m.NoteDeviceCertDemand()
	if err := m.PersistDeviceCertDemand(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("operator-managed TLS created vendor state", err)
	}
}
