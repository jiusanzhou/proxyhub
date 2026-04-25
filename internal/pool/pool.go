// Package pool 代理池管理
package pool

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"
)

// Source 代理来源（本包内定义接口，避免包循环引用）
type Source interface {
	Name() string
	Fetch(ctx context.Context) ([]*Proxy, error)
}

// Store 代理健康度持久化接口
type Store interface {
	Save(ctx context.Context, proxies []*Proxy) error
	LoadAll(ctx context.Context) ([]*Proxy, error)
}

// PickOpts 挑选代理的偏好
type PickOpts struct {
	Country     string
	Protocol    Protocol
	PreferAsian bool
	HTTPSOnly   bool
	TopN        int
}

// Pool 代理池
type Pool struct {
	mu              sync.RWMutex
	proxies         map[string]*Proxy
	sources         []Source
	refreshInterval time.Duration
	failCooldown    time.Duration
	store           Store
	logger          *slog.Logger
}

// Option 选项
type Option func(*Pool)

// WithSources 设置代理来源
func WithSources(sources ...Source) Option {
	return func(p *Pool) { p.sources = sources }
}

// WithRefreshInterval 自动刷新间隔（默认 10min）
func WithRefreshInterval(d time.Duration) Option {
	return func(p *Pool) { p.refreshInterval = d }
}

// WithFailCooldown 单代理失败后冷却时间（默认 5min）
func WithFailCooldown(d time.Duration) Option {
	return func(p *Pool) { p.failCooldown = d }
}

// WithStore 持久化存储
func WithStore(s Store) Option {
	return func(p *Pool) { p.store = s }
}

// WithLogger 日志
func WithLogger(l *slog.Logger) Option {
	return func(p *Pool) { p.logger = l }
}

// New 创建代理池
func New(opts ...Option) *Pool {
	p := &Pool{
		proxies:         make(map[string]*Proxy),
		refreshInterval: 10 * time.Minute,
		failCooldown:    5 * time.Minute,
		logger:          slog.Default(),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Refresh 立即从所有 source 拉取代理（聚合 + 去重）
func (p *Pool) Refresh(ctx context.Context) (int, error) {
	if len(p.sources) == 0 {
		return 0, fmt.Errorf("no sources configured")
	}

	var (
		all  []*Proxy
		errs []error
		wg   sync.WaitGroup
		mu   sync.Mutex
	)
	for _, src := range p.sources {
		wg.Add(1)
		go func(s Source) {
			defer wg.Done()
			items, err := s.Fetch(ctx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", s.Name(), err))
				p.logger.Warn("proxy source failed", "source", s.Name(), "err", err)
				return
			}
			all = append(all, items...)
			p.logger.Info("proxy source fetched", "source", s.Name(), "count", len(items))
		}(src)
	}
	wg.Wait()

	// 从 store 加载历史健康度
	historical := map[string]*Proxy{}
	if p.store != nil {
		if loaded, err := p.store.LoadAll(ctx); err == nil {
			for _, hp := range loaded {
				historical[hp.URL] = hp
			}
		}
	}

	added := 0
	p.mu.Lock()
	defer p.mu.Unlock()
	seen := map[string]bool{}
	for _, np := range all {
		if seen[np.URL] {
			continue
		}
		seen[np.URL] = true

		if existing, ok := p.proxies[np.URL]; ok {
			existing.Country = np.Country
			existing.Protocol = np.Protocol
			existing.Anonymity = np.Anonymity
			continue
		}
		if h, ok := historical[np.URL]; ok {
			np.SetStats(h.TotalRequests(), h.SuccessCount(), h.FailCount(), int64(h.AvgLatencyMs()*1000))
			np.SetLastUsedAt(h.LastUsedAt())
		}
		p.proxies[np.URL] = np
		added++
	}

	if len(errs) == len(p.sources) {
		return added, fmt.Errorf("all sources failed")
	}
	return added, nil
}

// Start 启动后台定时刷新
func (p *Pool) Start(ctx context.Context) error {
	if _, err := p.Refresh(ctx); err != nil {
		p.logger.Warn("initial refresh failed", "err", err)
	}
	ticker := time.NewTicker(p.refreshInterval)
	defer ticker.Stop()
	flushTicker := time.NewTicker(2 * time.Minute)
	defer flushTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			if p.store != nil {
				_ = p.flush(context.Background())
			}
			return ctx.Err()
		case <-ticker.C:
			n, err := p.Refresh(ctx)
			if err != nil {
				p.logger.Warn("proxy refresh failed", "err", err)
			} else {
				p.logger.Info("proxy refresh ok", "added", n, "total", p.Size())
			}
		case <-flushTicker.C:
			if err := p.flush(ctx); err != nil {
				p.logger.Warn("proxy flush failed", "err", err)
			}
		}
	}
}

// flush 把当前代理状态写入 store
func (p *Pool) flush(ctx context.Context) error {
	if p.store == nil {
		return nil
	}
	p.mu.RLock()
	list := make([]*Proxy, 0, len(p.proxies))
	for _, pr := range p.proxies {
		list = append(list, pr)
	}
	p.mu.RUnlock()
	return p.store.Save(ctx, list)
}

