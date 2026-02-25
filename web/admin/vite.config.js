import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
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
