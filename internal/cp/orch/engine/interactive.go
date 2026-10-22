// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"io"
	"net"
	"strconv"
	"sync"

	"github.com/musakkka/5050cloud/internal/cp/orch/hub"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Shell opens an interactive exec in a replica (5050 shell, 5050 ssh and
// the console terminal). rw carries terminal bytes; resizes arrive on resize.
func (o *Orchestrator) Shell(ctx context.Context, envID, serviceID, replicaID, session string, cmd []string, cols, rows int, rw io.ReadWriter, resize <-chan [2]int) (int, error) {
	hostID := ""
	if replicaID == "" {
		h, r, err := o.activeReplica(ctx, envID, serviceID)
		if err != nil {
			return -1, err
		}
		hostID, replicaID = h, r
	} else {
		rs, err := o.queryReps(ctx, `id = ? AND environment_id = ? AND service_id = ?`, replicaID, envID, serviceID)
		if err != nil {
			return -1, err
		}
		if len(rs) == 0 || !rs[0].live() {
			return -1, errorf("This replica does not exist or is no longer running.")
		}
		hostID = rs[0].HostID
	}
	if !o.hub.Connected(hostID) {
		return -1, errorf("The host of this replica is offline, so a shell cannot open.")
	}
	if len(cmd) == 0 {
		cmd = []string{"sh"}
	}
	s, err := o.hub.OpenStream(ctx, hostID, proto.BinExec, func(id uint64) (string, any) {
		return proto.TExecOpen, proto.ExecOpen{StreamID: id, ReplicaID: replicaID, Cmd: cmd, TTY: true, Cols: cols, Rows: rows, Session: session}
	})
	if err != nil {
		return -1, err
	}
	defer s.Close()
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		for {
			select {
			case <-sctx.Done():
				return
			case sz, ok := <-resize:
				if !ok {
					return
				}
				_ = s.Resize(sctx, sz[0], sz[1])
			}
		}
	}()
	go func() {
		_, _ = io.Copy(s, rw)
		cancel()
	}()
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(rw, s)
		close(done)
	}()
	select {
	case <-done:
	case <-sctx.Done():
	}
	select {
	case <-s.Done():
	default:
	}
	code, msg := s.Exit()
	if code < 0 && msg != "" {
		return code, errorf("%s", msg)
	}
	return code, nil
}

// Tunnel connects rw to a port of a running replica (5050 connect).
func (o *Orchestrator) Tunnel(ctx context.Context, envID, serviceID string, port int, rw io.ReadWriteCloser) error {
	defer rw.Close()
	hostID, replicaID, err := o.activeReplica(ctx, envID, serviceID)
	if err != nil {
		return err
	}
	rs, err := o.queryReps(ctx, `id = ?`, replicaID)
	if err != nil || len(rs) == 0 || rs[0].IP == "" {
		return errorf("The replica of this service has no address yet.")
	}
	if port <= 0 || port > 65535 {
		return errorf("The port %d is not valid.", port)
	}
	target := net.JoinHostPort(rs[0].IP, strconv.Itoa(port))
	s, err := o.hub.OpenStream(ctx, hostID, proto.BinTCP, func(id uint64) (string, any) {
		return proto.TTCPOpen, proto.TCPOpen{StreamID: id, Target: target}
	})
	if err != nil {
		return err
	}
	defer s.Close()
	return pipe(ctx, s, rw)
}

// pipe copies both ways until both directions end or ctx is done.
func pipe(ctx context.Context, s *hub.Stream, rw io.ReadWriteCloser) error {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(s, rw)
		_ = s.CloseWrite()
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(rw, s)
		if cw, ok := rw.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			_ = rw.Close()
		}
	}()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		_ = rw.Close()
		_ = s.Close()
		<-done
	}
	_, msg := s.Exit()
	if msg != "" {
		return errorf("%s", msg)
	}
	return nil
}
