// Package dashboard 提供嵌入式 Web UI
//
// Web UI 由 Vite/React 构建（internal/dashboard/web/），
// 产物输出到 internal/dashboard/assets/，再通过 go:embed 打进二进制。
//
// 目录结构（build 后）：
//
//	assets/
//	├── index.html          ← 入口
//	└── assets/             ← Vite 默认产物子目录
//	    ├── app.js
//	    └── index.css
package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed assets
var rootFS embed.FS

// Handler 返回 Dashboard 的 http.Handler：
//
//	"/"              → assets/index.html（直接读出，不走 301）
//	"/assets/*"      → assets/assets/* （Vite 产物子目录）
//
// 其他路径（如 /api/v1/*）交给 next。
func Handler(next http.Handler) http.Handler {
	// 把 embed root 抽到 assets 目录之下
	sub, err := fs.Sub(rootFS, "assets")
	if err != nil {
		panic(err)
	}

	indexHTML, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		// 没 build 过的话，给个友好提示而不是 panic
		indexHTML = []byte(`<!doctype html><html><body>
<h1>proxyhub dashboard not built</h1>
<p>Run <code>make dashboard</code> or
<code>cd internal/dashboard/web &amp;&amp; pnpm install &amp;&amp; pnpm build</code></p>
</body></html>`)
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
			// /assets/app.js → 实际位置 sub/assets/app.js
			// 直接交给 fileServer，路径不变
			fileServer.ServeHTTP(w, r)
		case p == "/favicon.ico":
			// data URL 在 html 里，浏览器还是可能请求；返回 204
			w.WriteHeader(http.StatusNoContent)
		default:
			next.ServeHTTP(w, r)
		}
	})
}
