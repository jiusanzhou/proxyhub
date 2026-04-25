// Package source 代理来源接口 + 实现（proxifly 主源 + 通用文本订阅源）
package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jiusanzhou/proxyhub/internal/pool"
)

// Source 代理来源接口
type Source interface {
	Name() string
	Fetch(ctx context.Context) ([]*pool.Proxy, error)
}

// ============================================================
// Proxifly 源（github.com/proxifly/free-proxy-list, jsdelivr CDN）
// ============================================================

const proxiflyAllJSON = "https://cdn.jsdelivr.net/gh/proxifly/free-proxy-list@main/proxies/all/data.json"

type proxiflyEntry struct {
	ProxyURL  string `json:"proxy"`
	Protocol  string `json:"protocol"`
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	HTTPS     bool   `json:"https"`
	Anonymity string `json:"anonymity"`
	Score     int    `json:"score"`
	Geo       struct {
		// proxifly 的 country 字段已是 ISO 2 字母代码（CN/US/HK/...），
		// "ZZ" 表示未知。早期版本可能是 isocode，这里两个都兼容。
		Country string `json:"country"`
		ISOCode string `json:"isocode"`
		City    string `json:"city"`
	} `json:"geolocation"`
}

// Proxifly 主代理源
type Proxifly struct {
	URL        string
	HTTPClient *http.Client
}

// NewProxifly 默认 proxifly 源
func NewProxifly() *Proxifly {
	return &Proxifly{
		URL:        proxiflyAllJSON,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Name 源名
func (s *Proxifly) Name() string { return "proxifly" }

// Fetch 拉取并解析
func (s *Proxifly) Fetch(ctx context.Context) ([]*pool.Proxy, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("proxifly fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxifly status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	entries, err := parseProxiflyEntries(data)
	if err != nil {
		return nil, err
	}

	out := make([]*pool.Proxy, 0, len(entries))
	for _, e := range entries {
		countryCode := strings.ToUpper(e.Geo.ISOCode)
		if countryCode == "" {
			countryCode = strings.ToUpper(e.Geo.Country)
		}
		if countryCode == "" {
			countryCode = "XX"
		}
		p := &pool.Proxy{
			URL:       e.ProxyURL,
			Host:      e.IP,
			Port:      e.Port,
			Protocol:  pool.Protocol(strings.ToLower(e.Protocol)),
			Country:   countryCode,
			Anonymity: pool.Anonymity(strings.ToLower(e.Anonymity)),
			Source:    s.Name(),
		}
		if p.URL == "" && p.Host != "" && p.Port > 0 {
			p.URL = fmt.Sprintf("%s://%s:%d", p.Protocol, p.Host, p.Port)
		}
		if p.URL == "" {
			continue
		}
		if p.Anonymity == "" {
			p.Anonymity = pool.AnonUnknown
		}
		out = append(out, p)
	}
	return out, nil
}

// parseProxiflyEntries 兼容 NDJSON 和 JSON array
func parseProxiflyEntries(data []byte) ([]proxiflyEntry, error) {
	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty body")
	}
	if trimmed[0] == '[' {
		var arr []proxiflyEntry
		if err := json.Unmarshal([]byte(trimmed), &arr); err == nil {
			return arr, nil
		}
	}
	var out []proxiflyEntry
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e proxiflyEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid entries")
	}
	return out, nil
}

// ============================================================
// 通用文本订阅源（每行 host:port 或 protocol://host:port）
// ============================================================

// Text 文本订阅源
type Text struct {
	SourceName     string
	URL            string
	DefaultProto   pool.Protocol
	DefaultCountry string
	HTTPClient     *http.Client
}

// NewText 创建文本订阅源
func NewText(name, url string, defaultProto pool.Protocol) *Text {
	return &Text{
		SourceName:     name,
		URL:            url,
		DefaultProto:   defaultProto,
		DefaultCountry: "XX",
		HTTPClient:     &http.Client{Timeout: 30 * time.Second},
	}
}

// Name 源名
func (s *Text) Name() string { return s.SourceName }

// Fetch 拉取并解析
func (s *Text) Fetch(ctx context.Context) ([]*pool.Proxy, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s fetch: %w", s.SourceName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s status %d", s.SourceName, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	out := []*pool.Proxy{}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p := parseProxyLine(line, s.DefaultProto, s.DefaultCountry, s.SourceName)
		if p != nil {
			out = append(out, p)
		}
	}
	return out, nil
}

// parseProxyLine 解析单行
func parseProxyLine(line string, defProto pool.Protocol, defCountry, source string) *pool.Proxy {
	var proto = defProto
	rest := line
	if i := strings.Index(line, "://"); i > 0 {
		proto = pool.Protocol(strings.ToLower(line[:i]))
		rest = line[i+3:]
	}
	parts := strings.Split(rest, ":")
	if len(parts) != 2 {
		return nil
	}
	host := parts[0]
	port, err := strconv.Atoi(parts[1])
	if err != nil || port <= 0 || port > 65535 {
		return nil
	}
	return &pool.Proxy{
		URL:       fmt.Sprintf("%s://%s:%d", proto, host, port),
		Host:      host,
		Port:      port,
		Protocol:  proto,
		Country:   defCountry,
		Anonymity: pool.AnonUnknown,
		Source:    source,
	}
}
