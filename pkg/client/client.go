// Package client 提供 proxyhub 的 Go SDK
//
// 三种使用方式：
//
//  1. 最简：把 proxyhub 当 HTTP 前向代理用（零侵入）
//     client := client.NewHTTPClient("http://localhost:7000", nil)
//     resp, _ := client.Get("https://api.example.com")
//
//  2. 指定偏好（通过 header 传递给 proxyhub）
//     client := client.NewHTTPClient("http://localhost:7000", &client.PickOpts{
//         Country: "CN", PreferAsian: true,
//     })
//
//  3. REST API：自己管理代理生命周期
//     api := client.NewAPI("http://localhost:7001")
//     pr, _ := api.Pick(ctx, &PickOpts{Country: "CN"})
//     // 自己用 pr.URL 发请求
//     api.Report(ctx, pr.URL, true, 234*time.Millisecond)
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// PickOpts 代理偏好（与服务端 pool.PickOpts 对应）
type PickOpts struct {
	Country     string
	Protocol    string // http / https / socks4 / socks5
	PreferAsian bool
	HTTPSOnly   bool
	TopN        int
}

// Proxy REST API 返回的代理信息
type Proxy struct {
	URL          string  `json:"url"`
	Protocol     string  `json:"protocol"`
	Country      string  `json:"country"`
	Anonymity    string  `json:"anonymity"`
	Source       string  `json:"source"`
	Score        float64 `json:"score"`
	SuccessRate  float64 `json:"success_rate"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	IsBanned     bool    `json:"is_banned"`
}

// Stats 池统计
type Stats struct {
	Total      int            `json:"total"`
	Available  int            `json:"available"`
	Banned     int            `json:"banned"`
	ByCountry  map[string]int `json:"by_country"`
	ByProtocol map[string]int `json:"by_protocol"`
	AvgScore   float64        `json:"avg_score"`
	AvgLatency float64        `json:"avg_latency_ms"`
}

// ============================================================
// 方式 1+2: HTTP 前向代理
// ============================================================

// NewHTTPClient 返回一个走 proxyhub 前向代理的 http.Client
//
// 所有请求都会发到 proxyhub，由 proxyhub 内部挑代理。
// opts 通过 header 传递偏好（X-Proxyhub-*）。
//
// 如果 proxyhub 不可用，会返回错误（不会自动降级）。
func NewHTTPClient(proxyhubURL string, opts *PickOpts) *http.Client {
	u, err := url.Parse(proxyhubURL)
	if err != nil {
		return &http.Client{Timeout: 30 * time.Second}
	}
	base := &http.Transport{
		Proxy:                 http.ProxyURL(u),
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	var rt http.RoundTripper = base
	if opts != nil {
		rt = &headerInjectTransport{base: base, opts: *opts}
	}
	return &http.Client{
		Transport: rt,
		Timeout:   45 * time.Second,
	}
}

// headerInjectTransport 把 PickOpts 写进 X-Proxyhub-* header
type headerInjectTransport struct {
	base http.RoundTripper
	opts PickOpts
}

func (t *headerInjectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.opts.Country != "" {
		req.Header.Set("X-Proxyhub-Country", t.opts.Country)
	}
	if t.opts.Protocol != "" {
		req.Header.Set("X-Proxyhub-Protocol", t.opts.Protocol)
	}
	if t.opts.PreferAsian {
		req.Header.Set("X-Proxyhub-Prefer-Asian", "true")
	}
	if t.opts.HTTPSOnly {
		req.Header.Set("X-Proxyhub-HTTPS-Only", "true")
	}
	if t.opts.TopN > 0 {
		req.Header.Set("X-Proxyhub-Top-N", strconv.Itoa(t.opts.TopN))
	}
	return t.base.RoundTrip(req)
}

// ============================================================
// 方式 3: REST API 客户端
// ============================================================

// API proxyhub REST API 客户端
type API struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewAPI 创建 REST API 客户端
func NewAPI(baseURL string) *API {
	return &API{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Pick 挑一个代理
func (a *API) Pick(ctx context.Context, opts *PickOpts) (*Proxy, error) {
	q := url.Values{}
	if opts != nil {
		if opts.Country != "" {
			q.Set("country", opts.Country)
		}
		if opts.Protocol != "" {
			q.Set("protocol", opts.Protocol)
		}
		if opts.PreferAsian {
			q.Set("prefer_asian", "true")
		}
		if opts.HTTPSOnly {
			q.Set("https_only", "true")
		}
		if opts.TopN > 0 {
			q.Set("top_n", strconv.Itoa(opts.TopN))
		}
	}
	reqURL := fmt.Sprintf("%s/api/v1/pick?%s", a.BaseURL, q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("pick failed: %d %s", resp.StatusCode, string(body))
	}
	var p Proxy
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Report 上报代理使用结果
func (a *API) Report(ctx context.Context, proxyURL string, success bool, latency time.Duration) error {
	body, _ := json.Marshal(map[string]any{
		"proxy":      proxyURL,
		"success":    success,
		"latency_ms": latency.Milliseconds(),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.BaseURL+"/api/v1/report", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("report failed: %d %s", resp.StatusCode, string(b))
	}
	return nil
}

// Stats 获取池统计
func (a *API) Stats(ctx context.Context) (*Stats, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.BaseURL+"/api/v1/stats", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stats failed: %d", resp.StatusCode)
	}
	var s Stats
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Refresh 触发立即刷新
func (a *API) Refresh(ctx context.Context) (added int, total int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+"/api/v1/refresh", nil)
	if err != nil {
		return 0, 0, err
	}
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	var out struct {
		Added int `json:"added"`
		Total int `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, 0, err
	}
	return out.Added, out.Total, nil
}
