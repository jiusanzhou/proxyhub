// Package server 提供 HTTP 前向代理 + REST API
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/jiusanzhou/proxyhub/internal/pool"
)

// Server proxyhub 服务
type Server struct {
	pool *pool.Pool

	// 启动时记录，给 /healthz 用
	startedAt time.Time

	// 累计请求数（前向代理 + API 各算）
	proxyReqCount atomic.Int64
	apiReqCount   atomic.Int64
}

// New 创建 server
func New(p *pool.Pool) *Server {
	return &Server{
		pool:      p,
		startedAt: time.Now(),
	}
}

// HTTPHandler 返回 HTTP REST API mux（部署在 --api-port）
func (s *Server) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/v1/pick", s.handlePick)
	mux.HandleFunc("/api/v1/report", s.handleReport)
	mux.HandleFunc("/api/v1/stats", s.handleStats)
	mux.HandleFunc("/api/v1/proxies", s.handleList)
	mux.HandleFunc("/api/v1/refresh", s.handleRefresh)
	return mux
}

// ProxyHandler 返回 HTTP 前向代理 handler（部署在 --proxy-port）
//
// 客户端用法：
//
//	curl -x http://localhost:7000 https://example.com
//
// 内部自动挑代理 + 重试。
func (s *Server) ProxyHandler() http.Handler {
	return http.HandlerFunc(s.handleForwardProxy)
}

// ============================================================
// REST API
// ============================================================

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	stats := s.pool.Stats()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"uptime":      time.Since(s.startedAt).String(),
		"pool_size":   stats.Total,
		"available":   stats.Available,
		"proxy_reqs":  s.proxyReqCount.Load(),
		"api_reqs":    s.apiReqCount.Load(),
	})
}

