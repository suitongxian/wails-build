import { defineConfig } from 'vite'
import path from 'node:path'
import vue from '@vitejs/plugin-vue'
import vuetify from 'vite-plugin-vuetify'

// Wails 前端构建：源码在 frontend_real/，产物落到 frontend-assets/，由
// main.go 通过 //go:embed 嵌入 Go 二进制。
const FRONTEND_ROOT = path.resolve(__dirname, 'frontend_real')
const FRONTEND_OUT = path.resolve(__dirname, 'frontend-assets')

export default defineConfig({
  root: FRONTEND_ROOT,
  base: './',
  plugins: [
    vue(),
    vuetify({ autoImport: true }),
  ],
  resolve: {
    alias: {
      '@': FRONTEND_ROOT,
    },
  },
  build: {
    outDir: FRONTEND_OUT,
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      // dev 模式下把 API 请求代理到本地 Go 后端
      '/scan':              'http://127.0.0.1:3001',
      '/scan-tasks':        'http://127.0.0.1:3001',
      '/files':             'http://127.0.0.1:3001',
      '/resources':         'http://127.0.0.1:3001',
      '/archive':           'http://127.0.0.1:3001',
      '/archive-management': 'http://127.0.0.1:3001',
      '/distribution':      'http://127.0.0.1:3001',
      '/statistics':        'http://127.0.0.1:3001',
      '/user-info':         'http://127.0.0.1:3001',
      '/config':            'http://127.0.0.1:3001',
      '/health':            'http://127.0.0.1:3001',
    },
  },
})
