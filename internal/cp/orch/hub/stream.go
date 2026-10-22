// SPDX-License-Identifier: AGPL-3.0-only

package hub

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"sync"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// streamBuffer is how many bytes a stream buffers from the agent before the
// socket's reader waits (docs/SPEC.md 7.5: at most 1 MiB unacknowledged).
const streamBuffer = 1 << 20

// Stream is one exec session or TCP tunnel carried in binary frames.
type Stream struct {
	c    *conn
	kind byte
	ID   uint64

	mu      sync.Mutex
	cond    *sync.Cond
	buf     []byte
	eof     bool
	exit    int
	errText string
	closed  bool
	done    chan struct{}
}

// OpenStream starts a stream: it sends open (exec_open or tcp_open, whose
// StreamID field is filled here) and returns the stream for its data.
func (h *Hub) OpenStream(ctx context.Context, hostID string, kind byte, open func(id uint64) (string, any)) (*Stream, error) {
	c := h.get(hostID)
	if c == nil {
		return nil, &ErrOffline{Host: hostID}
	}
	s := &Stream{c: c, kind: kind, ID: c.nextSID.Add(1), done: make(chan struct{}), exit: -1}
	s.cond = sync.NewCond(&s.mu)
	c.smu.Lock()
	c.streams[s.ID] = s
	c.smu.Unlock()
	t, payload := open(s.ID)
	if err := c.send(ctx, t, NewID("req"), "", payload); err != nil {
		c.dropStream(s.ID)
		return nil, err
	}
	return s, nil
}

func (c *conn) dropStream(id uint64) {
	c.smu.Lock()
	delete(c.streams, id)
	c.smu.Unlock()
}

func (c *conn) onBinary(b []byte) {
	if len(b) < 9 {
		return
	}
	id := binary.BigEndian.Uint64(b[1:9])
	c.smu.Lock()
	s := c.streams[id]
	c.smu.Unlock()
	if s == nil || s.kind != b[0] {
		return
	}
	s.deliver(b[9:])
}

func (c *conn) onStreamEnd(env proto.Envelope) {
	var v struct {
		StreamID uint64 `json:"streamId"`
		Code     *int   `json:"code"`
		Error    string `json:"error"`
	}
	if json.Unmarshal(env.D, &v) != nil {
		return
	}
	c.smu.Lock()
	s := c.streams[v.StreamID]
	c.smu.Unlock()
	if s == nil {
		return
	}
	code := -1
	if v.Code != nil {
		code = *v.Code
	}
	s.remoteClosed(code, v.Error)
}

func (s *Stream) deliver(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(p) == 0 {
		if s.kind == proto.BinTCP {
			s.eof = true // half-close from the remote side
			s.cond.Broadcast()
		}
		return
	}
	for len(s.buf) >= streamBuffer && !s.closed {
		s.cond.Wait()
	}
	s.buf = append(s.buf, p...)
	s.cond.Broadcast()
}

func (s *Stream) remoteClosed(code int, errText string) {
	s.mu.Lock()
	if !s.eof {
		s.eof = true
	}
	if code >= 0 {
		s.exit = code
	}
	if errText != "" {
		s.errText = errText
	}
	s.cond.Broadcast()
	s.mu.Unlock()
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	s.c.dropStream(s.ID)
}

// Read returns data from the agent; io.EOF after the remote end closed.
func (s *Stream) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.buf) == 0 && !s.eof && !s.closed {
		s.cond.Wait()
	}
	if len(s.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	s.cond.Broadcast()
	return n, nil
}

// Write sends data to the agent.
func (s *Stream) Write(p []byte) (int, error) {
	if s.isClosed() {
		return 0, io.ErrClosedPipe
	}
	const chunk = 32 << 10
	written := 0
	for len(p) > 0 {
		n := len(p)
		if n > chunk {
			n = chunk
		}
		if err := s.c.sendBinary(context.Background(), s.kind, s.ID, p[:n]); err != nil {
			return written, err
		}
		written += n
		p = p[n:]
	}
	return written, nil
}

// CloseWrite half-closes a TCP stream (a zero-length payload).
func (s *Stream) CloseWrite() error {
	return s.c.sendBinary(context.Background(), s.kind, s.ID, nil)
}

func (s *Stream) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Close ends the stream on both sides.
func (s *Stream) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.cond.Broadcast()
	s.mu.Unlock()
	t := proto.TTCPClose
	if s.kind == proto.BinExec {
		t = proto.TExecClose
	}
	err := s.c.send(context.Background(), t, NewID("req"), "", proto.StreamClose{StreamID: s.ID})
	s.c.dropStream(s.ID)
	return err
}

// Done is closed when the remote end finished.
func (s *Stream) Done() <-chan struct{} { return s.done }

// Exit returns the exit code of an exec stream (-1 when unknown) and the
// remote error text, if any.
func (s *Stream) Exit() (int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exit, s.errText
}

// Resize sends a terminal size change for an exec stream.
func (s *Stream) Resize(ctx context.Context, cols, rows int) error {
	return s.c.send(ctx, proto.TExecResize, NewID("req"), "", proto.ExecResize{StreamID: s.ID, Cols: cols, Rows: rows})
}
