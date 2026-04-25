// Package server 提供 HTTP 前向代理 + REST API
package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go.zoe.im/proxyhub/internal/pool"
	"go.zoe.im/proxyhub/internal/session"
)

// Server proxyhub 服务
type Server struct {
	pool         *pool.Pool
	checker      *pool.Checker
	sessionMgr   *session.Manager

	// 启动时记录，给 /healthz 用
	startedAt time.Time

	// 累计请求数（前向代理 + API 各算）
	proxyReqCount atomic.Int64
	apiReqCount   atomic.Int64
}

// New 创建 server
func New(p *pool.Pool) *Server {
	return &Server{
		pool:        p,
		startedAt:   time.Now(),
		sessionMgr:  session.NewManager(10*time.Minute, 3),
	}
}

// SetChecker 注入探测器（可选）
func (s *Server) SetChecker(c *pool.Checker) {
	s.checker = c
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
	mux.HandleFunc("/api/v1/check", s.handleCheckStats)
	mux.HandleFunc("/api/v1/sessions", s.handleSessions)
	mux.HandleFunc("/api/v1/sessions/rotate", s.handleSessionRotate)
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
	resp := map[string]any{
		"status":     "ok",
		"uptime":     time.Since(s.startedAt).String(),
		"pool_size":  stats.Total,
		"available":  stats.Available,
		"sessions":   s.sessionMgr.Size(),
		"proxy_reqs": s.proxyReqCount.Load(),
		"api_reqs":   s.apiReqCount.Load(),
	}
	if s.checker != nil {
		resp["checker"] = s.checker.Stats()
	}
	writeJSON(w, http.StatusOK, resp)
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
	if proto := q.Get("protocol"); proto != "" {
		filtered := make([]*pool.Proxy, 0, len(all))
		want := pool.Protocol(proto)
		for _, p := range all {
			if p.Protocol == want {
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

func (s *Server) handleCheckStats(w http.ResponseWriter, r *http.Request) {
	s.apiReqCount.Add(1)
	if s.checker == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": true,
		"stats":   s.checker.Stats(),
	})
}

// ============================================================
// HTTP 前向代理
// ============================================================

// Response headers written to client on both HTTP and CONNECT paths
const (
	HdrProxy      = "X-Proxyhub-Proxy"       // 使用的代理 URL
	HdrCountry    = "X-Proxyhub-Country"     // 代理国家
	HdrLatencyMs  = "X-Proxyhub-Latency-Ms"  // 本次请求代理侧延迟
	HdrSession    = "X-Proxyhub-Session"     // 回显 session ID
	HdrRotated    = "X-Proxyhub-Rotated"     // session 是否发生 IP 轮转 (true/false)
	HdrAttempts   = "X-Proxyhub-Attempts"    // 实际尝试次数
)

// forwardReq 解析自请求头/用户名的前向代理控制参数
type forwardReq struct {
	opts     pool.PickOpts
	session  string        // session id（空 = 无 session）
	rotate   bool          // 强制轮转
	ttl      time.Duration // session TTL override
}

// parseForwardReq 从请求解析控制参数
//
// 支持两种传参方式（业界标准）：
//  1. 自定义 header X-Proxyhub-*
//  2. Proxy-Authorization Basic 的用户名里编码（Bright Data/SmartProxy 风格）
//     例: user-session-abc-country-CN:pass
//     字段以 "-" 分隔，键值成对：session/country/protocol/rotate
func (s *Server) parseForwardReq(r *http.Request) forwardReq {
	req := forwardReq{}

	// 1. Header 优先
	req.opts = pool.PickOpts{
		Country:     r.Header.Get("X-Proxyhub-Country"),
		Protocol:    pool.Protocol(r.Header.Get("X-Proxyhub-Protocol")),
		PreferAsian: r.Header.Get("X-Proxyhub-Prefer-Asian") == "true",
		HTTPSOnly:   r.Header.Get("X-Proxyhub-HTTPS-Only") == "true",
	}
	if topN := r.Header.Get("X-Proxyhub-Top-N"); topN != "" {
		if n, err := strconv.Atoi(topN); err == nil {
			req.opts.TopN = n
		}
	}
	req.session = r.Header.Get("X-Proxyhub-Session")
	req.rotate = r.Header.Get("X-Proxyhub-Rotate") == "true"
	if ttlStr := r.Header.Get("X-Proxyhub-TTL"); ttlStr != "" {
		if d, err := time.ParseDuration(ttlStr); err == nil {
			req.ttl = d
		}
	}

	// 2. Proxy-Authorization 兜底（Bright Data 兼容语法）
	if req.session == "" && req.opts.Country == "" {
		if user := proxyAuthUser(r); user != "" {
			parseBrightDataUser(user, &req)
		}
	}
	return req
}

// proxyAuthUser 从 Proxy-Authorization: Basic 中解析用户名
func proxyAuthUser(r *http.Request) string {
	auth := r.Header.Get("Proxy-Authorization")
	if auth == "" {
		return ""
	}
	const prefix = "Basic "
	if !strings.HasPrefix(auth, prefix) {
		return ""
	}
	dec, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
	if err != nil {
		return ""
	}
	s := string(dec)
	if idx := strings.IndexByte(s, ':'); idx >= 0 {
		return s[:idx]
	}
	return s
}

// parseBrightDataUser 解析 "user-session-abc-country-CN-rotate-true" 格式
func parseBrightDataUser(user string, req *forwardReq) {
	parts := strings.Split(user, "-")
	// 跳过第一个"user"前缀（如果有）
	start := 0
	if len(parts) > 0 && (parts[0] == "user" || parts[0] == "zone") {
		start = 1
	}
	for i := start; i+1 < len(parts); i += 2 {
		key, val := parts[i], parts[i+1]
		switch strings.ToLower(key) {
		case "session":
			req.session = val
		case "country":
			req.opts.Country = strings.ToUpper(val)
		case "protocol", "proto":
			req.opts.Protocol = pool.Protocol(strings.ToLower(val))
		case "rotate":
			req.rotate = val == "true" || val == "1"
		case "asian":
			req.opts.PreferAsian = val == "true" || val == "1"
		}
	}
}

// pickForSession 为请求挑代理：session 粘性优先，其他按 PickOpts 挑
//
// 返回: (proxy, rotated: 是否刚刚轮转了一个新 IP)
func (s *Server) pickForSession(req forwardReq) (*pool.Proxy, bool) {
	// 显式请求轮转：清掉 session 绑定
	if req.rotate && req.session != "" {
		s.sessionMgr.Rotate(req.session)
	}

	// 有 session：查现有绑定
	if req.session != "" {
		if sess := s.sessionMgr.Get(req.session); sess != nil && sess.Proxy != nil && !sess.Proxy.IsBanned() {
			return sess.Proxy, false
		}
		// 重新绑定
		pr := s.pool.Pick(req.opts)
		if pr == nil {
			return nil, false
		}
		s.sessionMgr.Bind(req.session, pr, req.ttl)
		return pr, true
	}

	// 无 session：直接挑
	return s.pool.Pick(req.opts), false
}

func (s *Server) handleForwardProxy(w http.ResponseWriter, r *http.Request) {
	s.proxyReqCount.Add(1)
	req := s.parseForwardReq(r)

	if r.Method == http.MethodConnect {
		s.handleConnect(w, r, req)
		return
	}
	s.handleHTTPProxy(w, r, req)
}

// writeProxyHeaders 把代理元数据写入响应头
func writeProxyHeaders(h http.Header, p *pool.Proxy, session string, rotated bool, attempts int, latency time.Duration) {
	if p != nil {
		h.Set(HdrProxy, p.URL)
		h.Set(HdrCountry, p.Country)
	}
	if latency > 0 {
		h.Set(HdrLatencyMs, strconv.FormatInt(latency.Milliseconds(), 10))
	}
	if session != "" {
		h.Set(HdrSession, session)
		h.Set(HdrRotated, strconv.FormatBool(rotated))
	}
	h.Set(HdrAttempts, strconv.Itoa(attempts))
}

// handleHTTPProxy 处理普通 HTTP 请求（非 CONNECT）
//
// 带重试 + session 轮转：
//   - 有 session: 失败达到阈值后切换新 IP
//   - 无 session: 每次失败都挑新代理
func (s *Server) handleHTTPProxy(w http.ResponseWriter, r *http.Request, req forwardReq) {
	const maxRetries = 5

	var (
		lastErr     error
		rotatedInit bool
	)

	for attempt := 1; attempt <= maxRetries; attempt++ {
		pr, rotated := s.pickForSession(req)
		if attempt == 1 {
			rotatedInit = rotated
		}
		if pr == nil {
			lastErr = fmt.Errorf("no proxy available")
			break
		}

		proxyURL, err := url.Parse(pr.URL)
		if err != nil {
			pr.RecordFail(s.pool.FailCooldown())
			lastErr = err
			continue
		}

		start := time.Now()
		tr := &http.Transport{
			Proxy:                 http.ProxyURL(proxyURL),
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		}
		client := &http.Client{Transport: tr, Timeout: 30 * time.Second}

		// 新 outReq（body 可能被消费）— 仅重试不重读 body，如需要可 rewind
		outReq, err := http.NewRequestWithContext(r.Context(), r.Method, r.URL.String(), r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		copyHeader(outReq.Header, r.Header)
		removeHopHeaders(outReq.Header)
		// 不透传 X-Proxyhub-* 到上游
		for k := range outReq.Header {
			if strings.HasPrefix(k, "X-Proxyhub-") {
				outReq.Header.Del(k)
			}
		}

		resp, err := client.Do(outReq)
		latency := time.Since(start)
		if err != nil {
			pr.RecordFail(s.pool.FailCooldown())
			lastErr = err
			// session 下：累计失败，达到阈值才真正换 IP
			if req.session != "" {
				if shouldRotate := s.sessionMgr.RecordFailure(req.session); shouldRotate {
					s.sessionMgr.Rotate(req.session)
					req.rotate = false // 下次循环自然会重新 bind
				} else {
					// 还没到阈值，但代理已 ban，下次循环 pickForSession 会自动换
				}
			}
			slog.Debug("HTTP proxy attempt failed",
				"attempt", attempt, "proxy", pr.URL, "url", r.URL.Host, "err", err)
			continue
		}

		// 成功
		pr.RecordSuccess(latency)
		if req.session != "" {
			s.sessionMgr.Touch(req.session)
		}

		// 写响应头（在 WriteHeader 前）
		copyHeader(w.Header(), resp.Header)
		removeHopHeaders(w.Header())
		writeProxyHeaders(w.Header(), pr, req.session, rotatedInit, attempt, latency)
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		resp.Body.Close()
		return
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("exhausted retries")
	}
	slog.Warn("forward proxy failed", "url", r.URL.String(), "err", lastErr)
	w.Header().Set(HdrAttempts, strconv.Itoa(maxRetries))
	http.Error(w, "proxy upstream error: "+lastErr.Error(), http.StatusBadGateway)
}

// handleConnect 处理 HTTPS CONNECT 隧道
func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request, req forwardReq) {
	// HTTPS CONNECT 跳过 socks 类型
	if req.opts.Protocol == "" {
		req.opts.Protocol = pool.ProtoHTTP
	}

	const maxRetries = 15
	var (
		upstream    net.Conn
		pickedPr    *pool.Proxy
		lastErr     error
		totalAttempts int
		rotatedInit bool
		latencyOK   time.Duration
	)
	for attempt := 0; attempt < maxRetries; attempt++ {
		totalAttempts = attempt + 1
		pr, rotated := s.pickForSession(req)
		if attempt == 0 {
			rotatedInit = rotated
		}
		if pr == nil {
			// 切换协议再试
			if req.opts.Protocol == pool.ProtoHTTP {
				req.opts.Protocol = pool.ProtoHTTPS
				continue
			}
			break
		}
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
			if req.session != "" {
				if shouldRotate := s.sessionMgr.RecordFailure(req.session); shouldRotate {
					s.sessionMgr.Rotate(req.session)
				}
			}
			slog.Debug("CONNECT attempt failed", "proxy", pr.URL, "host", r.Host, "err", err)
			continue
		}
		latencyOK = time.Since(start)
		pr.RecordSuccess(latencyOK)
		if req.session != "" {
			s.sessionMgr.Touch(req.session)
		}
		upstream = conn
		pickedPr = pr
		break
	}
	if upstream == nil {
		if lastErr == nil {
			lastErr = fmt.Errorf("no suitable proxy")
		}
		slog.Warn("CONNECT failed after retries", "host", r.Host, "err", lastErr, "retries", maxRetries)
		w.Header().Set(HdrAttempts, strconv.Itoa(totalAttempts))
		http.Error(w, "upstream error: "+lastErr.Error(), http.StatusBadGateway)
		return
	}
	defer upstream.Close()

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

	// 200 Connection Established + 代理元数据 header
	headers := "HTTP/1.1 200 Connection Established\r\n"
	headers += fmt.Sprintf("%s: %s\r\n", HdrProxy, pickedPr.URL)
	headers += fmt.Sprintf("%s: %s\r\n", HdrCountry, pickedPr.Country)
	headers += fmt.Sprintf("%s: %d\r\n", HdrLatencyMs, latencyOK.Milliseconds())
	headers += fmt.Sprintf("%s: %d\r\n", HdrAttempts, totalAttempts)
	if req.session != "" {
		headers += fmt.Sprintf("%s: %s\r\n", HdrSession, req.session)
		headers += fmt.Sprintf("%s: %v\r\n", HdrRotated, rotatedInit)
	}
	headers += "\r\n"
	if _, err := clientConn.Write([]byte(headers)); err != nil {
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

// ============================================================
// Session API
// ============================================================

// GET /api/v1/sessions               -> 列表
// POST /api/v1/sessions {id, ttl}    -> 创建/查看绑定
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	s.apiReqCount.Add(1)
	switch r.Method {
	case http.MethodGet:
		all := s.sessionMgr.All()
		out := make([]map[string]any, 0, len(all))
		now := time.Now()
		for _, sess := range all {
			item := map[string]any{
				"id":            sess.ID,
				"created_at":    sess.CreatedAt.Format(time.RFC3339),
				"last_used":     sess.LastUsed.Format(time.RFC3339),
				"ttl_seconds":   int(sess.TTL.Seconds()),
				"expires_in":    int(sess.TTL.Seconds() - now.Sub(sess.LastUsed).Seconds()),
				"failure_count": sess.FailureCount,
			}
			if sess.Proxy != nil {
				item["proxy"] = proxyToJSON(sess.Proxy)
			}
			out = append(out, item)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"sessions": out,
			"count":    len(out),
		})
	case http.MethodPost:
		var body struct {
			ID       string `json:"id"`
			Country  string `json:"country"`
			Protocol string `json:"protocol"`
			TTL      string `json:"ttl"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if body.ID == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		ttl := 0 * time.Second
		if body.TTL != "" {
			if d, err := time.ParseDuration(body.TTL); err == nil {
				ttl = d
			}
		}
		// 复用 pickForSession 的语义：如果已有绑定就返回，否则新建
		req := forwardReq{
			session: body.ID,
			ttl:     ttl,
			opts: pool.PickOpts{
				Country:  strings.ToUpper(body.Country),
				Protocol: pool.Protocol(body.Protocol),
			},
		}
		pr, _ := s.pickForSession(req)
		if pr == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "no proxy available"})
			return
		}
		sess := s.sessionMgr.Get(body.ID)
		writeJSON(w, http.StatusOK, map[string]any{
			"id":         body.ID,
			"proxy":      proxyToJSON(pr),
			"ttl_seconds": int(sess.TTL.Seconds()),
		})
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		s.sessionMgr.Rotate(id)
		writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// POST /api/v1/sessions/rotate?id=xxx -> 强制轮转
func (s *Server) handleSessionRotate(w http.ResponseWriter, r *http.Request) {
	s.apiReqCount.Add(1)
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	s.sessionMgr.Rotate(id)
	writeJSON(w, http.StatusOK, map[string]any{"rotated": id})
}

// CleanupSessions 启动后台清理过期 session
func (s *Server) CleanupSessions(stop <-chan struct{}, interval time.Duration) {
	if interval <= 0 {
		interval = 1 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			n := s.sessionMgr.Cleanup()
			if n > 0 {
				slog.Debug("session cleanup", "removed", n)
			}
		}
	}
}
