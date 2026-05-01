package tlsdial

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	utls "github.com/refraction-networking/utls"
)

var newStdlibConfig = func(host string) *tls.Config {
	return &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
	}
}

var newUTLSConfig = func(host string) *utls.Config {
	return &utls.Config{
		ServerName: host,
		MinVersion: utls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
	}
}

var afterUTLSHandshake = func() {}

// Dial opens a TLS connection to host:port using the selected fingerprint
// profile. go_stdlib preserves legacy crypto/tls behaviour; all other
// profiles use uTLS ClientHello mimicry.
func Dial(ctx context.Context, network, host, port string, profile Profile) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	addr := net.JoinHostPort(host, port)
	tcpConn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	if profile.Stdlib {
		tlsConn := tls.Client(tcpConn, newStdlibConfig(host))
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = tcpConn.Close()
			return nil, err
		}
		return tlsConn, nil
	}
	tlsConn := utls.UClient(tcpConn, newUTLSConfig(host), profile.ClientHelloID)
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = tcpConn.Close()
		case <-done:
		}
	}()
	if err := tlsConn.Handshake(); err != nil {
		close(done)
		_ = tcpConn.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("utls handshake %s/%s: %w", host, profile.Name, err)
	}
	close(done)
	afterUTLSHandshake()
	if ctxErr := ctx.Err(); ctxErr != nil {
		_ = tcpConn.Close()
		return nil, ctxErr
	}
	return tlsConn, nil
}
