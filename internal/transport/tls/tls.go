package tls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"time"

	"localmesh/internal/protocol"
	"localmesh/internal/transport/tcp"
)

var ErrTLSConfig = errors.New("invalid tls transport configuration")

type Config struct {
	TLS              *tls.Config
	Address          string
	DialTimeout      time.Duration
	HandshakeTimeout time.Duration
	WriteTimeout     time.Duration
}

func (c Config) clientConfig() (*tls.Config, error) {
	if c.TLS == nil || c.Address == "" {
		return nil, ErrTLSConfig
	}
	config := c.TLS.Clone()
	if config.MinVersion == 0 {
		config.MinVersion = tls.VersionTLS13
	}
	if config.ServerName == "" {
		return nil, fmt.Errorf("%w: server name is required", ErrTLSConfig)
	}
	if config.InsecureSkipVerify {
		return nil, fmt.Errorf("%w: insecure skip verify is forbidden", ErrTLSConfig)
	}
	return config, nil
}

func (c Config) Send(ctx context.Context, message protocol.Envelope) error {
	config, err := c.clientConfig()
	if err != nil {
		return err
	}
	dialer := &net.Dialer{Timeout: c.DialTimeout}
	rawConn, err := dialer.DialContext(ctx, "tcp", c.Address)
	if err != nil {
		return fmt.Errorf("tls dial: %w", err)
	}
	conn := tls.Client(rawConn, config)
	defer conn.Close()
	if c.HandshakeTimeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(c.HandshakeTimeout))
	}
	if err := conn.HandshakeContext(ctx); err != nil {
		return fmt.Errorf("tls handshake: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})
	return tcp.WriteMessage(ctx, conn, message, c.WriteTimeout)
}

func ListenAndServe(ctx context.Context, listener net.Listener, config *tls.Config, handler func(context.Context, protocol.Envelope) error) error {
	if listener == nil || config == nil || handler == nil {
		return ErrTLSConfig
	}
	serverConfig := config.Clone()
	if serverConfig.MinVersion == 0 {
		serverConfig.MinVersion = tls.VersionTLS13
	}
	tlsListener := tls.NewListener(listener, serverConfig)
	go func() {
		<-ctx.Done()
		_ = tlsListener.Close()
	}()
	for {
		conn, err := tlsListener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("accept tls connection: %w", err)
		}
		go func(conn net.Conn) {
			defer conn.Close()
			if err := conn.(*tls.Conn).HandshakeContext(ctx); err != nil {
				return
			}
			message, err := tcp.ReadMessage(ctx, conn)
			if err == nil {
				_ = handler(ctx, message)
			}
		}(conn)
	}
}

func RequireClientCertificates(config *tls.Config, roots *x509.CertPool) *tls.Config {
	result := config.Clone()
	result.ClientCAs = roots
	result.ClientAuth = tls.RequireAndVerifyClientCert
	return result
}
