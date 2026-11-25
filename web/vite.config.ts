import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import AutoImport from 'unplugin-auto-import/vite'
import { moduleFilter } from './plugins/module-filter'

// 开发期把 /api 代理到本地 Go 服务，生产期由 Go 直接托管 dist 产物。
export default defineConfig({
  plugins: [
    // 必须排在 vue 之前：先把未启用模块的入口换成空模块，再交给后续编译。
    moduleFilter(),
    vue(),
    AutoImport({
      imports: ['vue', 'vue-router', 'pinia'],
      dts: 'auto-imports.d.ts',
    }),
  ],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://127.0.0.1:9000', changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 1500,
    rollupOptions: {
      output: {
        // Vite 8 使用 Rolldown，manualChunks 仅支持函数形式
        manualChunks(id: string) {
          if (!id.includes('node_modules')) return undefined
          if (id.includes('element-plus')) return 'element'
          return 'vendor'
        },
      },
    },
  },
})
