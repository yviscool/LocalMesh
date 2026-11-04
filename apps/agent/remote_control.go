package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"localmesh/internal/capabilities/screen"
)

const (
	remoteControlPort     = 7451
	maxEventFrame         = 4096
	remoteControlIdleTTL  = 30 * time.Second
)

// remoteControlGate allows exactly one authorized teacher connection at a
// time; the gate opens via the teacher's remote_control command.
type remoteControlGate struct {
	mu    sync.Mutex
	open  bool
	until atomic.Int64 // unix nano
}

func (g *remoteControlGate) activate(duration time.Duration) {
	g.mu.Lock()
	g.open = true
	g.mu.Unlock()
	g.until.Store(time.Now().Add(duration).UnixNano())
}

func (g *remoteControlGate) deactivate() {
	g.mu.Lock()
	g.open = false
	g.mu.Unlock()
	g.until.Store(0)
}

func (g *remoteControlGate) isActive() bool {
	g.mu.Lock()
	open := g.open
	g.mu.Unlock()
	expiry := g.until.Load()
	return open && (expiry == 0 || time.Now().UnixNano() < expiry)
}

// startRemoteControlServer accepts one teacher connection at a time on
// :7451 and injects binary InputEvent frames while the gate is open.
func (a *AgentApp) startRemoteControlServer(ctx context.Context) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", remoteControlPort))
	if err != nil {
		return fmt.Errorf("listen remote control: %w", err)
	}
	log.Printf("[RemoteCtl] input channel listening on :%d", remoteControlPort)

	injector := screen.NewPlatformInjector()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				continue
			}
			go a.serveRemoteControlConn(conn, injector)
		}
	}()
	return nil
}

// serveRemoteControlConn decodes length-prefixed InputEvent frames and
// injects them; the connection is dropped the moment the gate closes.
func (a *AgentApp) serveRemoteControlConn(conn net.Conn, injector screen.Injector) {
	defer conn.Close()
	remote := conn.RemoteAddr().String()
	log.Printf("[RemoteCtl] teacher connected from %s", remote)
	defer log.Printf("[RemoteCtl] %s disconnected", remote)

	for {
		if !a.remoteGate.isActive() {
			log.Printf("[RemoteCtl] gate closed — dropping %s", remote)
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(remoteControlIdleTTL))

		var lenBuf [4]byte
		if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
			return
		}
		size := binary.BigEndian.Uint32(lenBuf[:])
		if size == 0 || size > maxEventFrame {
			return
		}
		data := make([]byte, size)
		if _, err := io.ReadFull(conn, data); err != nil {
			return
		}

		var event screen.InputEvent
		if err := event.UnmarshalBinary(data); err != nil {
			continue // malformed frame — skip
		}
		if err := injector.Inject(event); err != nil {
			log.Printf("[RemoteCtl] inject failed: %v", err)
		}
	}
}
