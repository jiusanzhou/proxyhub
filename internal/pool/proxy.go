// Package pool 代理池核心：内存代理池 + 健康度评分 + 智能挑选。
package pool

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Protocol 代理协议
type Protocol string

const (
	ProtoHTTP   Protocol = "http"
	ProtoHTTPS  Protocol = "https"
	ProtoSOCKS4 Protocol = "socks4"
	ProtoSOCKS5 Protocol = "socks5"
)

// Anonymity 代理匿名度
type Anonymity string

const (
	AnonTransparent Anonymity = "transparent"
	AnonAnonymous   Anonymity = "anonymous"
	AnonElite       Anonymity = "elite"
	AnonUnknown     Anonymity = "unknown"
)

// Proxy 单个代理记录（内存态）
type Proxy struct {
	URL       string
	Host      string
	Port      int
	Protocol  Protocol
	Country   string
	Anonymity Anonymity
	Source    string

	// 健康度（用 atomic 避免锁）
	totalReq      atomic.Int64
	successCount  atomic.Int64
	failCount     atomic.Int64
	lastLatencyMs atomic.Int64
	avgLatencyMs  atomic.Int64 // 滚动平均 * 1000 保留精度
	bannedUntil   atomic.Int64 // UnixNano
	lastUsedAt    atomic.Int64 // UnixNano

	mu sync.RWMutex
}

// SuccessRate 成功率
func (p *Proxy) SuccessRate() float64 {
	total := p.totalReq.Load()
	if total == 0 {
		return 1.0
	}
	success := p.successCount.Load()
	return float64(success) / float64(total)
}

// AvgLatencyMs 平均延迟（毫秒）
func (p *Proxy) AvgLatencyMs() float64 {
	return float64(p.avgLatencyMs.Load()) / 1000.0
}

// IsBanned 是否被封禁
func (p *Proxy) IsBanned() bool {
	until := p.bannedUntil.Load()
	if until == 0 {
		return false
	}
	return time.Now().UnixNano() < until
}

// RecordSuccess 记录成功
func (p *Proxy) RecordSuccess(latency time.Duration) {
	p.totalReq.Add(1)
	p.successCount.Add(1)
	latMs := latency.Milliseconds()
	p.lastLatencyMs.Store(latMs)
	old := p.avgLatencyMs.Load()
	if old == 0 {
		p.avgLatencyMs.Store(latMs * 1000)
	} else {
		newAvg := old*800/1000 + latMs*200
		p.avgLatencyMs.Store(newAvg)
	}
	p.lastUsedAt.Store(time.Now().UnixNano())
	p.bannedUntil.Store(0)
}

// RecordFail 记录失败
func (p *Proxy) RecordFail(cooldown time.Duration) {
	p.totalReq.Add(1)
	p.failCount.Add(1)
	p.lastUsedAt.Store(time.Now().UnixNano())
	if cooldown > 0 {
		p.bannedUntil.Store(time.Now().Add(cooldown).UnixNano())
	}
}

// Score 综合评分
func (p *Proxy) Score() float64 {
	rate := p.SuccessRate()
	avg := p.AvgLatencyMs()
	if avg <= 0 {
		avg = 5000
	}
	latScore := 1.0 / (1.0 + avg/1000.0)
	return rate*0.6 + latScore*0.4
}

// asianCountries 亚洲代理（A 股等场景优先）
var asianCountries = map[string]bool{
	"CN": true, "HK": true, "TW": true,
	"JP": true, "KR": true, "SG": true,
	"MY": true, "TH": true, "VN": true, "ID": true,
	"PH": true, "IN": true,
}

// IsAsian 是否亚洲代理
func (p *Proxy) IsAsian() bool {
	return asianCountries[strings.ToUpper(p.Country)]
}

// String 调试输出
func (p *Proxy) String() string {
	return fmt.Sprintf("%s [%s/%s] success=%.1f%% lat=%.0fms",
		p.URL, p.Country, p.Protocol, p.SuccessRate()*100, p.AvgLatencyMs())
}

// Getter 接口（暴露原子字段）
func (p *Proxy) TotalRequests() int64 { return p.totalReq.Load() }
func (p *Proxy) SuccessCount() int64  { return p.successCount.Load() }
func (p *Proxy) FailCount() int64     { return p.failCount.Load() }
func (p *Proxy) LastLatencyMs() int64 { return p.lastLatencyMs.Load() }
func (p *Proxy) BannedUntil() int64   { return p.bannedUntil.Load() }
func (p *Proxy) LastUsedAt() int64    { return p.lastUsedAt.Load() }

// Setter 用于从 store 恢复状态
func (p *Proxy) SetStats(totalReq, successCount, failCount, avgLatencyMsX1000 int64) {
	p.totalReq.Store(totalReq)
	p.successCount.Store(successCount)
	p.failCount.Store(failCount)
	p.avgLatencyMs.Store(avgLatencyMsX1000)
}
func (p *Proxy) SetBannedUntil(ns int64) { p.bannedUntil.Store(ns) }
func (p *Proxy) SetLastUsedAt(ns int64)  { p.lastUsedAt.Store(ns) }
