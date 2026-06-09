package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy"
)

func TestStartProxyFn_DefaultClosureStartsAndStopsProxy(t *testing.T) {
	cfg := config.Defaults()
	cfg.Proxy.ListenPort = 0
	cfg.Compression.Layer2Enabled = false
	cfg.Secrets.Mode = "off"

	shutdown, err := startProxyFn(cfg)
	if err != nil {
		t.Fatalf("startProxyFn failed: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
}

func TestStartProxyFn_DefaultClosureStartError(t *testing.T) {
	origNewProxy := newProxyFn
	origRunner := proxyStartRunnerFn
	origHasListener := proxyHasListenerFn
	defer func() {
		newProxyFn = origNewProxy
		proxyStartRunnerFn = origRunner
		proxyHasListenerFn = origHasListener
	}()

	newProxyFn = func(*config.Config) *proxy.Proxy { return &proxy.Proxy{} }
	proxyStartRunnerFn = func(*proxy.Proxy) error { return errors.New("start boom") }
	proxyHasListenerFn = func(*proxy.Proxy) bool { return false }

	_, err := startProxyFn(config.Defaults())
	if err == nil || err.Error() != "start boom" {
		t.Fatalf("expected start error, got %v", err)
	}
}

func TestStartProxyFn_DefaultClosureTimeout(t *testing.T) {
	origNewProxy := newProxyFn
	origRunner := proxyStartRunnerFn
	origHasListener := proxyHasListenerFn
	origTimeout := proxyStartTimeout
	defer func() {
		newProxyFn = origNewProxy
		proxyStartRunnerFn = origRunner
		proxyHasListenerFn = origHasListener
		proxyStartTimeout = origTimeout
	}()

	block := make(chan struct{})
	newProxyFn = func(*config.Config) *proxy.Proxy { return &proxy.Proxy{} }
	proxyStartRunnerFn = func(*proxy.Proxy) error {
		<-block
		return nil
	}
	proxyHasListenerFn = func(*proxy.Proxy) bool { return false }
	proxyStartTimeout = 20 * time.Millisecond

	_, err := startProxyFn(config.Defaults())
	close(block)
	if err == nil || !strings.Contains(err.Error(), "proxy start timeout") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestStartProxyFn_DefaultClosureDeadlineWithListener(t *testing.T) {
	origNewProxy := newProxyFn
	origRunner := proxyStartRunnerFn
	origHasListener := proxyHasListenerFn
	origTimeout := proxyStartTimeout
	origAfter := timeAfterFn
	origTicker := newTickerFn
	defer func() {
		newProxyFn = origNewProxy
		proxyStartRunnerFn = origRunner
		proxyHasListenerFn = origHasListener
		proxyStartTimeout = origTimeout
		timeAfterFn = origAfter
		newTickerFn = origTicker
	}()

	block := make(chan struct{})
	newProxyFn = func(*config.Config) *proxy.Proxy { return &proxy.Proxy{} }
	proxyStartRunnerFn = func(*proxy.Proxy) error {
		<-block
		return nil
	}
	proxyHasListenerFn = func(*proxy.Proxy) bool { return true }
	proxyStartTimeout = 20 * time.Millisecond
	timeAfterFn = func(time.Duration) <-chan time.Time {
		return time.After(20 * time.Millisecond)
	}
	newTickerFn = func(time.Duration) *time.Ticker {
		return time.NewTicker(time.Hour)
	}

	shutdown, err := startProxyFn(config.Defaults())
	close(block)
	if err != nil {
		t.Fatalf("expected deadline success with listener, got %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected shutdown function on deadline success path")
	}
}
