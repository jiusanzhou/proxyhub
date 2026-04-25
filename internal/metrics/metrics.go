// Package metrics 导出 Prometheus 指标
package metrics

import (
	"fmt"
	"net/http"

	"go.zoe.im/proxyhub/internal/pool"
)

// Handler 返回 Prometheus 兼容的 /metrics handler
//
// 输出纯文本格式（exposition format），避免引入 prometheus 客户端依赖。
// 后续需要直方图/计数器再引入 github.com/prometheus/client_golang。
func Handler(p *pool.Pool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stats := p.Stats()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")

		fmt.Fprintf(w, "# HELP proxyhub_pool_total Total number of proxies in pool\n")
		fmt.Fprintf(w, "# TYPE proxyhub_pool_total gauge\n")
		fmt.Fprintf(w, "proxyhub_pool_total %d\n", stats.Total)

		fmt.Fprintf(w, "# HELP proxyhub_pool_available Number of available proxies (not banned)\n")
		fmt.Fprintf(w, "# TYPE proxyhub_pool_available gauge\n")
		fmt.Fprintf(w, "proxyhub_pool_available %d\n", stats.Available)

		fmt.Fprintf(w, "# HELP proxyhub_pool_banned Number of currently banned proxies\n")
		fmt.Fprintf(w, "# TYPE proxyhub_pool_banned gauge\n")
		fmt.Fprintf(w, "proxyhub_pool_banned %d\n", stats.Banned)

		fmt.Fprintf(w, "# HELP proxyhub_pool_avg_score Average score across all proxies\n")
		fmt.Fprintf(w, "# TYPE proxyhub_pool_avg_score gauge\n")
		fmt.Fprintf(w, "proxyhub_pool_avg_score %f\n", stats.AvgScore)

		fmt.Fprintf(w, "# HELP proxyhub_pool_avg_latency_ms Average latency of proxies in ms\n")
		fmt.Fprintf(w, "# TYPE proxyhub_pool_avg_latency_ms gauge\n")
		fmt.Fprintf(w, "proxyhub_pool_avg_latency_ms %f\n", stats.AvgLatency)

		fmt.Fprintf(w, "# HELP proxyhub_pool_by_country Proxy count by country\n")
		fmt.Fprintf(w, "# TYPE proxyhub_pool_by_country gauge\n")
		for country, count := range stats.ByCountry {
			fmt.Fprintf(w, "proxyhub_pool_by_country{country=\"%s\"} %d\n", country, count)
		}

		fmt.Fprintf(w, "# HELP proxyhub_pool_by_protocol Proxy count by protocol\n")
		fmt.Fprintf(w, "# TYPE proxyhub_pool_by_protocol gauge\n")
		for proto, count := range stats.ByProtocol {
			fmt.Fprintf(w, "proxyhub_pool_by_protocol{protocol=\"%s\"} %d\n", proto, count)
		}
	})
}
