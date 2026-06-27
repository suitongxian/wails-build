import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import path from 'node:path'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'frontend_real'),
    },
  },
  test: {
    include: [
      'frontend_real/**/__tests__/**/*.test.ts',
      'frontend_real/__tests__/**/*.test.ts',
    ],
    environment: 'happy-dom',
    globals: true,
    setupFiles: ['frontend_real/vitest.setup.ts'],
    // 全量 app 挂载的集成测试在负载下 >5s，默认 5000ms 易超时误判（断言本身通过）。
    testTimeout: 20000,
    deps: {
      inline: ['vuetify'],
    },
  },
})
