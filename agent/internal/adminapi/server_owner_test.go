package adminapi

import (
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAdminCloseBeforeServeRetiresAcquiredListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	path := filepath.Join(t.TempDir(), "owned-marker")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{listener: ln, httpSrv: &http.Server{}, socketPath: path}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("owned path remained", err)
	}
	c, err := net.DialTimeout("tcp", ln.Addr().String(), 100*time.Millisecond)
	if err == nil {
		c.Close()
		t.Fatal("listener survived close before Serve")
	}
	if err := s.Serve(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestAdminCloseServeRace(t *testing.T) {
	for i := 0; i < 30; i++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		s := &Server{listener: ln, httpSrv: &http.Server{}, socketPath: filepath.Join(t.TempDir(), "absent")}
		done := make(chan error, 1)
		go func() { done <- s.Serve() }()
		var wg sync.WaitGroup
		for n := 0; n < 4; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := s.Close(); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("Serve did not retire")
		}
	}
}
func TestAdminListenerCloseDoesNotMaskJoinedFailure(t *testing.T) {
	failure := errors.New("release failed")
	if adminListenerCloseError(net.ErrClosed) != nil {
		t.Fatal("ordinary closed failure")
	}
	if err := adminListenerCloseError(errors.Join(net.ErrClosed, failure)); !errors.Is(err, failure) {
		t.Fatal("release failure masked")
	}
}

func TestAdminPermissionFailureRetainsListenerOwnership(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	denied := errors.New("fixture permission failure")
	path := filepath.Join(t.TempDir(), "absent")
	s, err := adminServerFromListener(path, ln, func(got string, mode os.FileMode) error {
		if got != path || mode != 0600 {
			t.Fatal("permission contract changed")
		}
		return denied
	})
	if s == nil || !errors.Is(err, denied) {
		t.Fatal("partial ownership lost", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	c, err := net.DialTimeout("tcp", ln.Addr().String(), 100*time.Millisecond)
	if err == nil {
		c.Close()
		t.Fatal("partial listener survived close")
	}
}

type adminCloseFailureListener struct {
	net.Listener
	failure error
}

func (l adminCloseFailureListener) Close() error { return errors.Join(l.Listener.Close(), l.failure) }
func TestAdminClosePreservesAcquiredListenerFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("fixture close uncertainty")
	s := &Server{listener: adminCloseFailureListener{ln, failure}, httpSrv: &http.Server{}, socketPath: filepath.Join(t.TempDir(), "absent")}
	err = s.Close()
	if !errors.Is(err, failure) {
		t.Fatal("release uncertainty discarded", err)
	}
	if s.Close() != err {
		t.Fatal("repeated close replaced original result")
	}
}
