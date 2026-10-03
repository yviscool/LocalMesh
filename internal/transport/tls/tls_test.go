package tls

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"testing"
	"time"

	"localmesh/internal/protocol"
)

func certificate(t *testing.T, ca *x509.Certificate, caKey *rsa.PrivateKey, commonName string, isCA bool) tls.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: commonName}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, IsCA: isCA, BasicConstraintsValid: isCA}
	if isCA {
		template.KeyUsage |= x509.KeyUsageCertSign
	}
	if !isCA {
		template.DNSNames = []string{commonName}
	}
	parent := template
	parentKey := key
	if ca != nil {
		parent = ca
		parentKey = caKey
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func caPool(cert tls.Certificate) *x509.CertPool {
	pool := x509.NewCertPool()
	parsed, _ := x509.ParseCertificate(cert.Certificate[0])
	pool.AddCert(parsed)
	return pool
}

func TestMutualTLSRoundTrip(t *testing.T) {
	caCert := certificate(t, nil, nil, "localmesh-ca", true)
	parsedCA, err := x509.ParseCertificate(caCert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	caKey := caCert.PrivateKey.(*rsa.PrivateKey)
	serverCert := certificate(t, parsedCA, caKey, "localhost", false)
	clientCert := certificate(t, parsedCA, caKey, "client", false)
	roots := caPool(caCert)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	received := make(chan protocol.Envelope, 1)
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- ListenAndServe(ctx, listener, RequireClientCertificates(&tls.Config{Certificates: []tls.Certificate{serverCert}}, roots), func(_ context.Context, message protocol.Envelope) error { received <- message; return nil })
	}()
	now := time.Now()
	message := protocol.Envelope{ProtocolVersion: protocol.CurrentVersion, MessageType: protocol.MessageSessionHeartbeat, MessageID: "msg-1", RequestID: "req-1", SenderID: "client", SentAt: now, Deadline: now.Add(time.Minute)}
	transport := Config{Address: listener.Addr().String(), DialTimeout: time.Second, HandshakeTimeout: time.Second, TLS: &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{clientCert}, ServerName: "localhost"}}
	if err := transport.Send(ctx, message); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-received:
		if got.MessageID != message.MessageID {
			t.Fatalf("message id = %s", got.MessageID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tls message")
	}
	cancel()
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("tls server did not stop")
	}
}

func TestClientRejectsInsecureConfiguration(t *testing.T) {
	config := Config{Address: "127.0.0.1:1", TLS: &tls.Config{InsecureSkipVerify: true, ServerName: "localhost"}}
	if err := config.Send(context.Background(), protocol.Envelope{}); !errors.Is(err, ErrTLSConfig) {
		t.Fatal("expected insecure configuration to fail")
	}
}
