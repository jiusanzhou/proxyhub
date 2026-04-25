package pool

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestCheckerProbeAlive(t *testing.T) {
	// 起个本地 TCP listener 作为假代理
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	p := New()
	pr := &Proxy{
		URL:      "http://" + ln.Addr().String(),
		Host:     "127.0.0.1",
		Port:     ln.Addr().(*net.TCPAddr).Port,
		Protocol: ProtoHTTP,
	}
	p.mu.Lock()
	p.proxies[pr.URL] = pr
	p.mu.Unlock()

	c := NewChecker(p, CheckerConfig{
		Enabled:     true,
		DialTimeout: 2 * time.Second,
		Concurrency: 1,
		BanOnFail:   3,
	}, nil)

	ctx := context.Background()
	ok := c.probe(ctx, pr)
	if !ok {
		t.Error("probe should succeed on local listener")
	}
	if pr.SuccessCount() != 1 {
		t.Errorf("want success=1, got %d", pr.SuccessCount())
	}
	if pr.IsBanned() {
		t.Error("should not be banned")
	}
}

func TestCheckerProbeDead(t *testing.T) {
	// 用一个肯定不通的端口
	p := New(WithFailCooldown(1 * time.Minute))
	pr := &Proxy{
		URL:      "http://127.0.0.1:1", // port 1 几乎不可能开
		Host:     "127.0.0.1",
		Port:     1,
		Protocol: ProtoHTTP,
	}
	p.mu.Lock()
	p.proxies[pr.URL] = pr
	p.mu.Unlock()

	c := NewChecker(p, CheckerConfig{
		Enabled:     true,
		DialTimeout: 500 * time.Millisecond,
		Concurrency: 1,
		BanOnFail:   2,
	}, nil)

	ctx := context.Background()

	// 第 1 次失败：不 ban
	if c.probe(ctx, pr) {
		t.Error("probe should fail")
	}
	if pr.IsBanned() {
		t.Error("should NOT be banned on first fail")
	}

	// 第 2 次失败：达到 BanOnFail，应该 ban
	if c.probe(ctx, pr) {
		t.Error("probe should fail")
	}
	if !pr.IsBanned() {
		t.Error("should be banned after BanOnFail")
	}
}
