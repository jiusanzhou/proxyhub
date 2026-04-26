// D1 store 实现（通过 Cloudflare D1 REST API）
//
// D1 是 Cloudflare 的 SQLite 兼容数据库，schema 与 sqlite.go 完全相同。
// 由于 D1 REST API 是无状态 HTTP，没有连接池概念；
// 每次操作通过 HTTP POST 发送 SQL。
//
// API 端点：
//
//	POST https://api.cloudflare.com/client/v4/accounts/{account_id}/d1/database/{database_id}/query
//	Authorization: Bearer {api_token}
//	Body: {"sql": "...", "params": [...]}
package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.zoe.im/proxyhub/internal/pool"
)

const (
	d1BaseURL       = "https://api.cloudflare.com/client/v4"
	d1MaxRetries    = 3
	d1RetryBaseWait = 500 * time.Millisecond
)

// D1Store 通过 Cloudflare D1 REST API 实现持久化
type D1Store struct {
	accountID  string
	databaseID string
	apiToken   string
	endpoint   string
	client     *http.Client
}

// D1Config D1 store 配置
type D1Config struct {
	AccountID  string `json:"account_id"`
	DatabaseID string `json:"database_id"`
	APIToken   string `json:"api_token"`
}

// NewD1 创建 D1Store 并初始化 schema
func NewD1(ctx context.Context, cfg D1Config) (*D1Store, error) {
	if cfg.AccountID == "" {
		return nil, fmt.Errorf("d1: account_id required")
	}
	if cfg.DatabaseID == "" {
		return nil, fmt.Errorf("d1: database_id required")
	}
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("d1: api_token required")
	}

	s := &D1Store{
		accountID:  cfg.AccountID,
		databaseID: cfg.DatabaseID,
		apiToken:   cfg.APIToken,
		endpoint: fmt.Sprintf(
			"%s/accounts/%s/d1/database/%s/query",
			d1BaseURL, cfg.AccountID, cfg.DatabaseID,
		),
		client: &http.Client{Timeout: 30 * time.Second},
	}

	if err := s.initSchema(ctx); err != nil {
		return nil, fmt.Errorf("d1: init schema: %w", err)
	}
	return s, nil
}

// Close 无连接池，无需释放
func (s *D1Store) Close() error { return nil }

// d1Request 单条 SQL 的请求体
type d1Request struct {
	SQL    string `json:"sql"`
	Params []any  `json:"params"`
}

// d1Response D1 REST API 响应
type d1Response struct {
	Success bool           `json:"success"`
	Errors  []d1APIError   `json:"errors"`
	Result  []d1ResultItem `json:"result"`
}

type d1APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type d1ResultItem struct {
	Results []map[string]any `json:"results"`
	Meta    struct {
		RowsRead    int `json:"rows_read"`
		RowsWritten int `json:"rows_written"`
	} `json:"meta"`
}

// execSQL 发送单条 SQL（带重试）
func (s *D1Store) execSQL(ctx context.Context, sql string, params []any) (*d1Response, error) {
	body, err := json.Marshal(d1Request{SQL: sql, Params: params})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < d1MaxRetries; attempt++ {
		if attempt > 0 {
			wait := d1RetryBaseWait * time.Duration(1<<uint(attempt-1))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+s.apiToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := s.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("http: %w", err)
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read body: %w", err)
			continue
		}

		// 429/5xx 重试
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("http %d: %s", resp.StatusCode, respBody)
			continue
		}

		var d1Resp d1Response
		if err := json.Unmarshal(respBody, &d1Resp); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}
		if !d1Resp.Success {
			if len(d1Resp.Errors) > 0 {
				return nil, fmt.Errorf("d1 error %d: %s", d1Resp.Errors[0].Code, d1Resp.Errors[0].Message)
			}
			return nil, fmt.Errorf("d1: unknown error")
		}
		return &d1Resp, nil
	}
	return nil, fmt.Errorf("d1: max retries exceeded: %w", lastErr)
}

// execBatch 批量执行多条 SQL（每条独立请求，但顺序执行）
// D1 REST API 目前不支持真正的 batch，只能逐条发送。
func (s *D1Store) execBatch(ctx context.Context, stmts []d1Request) error {
	for i, stmt := range stmts {
		if _, err := s.execSQL(ctx, stmt.SQL, stmt.Params); err != nil {
			return fmt.Errorf("stmt[%d]: %w", i, err)
		}
	}
	return nil
}

