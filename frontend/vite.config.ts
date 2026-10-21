import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// Wails dev 会把前端 dev server 地址注入 WebView；这里不需要额外配置。
export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})
