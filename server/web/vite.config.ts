import { fileURLToPath, URL } from 'node:url'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

const GO_SERVER_ORIGIN = process.env.HHO_DEV_SERVER ?? 'http://localhost:7745'

export default defineConfig({
  base: '/',
  plugins: [vue()],
  publicDir: 'public',
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
    target: 'es2022',
    chunkSizeWarningLimit: 500,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: GO_SERVER_ORIGIN,
        changeOrigin: false,
      },
    },
  },
})
