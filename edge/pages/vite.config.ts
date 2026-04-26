import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// 两种构建模式:
// - 默认 (Go embed): outDir = ../../internal/dashboard/assets
// - Cloudflare Pages: PAGES_BUILD=1 -> outDir = dist
const isPagesBuild = process.env.PAGES_BUILD === '1'

// API base URL (Pages 模式下指向生产 Workers)
const apiBase = process.env.VITE_API_BASE || ''

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  // Pages 模式下用 pages-public 作为静态资源目录（含 _headers）；
  // embed 模式下不要 publicDir，避免污染 Go embed 目录
  publicDir: isPagesBuild ? 'pages-public' : false,
  define: {
    __API_BASE__: JSON.stringify(apiBase),
  },
  build: {
    outDir: isPagesBuild ? 'dist' : '../../internal/dashboard/assets',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        entryFileNames: 'assets/app.js',
        chunkFileNames: 'assets/[name].js',
        assetFileNames: 'assets/[name][extname]',
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:7001',
      '/healthz': 'http://localhost:7001',
      '/metrics': 'http://localhost:7001',
    },
  },
})
