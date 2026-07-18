import { execSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

// Build-time version metadata baked into the bundle. In Docker the three VITE_*
// values arrive as build args (set from oglimmer.sh / the release workflow);
// for a plain local build they fall back to package.json and `git`.
const pkg = JSON.parse(readFileSync(new URL('./package.json', import.meta.url), 'utf-8'))
const appVersion = process.env.VITE_APP_VERSION || pkg.version || 'dev'
let gitCommit = process.env.VITE_GIT_COMMIT || ''
if (!gitCommit) {
  try {
    gitCommit = execSync('git rev-parse --short HEAD').toString().trim()
  } catch {
    gitCommit = 'unknown'
  }
}
const buildTime = process.env.VITE_BUILD_TIME || new Date().toISOString()

// In dev, the Vite server proxies API calls to the Go backend so the SPA can
// use same-origin relative paths (/api, /healthz) exactly as in production.
export default defineConfig({
  plugins: [vue()],
  define: {
    __APP_VERSION__: JSON.stringify(appVersion),
    __GIT_COMMIT__: JSON.stringify(gitCommit),
    __BUILD_TIME__: JSON.stringify(buildTime),
  },
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
