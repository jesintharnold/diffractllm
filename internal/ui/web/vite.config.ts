import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// Builds into ../dist, which the Go package embeds. In dev, API calls go to the gateway.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': path.resolve(import.meta.dirname, './src') },
  },
  server: {
    proxy: {
      '/v1': 'http://localhost:8085',
      '/ready': 'http://localhost:8085',
    },
  },
  build: {
    outDir: '../dist',
    emptyOutDir: true,
  },
})
