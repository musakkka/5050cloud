// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// TCP proxy settings (docs/SPEC.md 8.4, 9.11).
const (
	TCPProxyMinPort = 20000
	TCPProxyMaxPort = 29999
	tcpIdleTimeout  = time.Hour
	tcpDialTimeout  = 5 * time.Second
)

// tcpProxy runs one listener per allocated host port and forwards each
// connection to the next upstream in round robin, trying the others when a
// dial fails.
type tcpProxy struct {
	log    *slog.Logger
	listen func(network, addr string) (net.Listener, error)
	dial   func(network, addr string, timeout time.Duration) (net.Conn, error)
	idle   time.Duration
	bind   string // host part of listen addresses; "" = every address

	mu        sync.Mutex
	listeners map[int]*tcpListener
}

type tcpListener struct {
	port      int
	ln        net.Listener
	upstreams atomic.Pointer[[]string]
	next      atomic.Uint64
	conns     sync.Map // net.Conn -> struct{}
	closed    atomic.Bool
}

func newTCPProxy(log *slog.Logger) *tcpProxy {
	return &tcpProxy{
		log:       log,
		listen:    net.Listen,
		dial:      net.DialTimeout,
		idle:      tcpIdleTimeout,
		listeners: map[int]*tcpListener{},
	}
}

// apply replaces the proxy set: listeners for new ports open first, then
// upstream lists change in place, then listeners for removed ports close.
// Open connections on a removed port are closed with it.
func (p *tcpProxy) apply(set []proto.TCPProxy) error {
	want := map[int][]string{}
	var errs []error
	for _, t := range set {
		if t.HostPort < TCPProxyMinPort || t.HostPort > TCPProxyMaxPort {
			errs = append(errs, fmt.Errorf("TCP proxy port %d is outside %d to %d and was skipped", t.HostPort, TCPProxyMinPort, TCPProxyMaxPort))
			continue
		}
		if _, dup := want[t.HostPort]; dup {
			errs = append(errs, fmt.Errorf("TCP proxy port %d appears twice; only the first entry is used", t.HostPort))
			continue
		}
		var ups []string
		for _, u := range t.Upstreams {
			if _, port, err := net.SplitHostPort(u); err != nil || port == "" {
				errs = append(errs, fmt.Errorf("upstream %q of TCP proxy port %d is not host:port and was left out", u, t.HostPort))
				continue
			}
			ups = append(ups, u)
		}
		want[t.HostPort] = ups
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	ports := make([]int, 0, len(want))
	for port := range want {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	for _, port := range ports {
		ups := want[port]
		if l, ok := p.listeners[port]; ok {
			cur := l.upstreams.Load()
			if cur == nil || !slices.Equal(*cur, ups) {
				l.upstreams.Store(&ups)
			}
			continue
		}
		ln, err := p.listen("tcp", net.JoinHostPort(p.bind, strconv.Itoa(port)))
		if err != nil {
			errs = append(errs, fmt.Errorf("the TCP proxy cannot listen on port %d: %w", port, err))
			continue
		}
		l := &tcpListener{port: port, ln: ln}
		l.upstreams.Store(&ups)
		p.listeners[port] = l
		go p.serve(l)
	}
	for port, l := range p.listeners {
		if _, ok := want[port]; !ok {
			l.close()
			delete(p.listeners, port)
		}
	}
	return errors.Join(errs...)
}

func (p *tcpProxy) addr(port int) net.Addr {
	p.mu.Lock()
	defer p.mu.Unlock()
	if l, ok := p.listeners[port]; ok {
		return l.ln.Addr()
	}
	return nil
}

func (p *tcpProxy) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for port, l := range p.listeners {
		l.close()
		delete(p.listeners, port)
	}
}

func (l *tcpListener) close() {
	if l.closed.Swap(true) {
		return
	}
	_ = l.ln.Close()
	l.conns.Range(func(k, _ any) bool {
		_ = k.(net.Conn).Close()
		return true
	})
}

func (p *tcpProxy) serve(l *tcpListener) {
	for {
		c, err := l.ln.Accept()
		if err != nil {
			if l.closed.Load() {
				return
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			p.log.Warn("TCP proxy accept failed", "port", l.port, "err", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go p.handle(l, c)
	}
}

func (p *tcpProxy) handle(l *tcpListener, client net.Conn) {
	l.conns.Store(client, struct{}{})
	defer l.conns.Delete(client)
	defer client.Close()
	ups := l.upstreams.Load()
	if ups == nil || len(*ups) == 0 {
		return // no healthy replica: refuse by closing
	}
	list := *ups
	start := int(l.next.Add(1) - 1)
	var up net.Conn
	for i := range list {
		addr := list[(start+i)%len(list)]
		c, err := p.dial("tcp", addr, tcpDialTimeout)
		if err == nil {
			up = c
			break
		}
	}
	if up == nil {
		return
	}
	l.conns.Store(up, struct{}{})
	defer l.conns.Delete(up)
	defer up.Close()
	pipe(client, up, p.idle)
}

// pipe copies both ways with half-close and an idle timeout that any
// traffic in either direction resets.
func pipe(a, b net.Conn, idle time.Duration) {
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		buf := make([]byte, 32<<10)
		for {
			_ = src.SetReadDeadline(time.Now().Add(idle))
			n, err := src.Read(buf)
			if n > 0 {
				last.Store(time.Now().UnixNano())
				if _, werr := dst.Write(buf[:n]); werr != nil {
					break
				}
			}
			if err != nil {
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() && time.Since(time.Unix(0, last.Load())) < idle {
					continue // the other direction is active
				}
				break
			}
		}
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			_ = dst.Close()
		}
		done <- struct{}{}
	}
	go cp(b, a)
	go cp(a, b)
	<-done
	// One side finished; give the other direction until the idle timeout.
	select {
	case <-done:
	case <-time.After(idle):
	}
	_ = a.Close()
	_ = b.Close()
}
