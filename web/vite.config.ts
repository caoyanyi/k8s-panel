import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:8080',
      '/healthz': 'http://127.0.0.1:8080',
      '/readyz': 'http://127.0.0.1:8080',
    },
  },
  test: {
    include: ['src/**/*.test.{ts,tsx}'],
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    restoreMocks: true,
    maxWorkers: 2,
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json-summary'],
      include: ['src/**/*.{ts,tsx}'],
    },
  },
})
