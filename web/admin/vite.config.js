import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import compression from 'vite-plugin-compression'

export default defineConfig({
  plugins: [vue(), compression({ algorithm: 'brotliCompress', ext: '.br', threshold: 0 })],
  base: '/_admin/',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks: {
          codemirror: ['codemirror', '@codemirror/lang-json'],
        },
      },
    },
  },
  server: {
    proxy: {
      '/_admin/api': 'http://localhost:8080',
      '/api': 'http://localhost:8080',
    },
  },
})
