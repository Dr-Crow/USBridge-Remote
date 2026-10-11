package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
)

var (
	errEngineAlreadyRun = errors.New("engine: App Run already invoked")
	errHTTPSealed       = errors.New("http owner: engine is stopping")
	errHTTPRoleBusy     = errors.New("http owner: role already registered or replacing")
	errHTTPRetirement   = errors.New("http owner: retirement incomplete")
	// Closing an observed hijack is not proof that route-owned ping/relay workers
	// joined. Until those routes have explicit joins, never report full cleanup.
	errHTTPHijackedUnverified = errors.New("http owner: hijacked worker cleanup unverified")
)

// Suppress only a single unwrap chain ending in the known already-closed
// sentinel. A joined substantive error must never disappear with ErrClosed.
func cleanHTTPClosed(err error) error {
	original := err
	for i := 0; err != nil && i < 64; i++ {
		if err == net.ErrClosed || err == http.ErrServerClosed {
			return nil
		}
		if _, multiple := err.(interface{ Unwrap() []error }); multiple {
			return original
		}
		err = errors.Unwrap(err)
	}
	return original
}

type httpOwnedGeneration struct {
	server             *http.Server
	abortListener      func() error
	ctx                context.Context
	cancel             context.CancelFunc
	mu                 sync.Mutex
	conns              map[net.Conn]http.ConnState
	hijacked           bool
	stopping           bool
	connectionCloseErr error
	active             int
	idle               chan struct{}
	done               chan struct{}
	serveErr           error
	closeOnce          sync.Once
	closed             chan struct{}
	closeErr           error
}

type httpOwnedConnectionKey struct{}

type httpOwnedRole struct {
	generation *httpOwnedGeneration
	replacing  bool
}

// httpServerOwner tracks only this engine's explicitly registered servers.
// Final Close seals all roles before observing generations; hot replacements
// cannot create an unregistered listener after that point.
type httpServerOwner struct {
	ctx    context.Context
	fail   context.CancelFunc
	mu     sync.Mutex
	sealed bool
	roles  map[string]*httpOwnedRole
	all    []*httpOwnedGeneration
	err    error
}

func newHTTPServerOwner(ctx context.Context, fail context.CancelFunc) *httpServerOwner {
	return &httpServerOwner{ctx: ctx, fail: fail, roles: make(map[string]*httpOwnedRole)}
}

func (o *httpServerOwner) record(err error) {
	if err == nil {
		return
	}
	o.mu.Lock()
	o.err = errors.Join(o.err, err)
	o.mu.Unlock()
	if o.fail != nil {
		o.fail()
	}
}

func validHTTPRole(role string) bool {
	return role == "http" || role == "https" || role == "tailscale-http"
}

// start owns a Serve invocation, including an invocation that fails to bind.
// abort retires an already-acquired external listener if admission is rejected.
func (o *httpServerOwner) start(role string, server *http.Server, serve func() error, abort func() error) error {
	if !validHTTPRole(role) || server == nil || serve == nil || server.BaseContext != nil {
		err := errors.New("http owner: invalid server registration")
		if abort != nil {
			err = errors.Join(err, abort())
		}
		return err
	}
	o.mu.Lock()
	var err error
	if o.sealed || o.ctx.Err() != nil {
		err = errHTTPSealed
	} else if o.roles[role] != nil {
		err = errHTTPRoleBusy
	}
	if err != nil {
		o.mu.Unlock()
		if abort != nil {
			err = errors.Join(err, abort())
		}
		return err
	}
	g := o.makeGeneration(server, abort)
	o.roles[role] = &httpOwnedRole{generation: g}
	o.all = append(o.all, g)
	o.mu.Unlock()
	go o.serve(g, serve)
	return nil
}

