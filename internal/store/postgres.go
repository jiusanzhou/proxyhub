// PostgreSQL store 实现
//
// 用 pgx 驱动连接，schema 与 SQLite 兼容（差异：
// - INTEGER → BIGINT
// - 主键约束 ON CONFLICT 用 PG 的 ON CONFLICT DO UPDATE）
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.zoe.im/x"

	"go.zoe.im/proxyhub/internal/pool"
)

// PGStore PostgreSQL 持久化
type PGStore struct {
	pool *pgxpool.Pool
}

// NewPostgres 打开 PG 连接池并初始化 schema
//
// dsn 例: postgres://user:pass@host:5432/dbname?sslmode=disable
func NewPostgres(ctx context.Context, dsn string) (*PGStore, error) {
	pcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse pg dsn: %w", err)
	}
	// 默认值合理：MaxConns 10, MinConns 0
	pp, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("new pg pool: %w", err)
	}
	if err := pp.Ping(ctx); err != nil {
		pp.Close()
		return nil, fmt.Errorf("pg ping: %w", err)
	}
	if err := initPGSchema(ctx, pp); err != nil {
		pp.Close()
		return nil, fmt.Errorf("init pg schema: %w", err)
	}
	return &PGStore{pool: pp}, nil
}

// Close 关闭连接池
func (s *PGStore) Close() error {
	s.pool.Close()
	return nil
}

// Save 批量 UPSERT
func (s *PGStore) Save(ctx context.Context, proxies []*pool.Proxy) error {
	if len(proxies) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	now := time.Now().UnixNano()
	const stmt = `
		INSERT INTO proxies (
			proxy_url, protocol, country, anonymity,
			total_requests, success_count, fail_count,
			last_latency_ms, avg_latency_ms_x1000,
			is_banned, ban_until_ns, last_used_at_ns, last_check_at_ns, source
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (proxy_url) DO UPDATE SET
			protocol = EXCLUDED.protocol,
			country = EXCLUDED.country,
			anonymity = EXCLUDED.anonymity,
			total_requests = EXCLUDED.total_requests,
			success_count = EXCLUDED.success_count,
			fail_count = EXCLUDED.fail_count,
			last_latency_ms = EXCLUDED.last_latency_ms,
			avg_latency_ms_x1000 = EXCLUDED.avg_latency_ms_x1000,
			is_banned = EXCLUDED.is_banned,
			ban_until_ns = EXCLUDED.ban_until_ns,
			last_used_at_ns = EXCLUDED.last_used_at_ns,
			last_check_at_ns = EXCLUDED.last_check_at_ns,
			source = EXCLUDED.source
	`
	for _, p := range proxies {
		isBanned := false
		if p.IsBanned() {
			isBanned = true
		}
		_, err := tx.Exec(ctx, stmt,
			p.URL, string(p.Protocol), p.Country, string(p.Anonymity),
			p.TotalRequests(), p.SuccessCount(), p.FailCount(),
			p.LastLatencyMs(), int64(p.AvgLatencyMs()*1000),
			isBanned, p.BannedUntil(), p.LastUsedAt(), now, p.Source,
		)
		if err != nil {
			return fmt.Errorf("save %s: %w", p.URL, err)
		}
	}
	return tx.Commit(ctx)
}

// LoadAll 加载所有代理
func (s *PGStore) LoadAll(ctx context.Context) ([]*pool.Proxy, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT proxy_url, protocol, country, anonymity,
			total_requests, success_count, fail_count,
			last_latency_ms, avg_latency_ms_x1000,
			ban_until_ns, last_used_at_ns, source
		FROM proxies
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*pool.Proxy
	for rows.Next() {
		p := &pool.Proxy{}
		var protoStr, anonStr string
		var totalReq, successCount, failCount, lastLatency, avgLatencyX1000 int64
		var banUntilNs, lastUsedNs int64
		err := rows.Scan(
			&p.URL, &protoStr, &p.Country, &anonStr,
			&totalReq, &successCount, &failCount,
			&lastLatency, &avgLatencyX1000,
			&banUntilNs, &lastUsedNs, &p.Source,
		)
		if err != nil {
			continue
		}
		p.Protocol = pool.Protocol(protoStr)
		p.Anonymity = pool.Anonymity(anonStr)
		p.SetStats(totalReq, successCount, failCount, avgLatencyX1000)
		p.SetBannedUntil(banUntilNs)
		p.SetLastUsedAt(lastUsedNs)
		out = append(out, p)
	}
	return out, rows.Err()
}

// initPGSchema 创建 PG 表
func initPGSchema(ctx context.Context, pp *pgxpool.Pool) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS proxies (
	proxy_url            TEXT PRIMARY KEY,
	protocol             TEXT NOT NULL,
	country              TEXT NOT NULL DEFAULT 'XX',
	anonymity            TEXT NOT NULL DEFAULT 'unknown',
	total_requests       BIGINT NOT NULL DEFAULT 0,
	success_count        BIGINT NOT NULL DEFAULT 0,
	fail_count           BIGINT NOT NULL DEFAULT 0,
	last_latency_ms      BIGINT NOT NULL DEFAULT 0,
	avg_latency_ms_x1000 BIGINT NOT NULL DEFAULT 0,
	is_banned            BOOLEAN NOT NULL DEFAULT FALSE,
	ban_until_ns         BIGINT NOT NULL DEFAULT 0,
	last_used_at_ns      BIGINT NOT NULL DEFAULT 0,
	last_check_at_ns     BIGINT NOT NULL DEFAULT 0,
	source               TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_proxies_country ON proxies(country);
CREATE INDEX IF NOT EXISTS idx_proxies_protocol ON proxies(protocol);
CREATE INDEX IF NOT EXISTS idx_proxies_banned ON proxies(is_banned);
`
	_, err := pp.Exec(ctx, ddl)
	return err
}

// PG store factory 注册
func pgCreator(cfg x.TypedLazyConfig, opts ...Option) (Store, error) {
	var c struct {
		DSN string `json:"dsn"`
	}
	if err := cfg.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("postgres config unmarshal: %w", err)
	}
	if c.DSN == "" {
		return nil, fmt.Errorf("postgres store: dsn required")
	}
	return NewPostgres(context.Background(), c.DSN)
}

func init() {
	_ = Register("postgres", pgCreator, "pg", "postgresql")
}
