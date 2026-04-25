import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  // 产物直接输出到 ../assets (Go embed 目标)
  build: {
    outDir: '../assets',
    emptyOutDir: true,
    // 单文件产物，避免 hash 分包（dashboard 体积小没必要）
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
    // 开发时把 API 请求 proxy 到本地 proxyhub
    proxy: {
      '/api': 'http://localhost:7001',
      '/healthz': 'http://localhost:7001',
      '/metrics': 'http://localhost:7001',
    },
  },
})