func (s *Server) handlePick(w http.ResponseWriter, r *http.Request) {
	s.apiReqCount.Add(1)
	q := r.URL.Query()
	opts := pool.PickOpts{
		Country:     q.Get("country"),
		Protocol:    pool.Protocol(q.Get("protocol")),
		PreferAsian: q.Get("prefer_asian") == "true" || q.Get("prefer_asian") == "1",
		HTTPSOnly:   q.Get("https_only") == "true" || q.Get("https_only") == "1",
	}
	if topN := q.Get("top_n"); topN != "" {
		if n, err := strconv.Atoi(topN); err == nil {
			opts.TopN = n
		}
	}
	pr := s.pool.Pick(opts)
	if pr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "no proxy available"})
		return
	}
	writeJSON(w, http.StatusOK, proxyToJSON(pr))
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	s.apiReqCount.Add(1)
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Proxy     string `json:"proxy"`
		Success   bool   `json:"success"`
		LatencyMs int64  `json:"latency_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pr := s.pool.Get(body.Proxy)
	if pr == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "proxy not found"})
		return
	}
	if body.Success {
		pr.RecordSuccess(time.Duration(body.LatencyMs) * time.Millisecond)
	} else {
		pr.RecordFail(s.pool.FailCooldown())
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.apiReqCount.Add(1)
	writeJSON(w, http.StatusOK, s.pool.Stats())
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	s.apiReqCount.Add(1)
	q := r.URL.Query()
	all := s.pool.All()

	// 过滤
	if c := q.Get("country"); c != "" {
		filtered := make([]*pool.Proxy, 0, len(all))
		for _, p := range all {
			if p.Country == c {
				filtered = append(filtered, p)
			}
		}
		all = filtered
	}
	if onlyAvailable := q.Get("available") == "true"; onlyAvailable {
		filtered := make([]*pool.Proxy, 0, len(all))
		for _, p := range all {
			if !p.IsBanned() {
				filtered = append(filtered, p)
			}
		}
		all = filtered
	}

	// 排序：默认 score 降序
	switch q.Get("sort") {
	case "latency":
		sort.Slice(all, func(i, j int) bool { return all[i].AvgLatencyMs() < all[j].AvgLatencyMs() })
	case "country":
		sort.Slice(all, func(i, j int) bool { return all[i].Country < all[j].Country })
	default:
		sort.Slice(all, func(i, j int) bool { return all[i].Score() > all[j].Score() })
	}

	// 分页
	limit := 100
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	if len(all) > limit {
		all = all[:limit]
	}

	out := make([]map[string]any, 0, len(all))
	for _, p := range all {
		out = append(out, proxyToJSON(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"proxies": out, "count": len(out)})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	s.apiReqCount.Add(1)
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	n, err := s.pool.Refresh(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "added": n})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": n, "total": s.pool.Size()})
}

// ============================================================
// HTTP 前向代理
// ============================================================

func (s *Server) handleForwardProxy(w http.ResponseWriter, r *http.Request) {
	s.proxyReqCount.Add(1)

	// 从 header / 查询参数读取偏好
	opts := pool.PickOpts{
		Country:     r.Header.Get("X-Proxyhub-Country"),
		Protocol:    pool.Protocol(r.Header.Get("X-Proxyhub-Protocol")),
		PreferAsian: r.Header.Get("X-Proxyhub-Prefer-Asian") == "true",
		HTTPSOnly:   r.Header.Get("X-Proxyhub-HTTPS-Only") == "true",
	}
	if topN := r.Header.Get("X-Proxyhub-Top-N"); topN != "" {
		if n, err := strconv.Atoi(topN); err == nil {
			opts.TopN = n
		}
	}

	if r.Method == http.MethodConnect {
		s.handleConnect(w, r, opts)
		return
	}
	s.handleHTTPProxy(w, r, opts)
}

// handleHTTPProxy 处理普通 HTTP 请求（非 CONNECT）
func (s *Server) handleHTTPProxy(w http.ResponseWriter, r *http.Request, opts pool.PickOpts) {
	// 直接复用 pool.HTTPClient 的 RoundTripper
	client := s.pool.HTTPClient(opts)

	// 清理 hop-by-hop headers
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, r.URL.String(), r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	copyHeader(outReq.Header, r.Header)
	removeHopHeaders(outReq.Header)

	resp, err := client.Do(outReq)
	if err != nil {
		slog.Warn("forward proxy failed", "url", r.URL.String(), "err", err)
		http.Error(w, "proxy upstream error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyHeader(w.Header(), resp.Header)
	removeHopHeaders(w.Header())
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handleConnect 处理 HTTPS CONNECT 隧道
func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request, opts pool.PickOpts) {
	// HTTPS CONNECT 只支持 HTTP/HTTPS 代理（socks 暂不实现）
	// 默认优先 HTTP/HTTPS 代理（socks4/5 跳过）
	if opts.Protocol == "" {
		opts.Protocol = pool.ProtoHTTP // HTTP 代理通常也支持 CONNECT
	}

	const maxRetries = 15 // 免费代理质量差，给更多重试机会
	var (
		upstream net.Conn
		pickedPr *pool.Proxy
		lastErr  error
	)
	for attempt := 0; attempt < maxRetries; attempt++ {
		pr := s.pool.Pick(opts)
		if pr == nil {
			// 切换协议再试
			if opts.Protocol == pool.ProtoHTTP {
				opts.Protocol = pool.ProtoHTTPS
				continue
			}
			break
		}
		// 跳过不支持的协议（socks 类）
		if pr.Protocol == pool.ProtoSOCKS4 || pr.Protocol == pool.ProtoSOCKS5 {
			continue
		}
		proxyURL, err := url.Parse(pr.URL)
		if err != nil {
			pr.RecordFail(s.pool.FailCooldown())
			lastErr = err
			continue
		}
		start := time.Now()
		conn, err := dialThroughProxy(proxyURL, r.Host)
		if err != nil {
			pr.RecordFail(s.pool.FailCooldown())
			lastErr = err
			slog.Debug("CONNECT attempt failed", "proxy", pr.URL, "host", r.Host, "err", err)
			continue
		}
		pr.RecordSuccess(time.Since(start))
		upstream = conn
		pickedPr = pr
		slog.Debug("CONNECT ok", "proxy", pr.URL, "host", r.Host, "attempt", attempt+1)
		break
	}
	if upstream == nil {
		if lastErr == nil {
			lastErr = fmt.Errorf("no suitable proxy")
		}
		slog.Warn("CONNECT failed after retries", "host", r.Host, "err", lastErr, "retries", maxRetries)
		http.Error(w, "upstream error: "+lastErr.Error(), http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	_ = pickedPr

	// 劫持本地连接
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// 200 Connection Established
	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}
	// 双向转发
	go func() { _, _ = io.Copy(upstream, clientConn) }()
	_, _ = io.Copy(clientConn, upstream)
}

// dialThroughProxy 通过代理连接到目标 host:port
func dialThroughProxy(proxyURL *url.URL, target string) (net.Conn, error) {
	switch proxyURL.Scheme {
	case "http", "https":
		// 通过 HTTP CONNECT 通过代理建立隧道
		conn, err := net.DialTimeout("tcp", proxyURL.Host, 10*time.Second)
		if err != nil {
			return nil, err
		}
		req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
		if _, err := conn.Write([]byte(req)); err != nil {
			conn.Close()
			return nil, err
		}
		// 读 200 响应
		buf := make([]byte, 4096)
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		n, err := conn.Read(buf)
		conn.SetReadDeadline(time.Time{})
		if err != nil {
			conn.Close()
			return nil, err
		}
		response := string(buf[:n])
		if !contains200(response) {
			conn.Close()
			return nil, fmt.Errorf("proxy CONNECT failed: %s", firstLine(response))
		}
		return conn, nil
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
	}
}

func contains200(resp string) bool {
	return len(resp) >= 12 && (resp[9:12] == "200")
}

func firstLine(s string) string {
	for i, c := range s {
		if c == '\r' || c == '\n' {
			return s[:i]
		}
	}
	if len(s) > 100 {
		return s[:100]
	}
	return s
}

// ============================================================
// utils
// ============================================================

var hopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func removeHopHeaders(h http.Header) {
	for _, hh := range hopHeaders {
		h.Del(hh)
	}
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(body)
}

func proxyToJSON(p *pool.Proxy) map[string]any {
	return map[string]any{
		"url":            p.URL,
		"protocol":       p.Protocol,
		"country":        p.Country,
		"anonymity":      p.Anonymity,
		"source":         p.Source,
		"score":          p.Score(),
		"success_rate":   p.SuccessRate(),
		"avg_latency_ms": p.AvgLatencyMs(),
		"total_requests": p.TotalRequests(),
		"success_count":  p.SuccessCount(),
		"fail_count":     p.FailCount(),
		"is_banned":      p.IsBanned(),
	}
}
