// Package commands 注册 proxyhub 的所有子命令
package commands

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.zoe.im/x/cli"

	"go.zoe.im/proxyhub/cmd"
	"go.zoe.im/proxyhub/internal/config"
	"go.zoe.im/proxyhub/internal/dashboard"
	"go.zoe.im/proxyhub/internal/metrics"
	"go.zoe.im/proxyhub/internal/pool"
	"go.zoe.im/proxyhub/internal/server"
	"go.zoe.im/proxyhub/internal/source"
	"go.zoe.im/proxyhub/internal/store"
)

func init() {
	cmd.Register(
		cli.New(
			cli.Name("serve"),
			cli.Short("启动 proxyhub 服务（前向代理 + API + Dashboard）"),
			cli.Example(`  proxyhub serve
  proxyhub serve --proxy-port 8000 --api-port 8001 --db /var/lib/proxyhub.db
  proxyhub serve --config /etc/proxyhub.yaml
  proxyhub serve --extra-source "my-list=https://example.com/list.txt:http"`),
			cli.Run(func(c *cli.Command, args ...string) {
				if err := runServe(config.Global); err != nil {
					slog.Error("serve failed", "err", err)
					os.Exit(1)
				}
			}),
		),
	)
}

func runServe(cfg *config.Config) error {
	// 日志
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// 信号
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Info("shutting down...")
		cancel()
	}()

	// SQLite store
	st, err := store.NewSQLite(cfg.DB)
	if err != nil {
		return fmt.Errorf("open store %q: %w", cfg.DB, err)
	}
	defer st.Close()
	slog.Info("store ready", "path", cfg.DB)

	// 代理来源
	sources := []pool.Source{source.NewProxifly()}
	if cfg.ExtraSource != "" {
		extra, err := parseExtraSources(cfg.ExtraSource)
		if err != nil {
			return fmt.Errorf("parse extra-source: %w", err)
		}
		for _, s := range extra {
			sources = append(sources, s)
		}
	}

	// 代理池
	p := pool.New(
		pool.WithSources(sources...),
		pool.WithStore(st),
		pool.WithRefreshInterval(cfg.RefreshInterval),
		pool.WithFailCooldown(cfg.FailCooldown),
		pool.WithLogger(logger),
	)

	// 后台刷新
	go func() {
		if err := p.Start(ctx); err != nil && err != context.Canceled {
			slog.Error("pool stopped", "err", err)
		}
	}()

	// 健康探测
	var checker *pool.Checker
	if cfg.CheckEnabled {
		checker = pool.NewChecker(p, pool.CheckerConfig{
			Enabled:     true,
			Interval:    cfg.CheckInterval,
			DialTimeout: cfg.CheckDialTimeout,
			HTTPTimeout: cfg.CheckHTTPTimeout,
			Concurrency: cfg.CheckConcurrency,
			TargetHost:  cfg.CheckTarget,
			EnableL7:    cfg.CheckL7,
			BanOnFail:   cfg.CheckBanOnFail,
		}, logger)
		go checker.Run(ctx)
	}

	// 服务器
	srv := server.New(p)
	if checker != nil {
		srv.SetChecker(checker)
	}

	// session 清理 goroutine
	stopSessions := make(chan struct{})
	defer close(stopSessions)
	go srv.CleanupSessions(stopSessions, 1*time.Minute)

	// 前向代理
	proxyAddr := fmt.Sprintf(":%d", cfg.ProxyPort)
	proxySrv := &http.Server{Addr: proxyAddr, Handler: srv.ProxyHandler()}
	go func() {
		slog.Info("forward proxy listening", "addr", proxyAddr)
		if err := proxySrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("proxy server", "err", err)
		}
	}()

	// API + metrics + dashboard
	apiMux := http.NewServeMux()
	apiMux.Handle("/metrics", metrics.Handler(p))
	apiMux.Handle("/", srv.HTTPHandler())
	apiHandler := dashboard.Handler(apiMux)
	apiAddr := fmt.Sprintf(":%d", cfg.APIPort)
	apiSrv := &http.Server{Addr: apiAddr, Handler: apiHandler}
	go func() {
		slog.Info("api server listening", "addr", apiAddr)
		if err := apiSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("api server", "err", err)
		}
	}()

	<-ctx.Done()
	slog.Info("graceful shutdown...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = proxySrv.Shutdown(shutdownCtx)
	_ = apiSrv.Shutdown(shutdownCtx)
	_ = p.Flush(shutdownCtx)
	slog.Info("bye")
	return nil
}

// parseExtraSources 解析 "name1=url1:proto1;name2=url2:proto2"
func parseExtraSources(s string) ([]*source.Text, error) {
	out := []*source.Text{}
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.Index(part, "=")
		if eq < 0 {
			return nil, fmt.Errorf("invalid extra-source %q (need name=url:proto)", part)
		}
		name := part[:eq]
		rest := part[eq+1:]
		colonIdx := strings.LastIndex(rest, ":")
		var url, protoStr string
		if colonIdx < 0 {
			url = rest
			protoStr = "http"
		} else {
			url = rest[:colonIdx]
			protoStr = rest[colonIdx+1:]
		}
		out = append(out, source.NewText(name, url, pool.Protocol(protoStr)))
	}
	return out, nil
}