func (o *httpServerOwner) makeGeneration(server *http.Server, abort func() error) *httpOwnedGeneration {
	ctx, cancel := context.WithCancel(o.ctx)
	idle := make(chan struct{})
	close(idle)
	g := &httpOwnedGeneration{server: server, abortListener: abort, ctx: ctx, cancel: cancel, conns: make(map[net.Conn]http.ConnState), idle: idle, done: make(chan struct{}), closed: make(chan struct{})}
	handler := server.Handler
	if handler == nil {
		handler = http.DefaultServeMux
	}
	previousContext := server.ConnContext
	server.ConnContext = func(ctx context.Context, c net.Conn) context.Context {
		if previousContext != nil {
			ctx = previousContext(ctx, c)
		}
		return context.WithValue(ctx, httpOwnedConnectionKey{}, c)
	}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		if g.active == 0 {
			g.idle = make(chan struct{})
		}
		g.active++
		g.mu.Unlock()
		defer func() {
			// A hijack no longer receives StateClosed from net/http. Once its outer
			// handler returns, retire this exact socket and avoid an unbounded history
			// of already-closed websocket connections. Worker proof remains explicit.
			conn, _ := r.Context().Value(httpOwnedConnectionKey{}).(net.Conn)
			g.mu.Lock()
			state, known := g.conns[conn]
			g.mu.Unlock()
			if known && state == http.StateHijacked {
				err := conn.Close()
				g.mu.Lock()
				if cleanHTTPClosed(err) == nil {
					delete(g.conns, conn)
				} else {
					g.connectionCloseErr = errors.Join(g.connectionCloseErr, err)
				}
				g.mu.Unlock()
			}
			g.mu.Lock()
			g.active--
			if g.active == 0 {
				close(g.idle)
			}
			g.mu.Unlock()
		}()
		handler.ServeHTTP(w, r)
	})
	server.BaseContext = func(net.Listener) context.Context { return ctx }
	previous := server.ConnState
	server.ConnState = func(conn net.Conn, state http.ConnState) {
		g.mu.Lock()
		if state == http.StateClosed {
			delete(g.conns, conn)
		} else {
			g.conns[conn] = state
		}
		if state == http.StateHijacked {
			g.hijacked = true
		}
		closeHijack := state == http.StateHijacked && g.stopping
		g.mu.Unlock()
		if closeHijack {
			if e := cleanHTTPClosed(conn.Close()); e != nil {
				g.mu.Lock()
				g.connectionCloseErr = errors.Join(g.connectionCloseErr, e)
				g.mu.Unlock()
			}
		}
		if previous != nil {
			previous(conn, state)
		}
	}
	return g
}

func (o *httpServerOwner) serve(g *httpOwnedGeneration, serve func() error) {
	err := serve()
	g.mu.Lock()
	stopping := g.stopping
	g.mu.Unlock()
	if err == nil && !stopping {
		err = errors.New("http owner: Serve ended unexpectedly")
	}
	if err == http.ErrServerClosed {
		err = nil
	} else if stopping {
		err = cleanHTTPClosed(err)
	}
	g.mu.Lock()
	g.serveErr = err
	g.mu.Unlock()
	close(g.done)
	if err != nil {
		o.record(err)
	}
}

