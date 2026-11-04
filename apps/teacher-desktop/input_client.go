package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"localmesh/internal/capabilities/screen"
)

// RemoteControlPort is the agent's input-injection TCP port.
const RemoteControlPort = 7451

// inputConn is a persistent teacher→agent input channel.
type inputConn struct {
	mu   sync.Mutex
	conn net.Conn
}

func (ic *inputConn) write(frame []byte) error {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	if ic.conn == nil {
		return fmt.Errorf("input channel closed")
	}
	_ = ic.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := binary.Write(ic.conn, binary.BigEndian, uint32(len(frame))); err != nil {
		return err
	}
	_, err := ic.conn.Write(frame)
	return err
}

func (ic *inputConn) close() {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	if ic.conn != nil {
		_ = ic.conn.Close()
		ic.conn = nil
	}
}

// inputEventPayload mirrors screen.InputEvent as sent from the page.
type inputEventPayload struct {
	Type       int     `json:"type"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Button     int     `json:"button"`
	WheelDelta int32   `json:"wheel_delta"`
	KeyCode    uint32  `json:"key_code"`
	Normalized bool    `json:"normalized"`
}

// dialInput opens the input channel to the agent for remote control.
func (s *ControlService) dialInput(_ context.Context, deviceID string) error {
	s.c.inputMu.Lock()
	if _, ok := s.c.inputConns[deviceID]; ok {
		s.c.inputMu.Unlock()
		return nil // already connected
	}
	s.c.inputMu.Unlock()

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", resolveAgentHost(s.c, deviceID), RemoteControlPort), 3*time.Second)
	if err != nil {
		return fmt.Errorf("agent input channel: %w", err)
	}
	s.c.inputMu.Lock()
	s.c.inputConns[deviceID] = &inputConn{conn: conn}
	s.c.inputMu.Unlock()
	return nil
}

// closeInput drops the input channel.
func (s *ControlService) closeInput(deviceID string) {
	s.c.inputMu.Lock()
	ic := s.c.inputConns[deviceID]
	delete(s.c.inputConns, deviceID)
	s.c.inputMu.Unlock()
	if ic != nil {
		ic.close()
	}
}

// SendInputEvent forwards one input event to the agent's injector.
func (s *ControlService) SendInputEvent(_ context.Context, deviceID string, payload inputEventPayload) error {
	s.c.inputMu.Lock()
	ic := s.c.inputConns[deviceID]
	s.c.inputMu.Unlock()
	if ic == nil {
		return fmt.Errorf("input channel to %s is not open", deviceID)
	}

	event := screen.InputEvent{
		Type:       screen.InputEventType(payload.Type),
		X:          payload.X,
		Y:          payload.Y,
		Button:     screen.MouseButton(payload.Button),
		WheelDelta: payload.WheelDelta,
		KeyCode:    payload.KeyCode,
		Normalized: payload.Normalized,
	}
	frame, err := event.MarshalBinary()
	if err != nil {
		return err
	}
	return ic.write(frame)
}