// Save 批量 UPSERT（分块发送，避免请求过大）
func (s *D1Store) Save(ctx context.Context, proxies []*pool.Proxy) error {
	if len(proxies) == 0 {
		return nil
	}

	const chunkSize = 20 // D1 API 单次请求建议不超过 20 条
	now := time.Now().UnixNano()

	const upsertSQL = `
		INSERT INTO proxies (
			proxy_url, protocol, country, anonymity,
			total_requests, success_count, fail_count,
			last_latency_ms, avg_latency_ms_x1000,
			is_banned, ban_until_ns, last_used_at_ns, last_check_at_ns, source
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(proxy_url) DO UPDATE SET
			protocol = excluded.protocol,
			country = excluded.country,
			anonymity = excluded.anonymity,
			total_requests = excluded.total_requests,
			success_count = excluded.success_count,
			fail_count = excluded.fail_count,
			last_latency_ms = excluded.last_latency_ms,
			avg_latency_ms_x1000 = excluded.avg_latency_ms_x1000,
			is_banned = excluded.is_banned,
			ban_until_ns = excluded.ban_until_ns,
			last_used_at_ns = excluded.last_used_at_ns,
			last_check_at_ns = excluded.last_check_at_ns,
			source = excluded.source`

	for i := 0; i < len(proxies); i += chunkSize {
		end := i + chunkSize
		if end > len(proxies) {
			end = len(proxies)
		}
		chunk := proxies[i:end]

		var stmts []d1Request
		for _, p := range chunk {
			isBanned := 0
			if p.IsBanned() {
				isBanned = 1
			}
			stmts = append(stmts, d1Request{
				SQL: upsertSQL,
				Params: []any{
					p.URL, string(p.Protocol), p.Country, string(p.Anonymity),
					p.TotalRequests(), p.SuccessCount(), p.FailCount(),
					p.LastLatencyMs(), int64(p.AvgLatencyMs() * 1000),
					isBanned, p.BannedUntil(), p.LastUsedAt(), now, p.Source,
				},
			})
		}

		if err := s.execBatch(ctx, stmts); err != nil {
			return fmt.Errorf("save chunk %d: %w", i/chunkSize, err)
		}
	}
	return nil
}

// LoadAll 加载所有代理
func (s *D1Store) LoadAll(ctx context.Context) ([]*pool.Proxy, error) {
	resp, err := s.execSQL(ctx, `
		SELECT proxy_url, protocol, country, anonymity,
			total_requests, success_count, fail_count,
			last_latency_ms, avg_latency_ms_x1000,
			ban_until_ns, last_used_at_ns, source
		FROM proxies`, nil)
	if err != nil {
		return nil, err
	}

	if len(resp.Result) == 0 {
		return nil, nil
	}

	var out []*pool.Proxy
	for _, row := range resp.Result[0].Results {
		p := &pool.Proxy{}

		p.URL, _ = row["proxy_url"].(string)
		protoStr, _ := row["protocol"].(string)
		p.Country, _ = row["country"].(string)
		anonStr, _ := row["anonymity"].(string)
		p.Source, _ = row["source"].(string)

		p.Protocol = pool.Protocol(protoStr)
		p.Anonymity = pool.Anonymity(anonStr)

		totalReq := toInt64(row["total_requests"])
		successCount := toInt64(row["success_count"])
		failCount := toInt64(row["fail_count"])
		avgLatencyX1000 := toInt64(row["avg_latency_ms_x1000"])

		p.SetStats(totalReq, successCount, failCount, avgLatencyX1000)
		p.SetBannedUntil(toInt64(row["ban_until_ns"]))
		p.SetLastUsedAt(toInt64(row["last_used_at_ns"]))

		out = append(out, p)
	}
	return out, nil
}

// toInt64 JSON number → int64（D1 返回 float64）
func toInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

// initSchema 创建表（与 sqlite.go 完全一致）
func (s *D1Store) initSchema(ctx context.Context) error {
	ddl := []d1Request{
		{SQL: `CREATE TABLE IF NOT EXISTS proxies (
			proxy_url TEXT PRIMARY KEY,
			protocol TEXT NOT NULL,
			country TEXT NOT NULL DEFAULT 'XX',
			anonymity TEXT NOT NULL DEFAULT 'unknown',
			total_requests INTEGER NOT NULL DEFAULT 0,
			success_count INTEGER NOT NULL DEFAULT 0,
			fail_count INTEGER NOT NULL DEFAULT 0,
			last_latency_ms INTEGER NOT NULL DEFAULT 0,
			avg_latency_ms_x1000 INTEGER NOT NULL DEFAULT 0,
			is_banned INTEGER NOT NULL DEFAULT 0,
			ban_until_ns INTEGER NOT NULL DEFAULT 0,
			last_used_at_ns INTEGER NOT NULL DEFAULT 0,
			last_check_at_ns INTEGER NOT NULL DEFAULT 0,
			source TEXT NOT NULL DEFAULT ''
		)`},
		{SQL: `CREATE INDEX IF NOT EXISTS idx_proxies_country ON proxies(country)`},
		{SQL: `CREATE INDEX IF NOT EXISTS idx_proxies_protocol ON proxies(protocol)`},
		{SQL: `CREATE INDEX IF NOT EXISTS idx_proxies_banned ON proxies(is_banned)`},
	}
	return s.execBatch(ctx, ddl)
}