// Flush 公开版本
func (p *Pool) Flush(ctx context.Context) error { return p.flush(ctx) }

// FailCooldown 暴露 cooldown 供 RoundTripper 使用
func (p *Pool) FailCooldown() time.Duration { return p.failCooldown }

// Size 当前池大小
func (p *Pool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.proxies)
}

// Pick 挑一个代理
func (p *Pool) Pick(opts PickOpts) *Proxy {
	if opts.TopN <= 0 {
		opts.TopN = 20
	}
	candidates := p.candidates(opts)
	if len(candidates) == 0 {
		return nil
	}
	n := opts.TopN
	if n > len(candidates) {
		n = len(candidates)
	}
	return candidates[rand.IntN(n)]
}

// candidates 按偏好筛选 + 排序
func (p *Pool) candidates(opts PickOpts) []*Proxy {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*Proxy, 0, len(p.proxies))
	for _, pr := range p.proxies {
		if pr.IsBanned() {
			continue
		}
		if opts.Protocol != "" && pr.Protocol != opts.Protocol {
			continue
		}
		if opts.HTTPSOnly && pr.Protocol != ProtoHTTPS {
			continue
		}
		if opts.Country != "" && pr.Country != opts.Country {
			if !(opts.PreferAsian && pr.IsAsian()) {
				continue
			}
		}
		out = append(out, pr)
	}
	sort.Slice(out, func(i, j int) bool {
		if opts.PreferAsian {
			ai, aj := out[i].IsAsian(), out[j].IsAsian()
			if ai != aj {
				return ai
			}
		}
		return out[i].Score() > out[j].Score()
	})
	return out
}

// HTTPClient 返回一个使用代理池的 HTTP client
func (p *Pool) HTTPClient(opts PickOpts) *http.Client {
	tr := &poolTransport{
		pool:    p,
		opts:    opts,
		base:    http.DefaultTransport,
		retries: 3,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   30 * time.Second,
	}
}

// All 返回所有代理（拷贝）
func (p *Pool) All() []*Proxy {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*Proxy, 0, len(p.proxies))
	for _, pr := range p.proxies {
		out = append(out, pr)
	}
	return out
}

// Get 按 URL 获取
func (p *Pool) Get(url string) *Proxy {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.proxies[url]
}

// Stats 池整体统计
type Stats struct {
	Total       int            `json:"total"`
	Available   int            `json:"available"`
	Banned      int            `json:"banned"`
	ByCountry   map[string]int `json:"by_country"`
	ByProtocol  map[string]int `json:"by_protocol"`
	AvgScore    float64        `json:"avg_score"`
	AvgLatency  float64        `json:"avg_latency_ms"`
}

// Stats 返回当前池统计信息
func (p *Pool) Stats() Stats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s := Stats{
		ByCountry:  map[string]int{},
		ByProtocol: map[string]int{},
	}
	var sumScore, sumLatency float64
	var nLatency int
	for _, pr := range p.proxies {
		s.Total++
		if pr.IsBanned() {
			s.Banned++
		} else {
			s.Available++
		}
		s.ByCountry[pr.Country]++
		s.ByProtocol[string(pr.Protocol)]++
		sumScore += pr.Score()
		if lat := pr.AvgLatencyMs(); lat > 0 {
			sumLatency += lat
			nLatency++
		}
	}
	if s.Total > 0 {
		s.AvgScore = sumScore / float64(s.Total)
	}
	if nLatency > 0 {
		s.AvgLatency = sumLatency / float64(nLatency)
	}
	return s
}

// ============================================================
// poolTransport: RoundTripper + 自动轮转 + 重试
// ============================================================

type poolTransport struct {
	pool    *Pool
	opts    PickOpts
	base    http.RoundTripper
	retries int
}

func (t *poolTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < t.retries; attempt++ {
		pr := t.pool.Pick(t.opts)
		if pr == nil {
			// 池空：降级直连
			return t.base.RoundTrip(req)
		}
		proxyURL, err := url.Parse(pr.URL)
		if err != nil {
			pr.RecordFail(t.pool.failCooldown)
			lastErr = err
			continue
		}
		tr := &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			DialContext: (&netDialer{
				timeout: 10 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			DisableKeepAlives:     true,
			ResponseHeaderTimeout: 15 * time.Second,
		}
		reqCopy := req.Clone(req.Context())
		start := time.Now()
		resp, err := tr.RoundTrip(reqCopy)
		latency := time.Since(start)
		if err != nil {
			pr.RecordFail(t.pool.failCooldown)
			lastErr = err
			continue
		}
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			pr.RecordFail(t.pool.failCooldown)
			lastErr = fmt.Errorf("upstream %d", resp.StatusCode)
			continue
		}
		pr.RecordSuccess(latency)
		return resp, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no proxy available")
	}
	return nil, lastErr
}

type netDialer struct {
	timeout time.Duration
}

func (d *netDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: d.timeout, KeepAlive: 30 * time.Second}
	return dialer.DialContext(ctx, network, addr)
}
