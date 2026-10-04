package quic

import (
	"context"

	q "github.com/quic-go/quic-go"
)

type ConnectionAcceptor struct{ Conn *q.Conn }

func (a ConnectionAcceptor) Accept(ctx context.Context) (Stream, error) {
	if a.Conn == nil {
		return nil, ErrNotConfigured
	}
	return a.Conn.AcceptStream(ctx)
}

var _ StreamAcceptor = ConnectionAcceptor{}
