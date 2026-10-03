package tcp

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"localmesh/internal/protocol"
)

const MaxFrameBytes = protocol.MaxMessageBytes + 16*1024

var (
	ErrFrameTooLarge = errors.New("tcp frame too large")
	ErrInvalidFrame  = errors.New("invalid tcp frame")
)

type Transport struct {
	Address      string
	DialTimeout  time.Duration
	WriteTimeout time.Duration
}

func (t Transport) Send(ctx context.Context, message protocol.Envelope) error {
	if err := message.Validate(time.Now()); err != nil {
		return fmt.Errorf("validate outbound message: %w", err)
	}
	address := t.Address
	if address == "" {
		return errors.New("tcp address is required")
	}
	dialer := net.Dialer{Timeout: t.DialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("dial tcp control transport: %w", err)
	}
	defer conn.Close()
	return writeMessage(ctx, conn, message, t.WriteTimeout)
}

func ListenAndServe(ctx context.Context, listener net.Listener, handler func(context.Context, protocol.Envelope) error) error {
	if listener == nil || handler == nil {
		return errors.New("tcp listener and handler are required")
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("accept tcp control connection: %w", err)
		}
		go serveConnection(ctx, conn, handler)
	}
}

func serveConnection(ctx context.Context, conn net.Conn, handler func(context.Context, protocol.Envelope) error) {
	defer conn.Close()
	message, err := readMessage(ctx, conn)
	if err != nil {
		return
	}
	_ = handler(ctx, message)
}

func WriteMessage(ctx context.Context, conn net.Conn, message protocol.Envelope, timeout time.Duration) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal protocol envelope: %w", err)
	}
	if len(payload) > MaxFrameBytes {
		return ErrFrameTooLarge
	}
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
	copy(frame[4:], payload)
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetWriteDeadline(deadline)
	} else if timeout > 0 {
		_ = conn.SetWriteDeadline(time.Now().Add(timeout))
	}
	if _, err := conn.Write(frame); err != nil {
		return fmt.Errorf("write protocol frame: %w", err)
	}
	return nil
}

func ReadMessage(ctx context.Context, conn net.Conn) (protocol.Envelope, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetReadDeadline(deadline)
	}
	var header [4]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return protocol.Envelope{}, fmt.Errorf("read protocol frame header: %w", err)
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 || length > MaxFrameBytes {
		return protocol.Envelope{}, ErrFrameTooLarge
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return protocol.Envelope{}, fmt.Errorf("read protocol frame: %w", err)
	}
	var message protocol.Envelope
	if err := json.Unmarshal(payload, &message); err != nil {
		return protocol.Envelope{}, fmt.Errorf("%w: %v", ErrInvalidFrame, err)
	}
	if err := message.Validate(time.Now()); err != nil {
		return protocol.Envelope{}, fmt.Errorf("validate inbound message: %w", err)
	}
	return message, nil
}

func writeMessage(ctx context.Context, conn net.Conn, message protocol.Envelope, timeout time.Duration) error {
	return WriteMessage(ctx, conn, message, timeout)
}

func readMessage(ctx context.Context, conn net.Conn) (protocol.Envelope, error) {
	return ReadMessage(ctx, conn)
}