func (g *httpOwnedGeneration) close(ctx context.Context) error {
	g.closeOnce.Do(func() {
		go func() {
			defer close(g.closed)
			g.cancel()
			g.mu.Lock()
			g.stopping = true
			hijacked := g.hijacked
			connections := make([]net.Conn, 0, len(g.conns))
			for c := range g.conns {
				connections = append(connections, c)
			}
			g.mu.Unlock()
			var err error
			if g.abortListener != nil {
				if e := cleanHTTPClosed(g.abortListener()); e != nil {
					err = errors.Join(err, e)
				}
			}
			// A hijacked connection is outside net/http shutdown. Close only sockets
			// observed on this generation, while retaining the explicit proof gap.
			if hijacked {
				err = errors.Join(err, errHTTPHijackedUnverified)
				for _, c := range connections {
					if e := cleanHTTPClosed(c.Close()); e != nil {
						err = errors.Join(err, e)
					}
				}
			}
			if e := cleanHTTPClosed(g.server.Shutdown(ctx)); e != nil {
				err = errors.Join(err, e, cleanHTTPClosed(g.server.Close()))
			}
			wait := func(done <-chan struct{}) bool {
				select {
				case <-done:
					return true
				default:
				}
				select {
				case <-done:
					return true
				case <-ctx.Done():
					return false
				}
			}
			if !wait(g.done) {
				err = errors.Join(err, errHTTPRetirement, ctx.Err())
			}
			g.mu.Lock()
			idle := g.idle
			serveErr := g.serveErr
			hijacked = g.hijacked
			g.mu.Unlock()
			if !wait(idle) {
				err = errors.Join(err, errHTTPRetirement, ctx.Err())
			}
			if hijacked {
				err = errors.Join(err, errHTTPHijackedUnverified)
			}
			g.mu.Lock()
			connectionCloseErr := g.connectionCloseErr
			g.mu.Unlock()
			g.closeErr = errors.Join(err, serveErr, connectionCloseErr)
		}()
	})
	select {
	case <-g.closed:
		return g.closeErr
	default:
	}
	select {
	case <-g.closed:
		return g.closeErr
	case <-ctx.Done():
		return errors.Join(errHTTPRetirement, ctx.Err())
	}
}

// replace retires one exact generation before admitting its successor. Final
// seal wins any race; an incomplete retirement never starts a replacement.
func (o *httpServerOwner) replace(ctx context.Context, role string, next *http.Server, serve func() error) error {
	if ctx == nil || !validHTTPRole(role) || (next != nil && (serve == nil || next.BaseContext != nil)) {
		return errors.New("http owner: invalid replacement")
	}
	o.mu.Lock()
	if o.sealed || o.ctx.Err() != nil || ctx.Err() != nil {
		o.mu.Unlock()
		return errors.Join(errHTTPSealed, ctx.Err())
	}
	slot := o.roles[role]
	if slot == nil {
		slot = &httpOwnedRole{}
		o.roles[role] = slot
	}
	if slot.replacing {
		o.mu.Unlock()
		return errHTTPRoleBusy
	}
	slot.replacing = true
	old := slot.generation
	o.mu.Unlock()
	if old != nil {
		if err := old.close(ctx); err != nil {
			o.record(err)
			return err
		}
	}
	o.mu.Lock()
	if o.sealed || o.ctx.Err() != nil || ctx.Err() != nil {
		retiredButExpired := !o.sealed && o.ctx.Err() == nil && ctx.Err() != nil
		err := errors.Join(errHTTPSealed, ctx.Err())
		o.mu.Unlock()
		// The previous generation is already gone. If this replacement's
		// budget expired, cancel the engine instead of silently leaving the
		// role absent. A final seal already owns its own shutdown.
		if retiredButExpired {
			o.record(err)
		}
		return err
	}
	// Only successful, fully joined retirement may be pruned. Prior failures
	// stay in o.err and abort replacement. At most one live generation per role
	// remains, rather than retaining an unbounded history of hot applications.
	if old != nil {
		for i, g := range o.all {
			if g == old {
				o.all = append(o.all[:i], o.all[i+1:]...)
				break
			}
		}
	}
	if next == nil {
		delete(o.roles, role)
		o.mu.Unlock()
		return nil
	}
	g := o.makeGeneration(next, nil)
	slot.generation = g
	slot.replacing = false
	o.all = append(o.all, g)
	o.mu.Unlock()
	go o.serve(g, serve)
	return nil
}

func (o *httpServerOwner) close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("http owner: close deadline required")
	}
	o.mu.Lock()
	o.sealed = true
	all := append([]*httpOwnedGeneration(nil), o.all...)
	o.mu.Unlock()
	results := make(chan error, len(all))
	for _, g := range all {
		go func() { results <- g.close(ctx) }()
	}
	var result error
	for range all {
		select {
		case err := <-results:
			result = errors.Join(result, err)
		case <-ctx.Done():
			return errors.Join(result, errHTTPRetirement, ctx.Err())
		}
	}
	o.mu.Lock()
	result = errors.Join(result, o.err)
	o.mu.Unlock()
	return result
}
