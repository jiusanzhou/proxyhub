// Package pool: health checker
//
// 主动健康探测：
//  1. L4 TCP dial 到代理端口（所有协议）
//  2. L7 HTTP CONNECT 探测到目标 host（仅 http/https 代理）
//
// 分轮次滚动探测，避免同时打爆所有代理。
// 每个代理每 N 秒（默认 60s/轮，全池 N 分钟一轮）被探测一次。
package pool

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// CheckerConfig 健康探测配置
type CheckerConfig struct {
	// Enabled 启用健康探测
	Enabled bool

	// Interval 整轮探测间隔（默认 60s，所有代理在此时长内各被探测一次）
	Interval time.Duration

	// DialTimeout TCP 连接超时（默认 5s）
	DialTimeout time.Duration

	// HTTPTimeout HTTP CONNECT 探测超时（默认 8s）
	HTTPTimeout time.Duration

	// Concurrency 同时探测的代理数（默认 50）
	Concurrency int

	// TargetHost L7 探测目标（默认 httpbin.org:80）
	// 只做 L4 时不会访问这里
	TargetHost string

	// EnableL7 是否做 L7 HTTP CONNECT 探测（仅 http/https 代理有效）
	// 关闭时只做 L4 dial
	EnableL7 bool

	// BanOnFail 连续失败多少次才标记 banned（默认 3）
	BanOnFail int
}

// DefaultCheckerConfig 默认配置
func DefaultCheckerConfig() CheckerConfig {
	return CheckerConfig{
		Enabled:     true,
		Interval:    60 * time.Second,
		DialTimeout: 5 * time.Second,
		HTTPTimeout: 8 * time.Second,
		Concurrency: 50,
		TargetHost:  "httpbin.org:80",
		EnableL7:    false, // 默认关闭 L7，减少对 target 的压力
		BanOnFail:   3,
	}
}

// Checker 健康探测器
type Checker struct {
	pool   *Pool
	cfg    CheckerConfig
	logger *slog.Logger

	// 连续失败计数（URL -> count），非 atomic，探测串行化
	failStreakMu sync.Mutex
	failStreak   map[string]int

	// 统计
	probes  int64
	success int64
	failed  int64
}

// NewChecker 创建探测器
func NewChecker(p *Pool, cfg CheckerConfig, logger *slog.Logger) *Checker {
	if cfg.Interval <= 0 {
		cfg.Interval = 60 * time.Second
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 5 * time.Second
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 8 * time.Second
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 50
	}
	if cfg.BanOnFail <= 0 {
		cfg.BanOnFail = 3
	}
	if cfg.TargetHost == "" {
		cfg.TargetHost = "httpbin.org:80"
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Checker{
		pool:       p,
		cfg:        cfg,
		logger:     logger,
		failStreak: make(map[string]int),
	}
}

// Run 启动后台探测循环（阻塞直到 ctx 取消）
func (c *Checker) Run(ctx context.Context) {
	if !c.cfg.Enabled {
		return
	}
	c.logger.Info("health checker started",
		"interval", c.cfg.Interval,
		"concurrency", c.cfg.Concurrency,
		"l7", c.cfg.EnableL7,
	)

	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()

	// 启动后延迟 5s 开始首轮（等 pool 刷一次）
	firstRun := time.NewTimer(5 * time.Second)
	defer firstRun.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-firstRun.C:
			c.runOnce(ctx)
		case <-ticker.C:
			c.runOnce(ctx)
		}
	}
}

// runOnce 对当前池内所有代理探测一轮
func (c *Checker) runOnce(ctx context.Context) {
	all := c.pool.All()
	if len(all) == 0 {
		return
	}

	start := time.Now()
	sem := make(chan struct{}, c.cfg.Concurrency)
	var wg sync.WaitGroup
	var okCount, failCount int64
	var mu sync.Mutex

	for _, pr := range all {
		select {
		case <-ctx.Done():
			return
		default:
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(p *Proxy) {
			defer wg.Done()
			defer func() { <-sem }()
			ok := c.probe(ctx, p)
			mu.Lock()
			if ok {
				okCount++
			} else {
				failCount++
			}
			mu.Unlock()
		}(pr)
	}
	wg.Wait()

	c.probes += int64(len(all))
	c.success += okCount
	c.failed += failCount
	c.logger.Info("health check round done",
		"total", len(all),
		"ok", okCount,
		"fail", failCount,
		"elapsed", time.Since(start),
	)
}

// probe 探测单个代理；返回是否健康
//
// 成功：更新延迟 + 重置 failStreak
// 失败：+1 failStreak；达到 BanOnFail 时 ban
func (c *Checker) probe(ctx context.Context, p *Proxy) bool {
	latency, err := c.doProbe(ctx, p)
	if err == nil {
		// 成功
		p.RecordSuccess(latency)
		c.resetFailStreak(p.URL)
		return true
	}

	// 失败
	streak := c.incrFailStreak(p.URL)
	if streak >= c.cfg.BanOnFail {
		p.RecordFail(c.pool.FailCooldown())
		c.logger.Debug("proxy banned by checker",
			"proxy", p.URL, "streak", streak, "err", err)
	} else {
		// 只累计失败，不 ban（避免偶发网络抖动）
		// 用 totalReq/failCount 但不设 bannedUntil
		// 手动更新（因为 RecordFail 会 ban）
		p.totalReq.Add(1)
		p.failCount.Add(1)
	}
	return false
}

// doProbe 执行实际探测
// 1. L4 TCP dial
// 2. (可选) L7 HTTP CONNECT
func (c *Checker) doProbe(ctx context.Context, p *Proxy) (time.Duration, error) {
	proxyURL, err := url.Parse(p.URL)
	if err != nil {
		return 0, fmt.Errorf("parse url: %w", err)
	}
	proxyAddr := proxyURL.Host
	if proxyAddr == "" {
		proxyAddr = fmt.Sprintf("%s:%d", p.Host, p.Port)
	}

	start := time.Now()

	// L4 TCP dial
	dialer := &net.Dialer{Timeout: c.cfg.DialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return 0, fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	// 只做 L4，或非 http/https 协议 → 这里结束
	if !c.cfg.EnableL7 || (p.Protocol != ProtoHTTP && p.Protocol != ProtoHTTPS) {
		return time.Since(start), nil
	}

	// L7: HTTP CONNECT 到 TargetHost
	conn.SetDeadline(time.Now().Add(c.cfg.HTTPTimeout))
	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n",
		c.cfg.TargetHost, c.cfg.TargetHost)
	if _, err := conn.Write([]byte(req)); err != nil {
		return 0, fmt.Errorf("connect write: %w", err)
	}

	// 读响应行
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		return 0, fmt.Errorf("connect read: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("connect status %d", resp.StatusCode)
	}

	return time.Since(start), nil
}

func (c *Checker) incrFailStreak(url string) int {
	c.failStreakMu.Lock()
	defer c.failStreakMu.Unlock()
	c.failStreak[url]++
	return c.failStreak[url]
}

func (c *Checker) resetFailStreak(url string) {
	c.failStreakMu.Lock()
	defer c.failStreakMu.Unlock()
	delete(c.failStreak, url)
}

// CheckerStats 导出探测器统计
type CheckerStats struct {
	TotalProbes int64 `json:"total_probes"`
	Success     int64 `json:"success"`
	Failed      int64 `json:"failed"`
}

// Stats 返回探测器统计
func (c *Checker) Stats() CheckerStats {
	return CheckerStats{
		TotalProbes: c.probes,
		Success:     c.success,
		Failed:      c.failed,
	}
}
