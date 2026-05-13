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
		NextProtos: []string{"http/1.1"},
	}
}

var newUTLSConfig = func(host string) *utls.Config {
	return &utls.Config{
		ServerName: host,
		MinVersion: utls.VersionTLS12,
		NextProtos: []string{"http/1.1"},
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
	if err := forceHTTP11ALPN(tlsConn); err != nil {
		_ = tcpConn.Close()
		return nil, fmt.Errorf("prepare utls handshake %s/%s: %w", host, profile.Name, err)
	}
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

func forceHTTP11ALPN(conn *utls.UConn) error {
	if err := conn.BuildHandshakeState(); err != nil {
		return err
	}
	conn.Extensions = forceHTTP11Extensions(conn.Extensions)
	return conn.BuildHandshakeState()
}

func forceHTTP11Extensions(exts []utls.TLSExtension) []utls.TLSExtension {
	forced := make([]utls.TLSExtension, 0, len(exts))
	alpnSet := false
	for _, ext := range exts {
		switch ext.(type) {
		case *utls.ALPNExtension:
			forced = append(forced, &utls.ALPNExtension{AlpnProtocols: []string{"http/1.1"}})
			alpnSet = true
		case *utls.ApplicationSettingsExtension, *utls.ApplicationSettingsExtensionNew:
			// ALPS is only useful with HTTP/2. The custom upstream transport
			// speaks HTTP/1.1 over DialTLSContext, so advertising h2-side ALPS
			// can produce a negotiated protocol the transport cannot parse.
		default:
			forced = append(forced, ext)
		}
	}
	if !alpnSet {
		forced = append(forced, &utls.ALPNExtension{AlpnProtocols: []string{"http/1.1"}})
	}
	return forced
}
