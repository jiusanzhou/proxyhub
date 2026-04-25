package pool

import (
	"testing"
	"time"
)

func TestProxyScore(t *testing.T) {
	p := &Proxy{Protocol: ProtoHTTPS, Country: "CN"}
	p.totalReq.Store(100)
	p.successCount.Store(80)
	p.failCount.Store(20)
	p.avgLatencyMs.Store(int64(500 * 1000))

	if got := p.SuccessRate(); got != 0.8 {
		t.Errorf("success rate = %v, want 0.8", got)
	}
	if got := p.AvgLatencyMs(); got != 500.0 {
		t.Errorf("avg latency = %v, want 500", got)
	}
	if p.IsBanned() {
		t.Error("should not be banned")
	}
}

func TestProxyBanned(t *testing.T) {
	p := &Proxy{Protocol: ProtoHTTPS}
	p.RecordFail(10 * time.Minute)
	if !p.IsBanned() {
		t.Error("should be banned")
	}
}

func TestSuccessUnbans(t *testing.T) {
	p := &Proxy{Protocol: ProtoHTTPS}
	p.RecordFail(10 * time.Minute)
	p.RecordSuccess(200 * time.Millisecond)
	if p.IsBanned() {
		t.Error("success should unban")
	}
}

func TestPoolPick(t *testing.T) {
	p := New()
	p.proxies["http://1.1.1.1:80"] = &Proxy{
		URL:      "http://1.1.1.1:80",
		Protocol: ProtoHTTP,
		Country:  "CN",
	}
	p.proxies["http://2.2.2.2:80"] = &Proxy{
		URL:      "http://2.2.2.2:80",
		Protocol: ProtoHTTP,
		Country:  "US",
	}

	pr := p.Pick(PickOpts{Country: "CN"})
	if pr == nil || pr.Country != "CN" {
		t.Errorf("want CN proxy, got %v", pr)
	}
	pr = p.Pick(PickOpts{Country: "DE"})
	if pr != nil {
		t.Errorf("DE not in pool, want nil, got %v", pr)
	}
}

func TestPoolStats(t *testing.T) {
	p := New()
	p.proxies["http://1.1.1.1:80"] = &Proxy{Protocol: ProtoHTTP, Country: "CN"}
	p.proxies["http://2.2.2.2:80"] = &Proxy{Protocol: ProtoHTTPS, Country: "US"}

	s := p.Stats()
	if s.Total != 2 {
		t.Errorf("total = %d, want 2", s.Total)
	}
	if s.ByCountry["CN"] != 1 || s.ByCountry["US"] != 1 {
		t.Errorf("by_country = %v", s.ByCountry)
	}
}
