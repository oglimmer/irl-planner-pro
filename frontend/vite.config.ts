import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

// In dev, the Vite server proxies API calls to the Go backend so the SPA can
// use same-origin relative paths (/api, /healthz) exactly as in production.
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
      '/healthz': 'http://localhost:8080',
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test-setup.ts'],
  },
})
