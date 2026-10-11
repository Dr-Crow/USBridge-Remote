package app

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func httpTestDeadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func startHTTPTest(t *testing.T, o *httpServerOwner, role string, h http.Handler, tlsConfig *tls.Config) (*http.Server, net.Listener) {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &http.Server{Handler: h, TLSConfig: tlsConfig, ReadHeaderTimeout: time.Second}
	serve := func() error { return s.Serve(l) }
	if tlsConfig != nil {
		serve = func() error { return s.ServeTLS(l, "", "") }
	}
	if err = o.start(role, s, serve, l.Close); err != nil {
		t.Fatal(err)
	}
	return s, l
}
func expectHTTPPortReleased(t *testing.T, l net.Listener) {
	t.Helper()
	next, e := net.Listen("tcp4", l.Addr().String())
	if e != nil {
		t.Fatal("owned listener not released", e)
	}
	next.Close()
}
func waitHTTPTest(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("owned worker did not join")
	}
}

func TestHTTPServerOwnerClosesHTTPHTTPSAndTSRoles(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := newHTTPServerOwner(ctx, cancel)
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	config := fixture.TLS.Clone()
	client := fixture.Client()
	fixture.Close()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "owned") })
	for _, role := range []string{"http", "https", "tailscale-http"} {
		var tlsConfig *tls.Config
		scheme := "http"
		c := http.DefaultClient
		if role == "https" {
			tlsConfig = config
			scheme = "https"
			c = client
		}
		_, l := startHTTPTest(t, o, role, handler, tlsConfig)
		defer expectHTTPPortReleased(t, l)
		response, e := c.Get(scheme + "://" + l.Addr().String())
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(response.Body)
		response.Body.Close()
		if e != nil || string(b) != "owned" {
			t.Fatal("request failed")
		}
	}
	if err := o.close(httpTestDeadline(t)); err != nil {
		t.Fatal(err)
	}
	if err := o.close(httpTestDeadline(t)); err != nil {
		t.Fatal("repeated close", err)
	}
	for _, g := range o.all {
		select {
		case <-g.done:
		default:
			t.Fatal("Serve not joined")
		}
		select {
		case <-g.closed:
		default:
			t.Fatal("close not joined")
		}
	}
}
func TestHTTPServerOwnerCancelsAndJoinsActiveRequest(t *testing.T) {
	o := newHTTPServerOwner(context.Background(), nil)
	entered, returned := make(chan struct{}), make(chan struct{})
	_, l := startHTTPTest(t, o, "http", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(returned) }), nil)
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		r, _ := http.Get("http://" + l.Addr().String())
		if r != nil {
			r.Body.Close()
		}
	}()
	waitHTTPTest(t, entered)
	if err := o.close(httpTestDeadline(t)); err != nil {
		t.Fatal(err)
	}
	waitHTTPTest(t, returned)
	waitHTTPTest(t, clientDone)
	expectHTTPPortReleased(t, l)
}
func TestHTTPServerOwnerStartupFailureRollsBackOtherRole(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := newHTTPServerOwner(ctx, cancel)
	_, l := startHTTPTest(t, o, "http", http.NotFoundHandler(), nil)
	cause := errors.New("synthetic bind failure")
	if e := o.start("https", &http.Server{}, func() error { return cause }, nil); e != nil {
		t.Fatal(e)
	}
	waitHTTPTest(t, ctx.Done())
	if e := o.close(httpTestDeadline(t)); !errors.Is(e, cause) {
		t.Fatal("startup cause was not retained", e)
	}
	expectHTTPPortReleased(t, l)
}
func TestHTTPServerOwnerDeadlineNeverClaimsHandlerRetired(t *testing.T) {
	o := newHTTPServerOwner(context.Background(), nil)
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	_, l := startHTTPTest(t, o, "http", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; close(returned) }), nil)
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		r, _ := http.Get("http://" + l.Addr().String())
		if r != nil {
			r.Body.Close()
		}
	}()
	waitHTTPTest(t, entered)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	e := o.close(ctx)
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("unjoined handler reported clean", e)
	}
	close(release)
	waitHTTPTest(t, returned)
	waitHTTPTest(t, clientDone)
	if e = o.close(httpTestDeadline(t)); e == nil {
		t.Fatal("earlier cleanup uncertainty was discarded")
	}
	expectHTTPPortReleased(t, l)
}
func TestHTTPServerOwnerHijackIsClosedButNotOverclaimed(t *testing.T) {
	o := newHTTPServerOwner(context.Background(), nil)
	entered, returned := make(chan struct{}), make(chan struct{})
	_, l := startHTTPTest(t, o, "http", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, rw, e := w.(http.Hijacker).Hijack()
		if e != nil {
			panic(e)
		}
		defer c.Close()
		fmt.Fprint(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: test\r\nConnection: Upgrade\r\n\r\n")
		rw.Flush()
		close(entered)
		io.Copy(io.Discard, c)
		close(returned)
	}), nil)
	c, e := net.DialTimeout("tcp", l.Addr().String(), time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	fmt.Fprint(c, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
	c.SetReadDeadline(time.Now().Add(time.Second))
	b := bufio.NewReader(c)
	for {
		line, e := b.ReadString('\n')
		if e != nil {
			t.Fatal(e)
		}
		if line == "\r\n" {
			break
		}
	}
	waitHTTPTest(t, entered)
	if e = o.close(httpTestDeadline(t)); !errors.Is(e, errHTTPHijackedUnverified) {
		t.Fatal("hijacked workers overclaimed", e)
	}
	waitHTTPTest(t, returned)
	o.all[0].mu.Lock()
	retained := len(o.all[0].conns)
	o.all[0].mu.Unlock()
	if retained != 0 {
		t.Fatal("finished hijack history retained")
	}
	expectHTTPPortReleased(t, l)
}
func TestHTTPServerOwnerReplacementAndFinalSeal(t *testing.T) {
	o := newHTTPServerOwner(context.Background(), nil)
	_, l := startHTTPTest(t, o, "http", http.NotFoundHandler(), nil)
	oldGeneration := o.all[0]
	next := &http.Server{Addr: l.Addr().String(), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "next") })}
	if e := o.replace(httpTestDeadline(t), "http", next, next.ListenAndServe); e != nil {
		t.Fatal(e)
	}
	select {
	case <-oldGeneration.closed:
	default:
		t.Fatal("old generation not retired before replacement")
	}
	client := &http.Client{Timeout: time.Second}
	var response *http.Response
	var err error
	for i := 0; i < 100; i++ {
		response, err = client.Get("http://" + next.Addr)
		if err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(body) != "next" {
		t.Fatal("replacement not served")
	}
	if e := o.close(httpTestDeadline(t)); e != nil {
		t.Fatal(e)
	}
	if e := o.replace(httpTestDeadline(t), "http", &http.Server{}, func() error { t.Error("sealed owner started Serve"); return nil }); !errors.Is(e, errHTTPSealed) {
		t.Fatal(e)
	}
	expectHTTPPortReleased(t, l)
}
func TestHTTPServerOwnerConcurrentReplaceAndClose(t *testing.T) {
	for i := 0; i < 20; i++ {
		o := newHTTPServerOwner(context.Background(), nil)
		_, l := startHTTPTest(t, o, "http", http.NotFoundHandler(), nil)
		next := &http.Server{Addr: l.Addr().String(), Handler: http.NotFoundHandler()}
		var wg sync.WaitGroup
		wg.Add(2)
		var replacement, closed error
		go func() {
			defer wg.Done()
			replacement = o.replace(httpTestDeadline(t), "http", next, next.ListenAndServe)
		}()
		go func() { defer wg.Done(); closed = o.close(httpTestDeadline(t)) }()
		wg.Wait()
		if replacement != nil && !errors.Is(replacement, errHTTPSealed) {
			t.Fatal(replacement)
		}
		if closed != nil {
			t.Fatal(closed)
		}
		expectHTTPPortReleased(t, l)
	}
}
func TestHTTPServerOwnerRejectedExternalListenerIsClosed(t *testing.T) {
	o := newHTTPServerOwner(context.Background(), nil)
	if e := o.close(httpTestDeadline(t)); e != nil {
		t.Fatal(e)
	}
	for _, role := range []string{"http", "invalid"} {
		l, e := net.Listen("tcp4", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		err := o.start(role, &http.Server{}, func() error { t.Error("rejected Serve ran"); return nil }, l.Close)
		if err == nil {
			t.Fatal("invalid admission accepted")
		}
		expectHTTPPortReleased(t, l)
	}
}
func TestHTTPServerOwnerExpiredReplacementCannotStart(t *testing.T) {
	o := newHTTPServerOwner(context.Background(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := o.replace(ctx, "https", &http.Server{}, func() error { t.Error("expired replacement ran"); return nil }); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if len(o.all) != 0 {
		t.Fatal("expired generation admitted")
	}
}

func TestHTTPServerOwnerShutdownBeforeInitialServe(t *testing.T) {
	o := newHTTPServerOwner(context.Background(), nil)
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	s := &http.Server{Handler: http.NotFoundHandler()}
	resume := make(chan struct{})
	if e = o.start("http", s, func() error { <-resume; return s.Serve(l) }, l.Close); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- o.close(httpTestDeadline(t)) }()
	waitHTTPTest(t, o.all[0].ctx.Done())
	// Pending external listener is retired even before Serve has entered.
	observedClosed := false
	for i := 0; i < 100; i++ {
		c, e := net.DialTimeout("tcp", l.Addr().String(), time.Millisecond)
		if e != nil {
			observedClosed = true
			break
		}
		c.Close()
		time.Sleep(time.Millisecond)
	}
	close(resume)
	if !observedClosed {
		t.Fatal("pending listener survived final seal")
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	expectHTTPPortReleased(t, l)
}
func TestHTTPServerOwnerReplacementHistoryBounded(t *testing.T) {
	o := newHTTPServerOwner(context.Background(), nil)
	for i := 0; i < 100; i++ {
		s := &http.Server{Addr: "127.0.0.1:0", Handler: http.NotFoundHandler()}
		if e := o.replace(httpTestDeadline(t), "http", s, s.ListenAndServe); e != nil {
			t.Fatal(e)
		}
		if len(o.all) != 1 {
			t.Fatal("retired history accumulated")
		}
	}
	if e := o.close(httpTestDeadline(t)); e != nil {
		t.Fatal(e)
	}
}

func TestHTTPServerOwnerDoesNotSuppressJoinedCloseFailure(t *testing.T) {
	cause := errors.New("substantive close failure")
	for _, err := range []error{cause, errors.Join(net.ErrClosed, cause), errors.Join(http.ErrServerClosed, cause)} {
		if !errors.Is(cleanHTTPClosed(err), cause) {
			t.Fatal("substantive joined error suppressed")
		}
	}
	if cleanHTTPClosed(fmt.Errorf("wrapper: %w", net.ErrClosed)) != nil {
		t.Fatal("ordinary already-closed wrapper rejected")
	}
}

func TestHTTPServerOwnerUnexpectedNilServeCancelsOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := newHTTPServerOwner(ctx, cancel)
	if e := o.start("http", &http.Server{}, func() error { return nil }, nil); e != nil {
		t.Fatal(e)
	}
	waitHTTPTest(t, ctx.Done())
	if e := o.close(httpTestDeadline(t)); e == nil {
		t.Fatal("unexpected Serve termination discarded")
	}
}
