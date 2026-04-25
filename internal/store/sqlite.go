// Package store 代理池持久化（SQLite 实现）
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动，无需 CGO

	"github.com/jiusanzhou/proxyhub/internal/pool"
)

// SQLiteStore 基于 SQLite 的持久化（零依赖）
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLite 打开/创建 SQLite 数据库
func NewSQLite(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite 单写
	if err := initSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return &SQLiteStore{db: db}, nil
}

// Close 关闭
func (s *SQLiteStore) Close() error { return s.db.Close() }

// Save 批量 UPSERT
func (s *SQLiteStore) Save(ctx context.Context, proxies []*pool.Proxy) error {
	if len(proxies) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
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
			source = excluded.source
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UnixNano()
	for _, p := range proxies {
		isBanned := 0
		if p.IsBanned() {
			isBanned = 1
		}
		_, err := stmt.ExecContext(ctx,
			p.URL, string(p.Protocol), p.Country, string(p.Anonymity),
			p.TotalRequests(), p.SuccessCount(), p.FailCount(),
			p.LastLatencyMs(), int64(p.AvgLatencyMs()*1000),
			isBanned, p.BannedUntil(), p.LastUsedAt(), now, p.Source,
		)
		if err != nil {
			return fmt.Errorf("save %s: %w", p.URL, err)
		}
	}
	return tx.Commit()
}

// LoadAll 加载所有代理
func (s *SQLiteStore) LoadAll(ctx context.Context) ([]*pool.Proxy, error) {
	rows, err := s.db.QueryContext(ctx, `
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

// initSchema 创建表
func initSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS proxies (
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
		);
		CREATE INDEX IF NOT EXISTS idx_proxies_country ON proxies(country);
		CREATE INDEX IF NOT EXISTS idx_proxies_protocol ON proxies(protocol);
		CREATE INDEX IF NOT EXISTS idx_proxies_banned ON proxies(is_banned);
	`)
	return err
}
