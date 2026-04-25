// Package dashboard 提供嵌入式 Web UI
//
// 所有静态资源通过 go:embed 打进二进制，零运行时依赖。
package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed assets
var assetsFS embed.FS

// Handler 返回 Dashboard 的 http.Handler：
//
//	"/"              → index.html（直接读取，不走 301）
//	"/assets/*"      → embedded 静态文件
//
// 其他路径（如 /api/v1/*）交给 next。
func Handler(next http.Handler) http.Handler {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic(err)
	}

	// 读出 index.html，减少启动抖动
	indexHTML, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		panic(err)
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/" || p == "/index.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
			_, _ = w.Write(indexHTML)
		case strings.HasPrefix(p, "/assets/"):
			r2 := r.Clone(r.Context())
			r2.URL.Path = strings.TrimPrefix(p, "/assets")
			if r2.URL.Path == "" {
				r2.URL.Path = "/"
			}
			fileServer.ServeHTTP(w, r2)
		default:
			next.ServeHTTP(w, r)
		}
	})
}
