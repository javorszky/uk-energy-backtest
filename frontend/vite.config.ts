import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  build: {
    // Output goes into the Go embed package, not frontend/dist.
    outDir: '../internal/ui/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      // Overridable so docker compose can point at the backend service name
      // while bare-metal dev keeps localhost.
      '/api': process.env.VITE_PROXY_TARGET ?? 'http://localhost:8080',
    },
  },
  test: {
    environment: 'jsdom',
    passWithNoTests: true,
    coverage: {
      provider: 'v8',
      reporter: ['lcov'],
      exclude: ['node_modules/', 'dist/', '**/*.config.*'],
    },
  },
})
