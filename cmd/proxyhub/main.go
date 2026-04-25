// proxyhub - 聚合代理池服务
//
// 用法：
//
//	proxyhub serve --proxy-port 7000 --api-port 7001 --db ./proxyhub.db
//
// 启动后：
//
//	curl -x http://localhost:7000 https://example.com  # HTTP 前向代理
//	curl http://localhost:7001/api/v1/stats            # REST API
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jiusanzhou/proxyhub/internal/metrics"
	"github.com/jiusanzhou/proxyhub/internal/pool"
	"github.com/jiusanzhou/proxyhub/internal/server"
	"github.com/jiusanzhou/proxyhub/internal/source"
	"github.com/jiusanzhou/proxyhub/internal/store"
)

// 版本信息（由 goreleaser 通过 ldflags 注入）
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "serve":
		cmdServe(args)
	case "version", "-v", "--version":
		fmt.Printf("proxyhub %s\ncommit: %s\nbuilt:  %s\n", version, commit, date)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`proxyhub - 聚合代理池服务

USAGE:
    proxyhub <command> [flags]

COMMANDS:
    serve     启动 proxyhub 服务（HTTP 前向代理 + REST API）
    version   打印版本号
    help      显示帮助

EXAMPLES:
    # 启动服务（默认 :7000 前向代理 + :7001 API）
    proxyhub serve

    # 自定义端口和 DB 路径
    proxyhub serve --proxy-port 8000 --api-port 8001 --db /var/lib/proxyhub.db

    # 添加额外文本订阅源
    proxyhub serve --extra-source "my-list=https://example.com/proxies.txt:http"

CLIENT EXAMPLES:
    # 用作 HTTP 前向代理
    curl -x http://localhost:7000 https://api.example.com/data

    # REST API 获取代理
    curl 'http://localhost:7001/api/v1/pick?country=CN&protocol=https'

    # 查看池状态
    curl http://localhost:7001/api/v1/stats

    # Prometheus 指标
    curl http://localhost:7001/metrics`)
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	proxyPort := fs.Int("proxy-port", 7000, "HTTP 前向代理端口")
	apiPort := fs.Int("api-port", 7001, "REST API + Prometheus 端口")
	dbPath := fs.String("db", "./proxyhub.db", "SQLite 数据库路径")
	refreshInterval := fs.Duration("refresh-interval", 10*time.Minute, "代理池刷新间隔")
	failCooldown := fs.Duration("fail-cooldown", 5*time.Minute, "失败代理冷却时间")
	logLevel := fs.String("log-level", "info", "日志级别 debug/info/warn/error")
	extraSources := fs.String("extra-source", "", "额外文本订阅源，格式 name=url:proto，多个用 ; 分隔")

	// 健康探测
	checkEnabled := fs.Bool("health-check", true, "启用后台健康探测")
	checkInterval := fs.Duration("check-interval", 60*time.Second, "健康探测整轮间隔")
	checkDialTimeout := fs.Duration("check-dial-timeout", 5*time.Second, "L4 TCP dial 超时")
	checkHTTPTimeout := fs.Duration("check-http-timeout", 8*time.Second, "L7 HTTP CONNECT 探测超时")
	checkConcurrency := fs.Int("check-concurrency", 50, "健康探测并发度")
	checkL7 := fs.Bool("check-l7", false, "启用 L7 HTTP CONNECT 探测（对目标 host 有压力）")
	checkTarget := fs.String("check-target", "httpbin.org:80", "L7 探测目标 host:port")
	checkBanOnFail := fs.Int("check-ban-on-fail", 3, "连续探测失败多少次标记 banned")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	// 日志
	var level slog.Level
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
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
	st, err := store.NewSQLite(*dbPath)
	if err != nil {
		slog.Error("open store", "err", err, "path", *dbPath)
		os.Exit(1)
	}
	defer st.Close()
	slog.Info("store ready", "path", *dbPath)

	// 代理来源
	sources := []pool.Source{source.NewProxifly()}
	if *extraSources != "" {
		extra, err := parseExtraSources(*extraSources)
		if err != nil {
			slog.Error("parse extra-source", "err", err)
			os.Exit(1)
		}
		for _, s := range extra {
			sources = append(sources, s)
		}
	}

	// 代理池
	p := pool.New(
		pool.WithSources(sources...),
		pool.WithStore(st),
		pool.WithRefreshInterval(*refreshInterval),
		pool.WithFailCooldown(*failCooldown),
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
	if *checkEnabled {
		checker = pool.NewChecker(p, pool.CheckerConfig{
			Enabled:     true,
			Interval:    *checkInterval,
			DialTimeout: *checkDialTimeout,
			HTTPTimeout: *checkHTTPTimeout,
			Concurrency: *checkConcurrency,
			TargetHost:  *checkTarget,
			EnableL7:    *checkL7,
			BanOnFail:   *checkBanOnFail,
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

	// 前向代理（端口 1）
	proxyAddr := fmt.Sprintf(":%d", *proxyPort)
	proxySrv := &http.Server{
		Addr:    proxyAddr,
		Handler: srv.ProxyHandler(),
	}
	go func() {
		slog.Info("forward proxy listening", "addr", proxyAddr)
		if err := proxySrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("proxy server", "err", err)
		}
	}()

	// API + metrics（端口 2）
	apiMux := http.NewServeMux()
	apiMux.Handle("/", srv.HTTPHandler())
	apiMux.Handle("/metrics", metrics.Handler(p))
	apiAddr := fmt.Sprintf(":%d", *apiPort)
	apiSrv := &http.Server{
		Addr:    apiAddr,
		Handler: apiMux,
	}
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
		// 反向找 :proto（避免 url 内的冒号干扰）
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
