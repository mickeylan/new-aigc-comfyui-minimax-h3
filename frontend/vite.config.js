import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import react from '@vitejs/plugin-react'
import wasm from 'vite-plugin-wasm'

const apiTarget = process.env.VITE_API_PROXY_TARGET || 'http://127.0.0.1:18000'

export default defineConfig({
  // Vue runs the app shell; React only mounts the ArtCraft video editor
  // (src/studio). opencut-wasm ships a .wasm asset and top-level await;
  // build.target=esnext keeps that native, so no TLA transform is needed.
  plugins: [vue(), react(), wasm()],
  resolve: {
    dedupe: ['react', 'react-dom']
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: apiTarget,
        changeOrigin: true,
        ws: true
      }
    }
  },
  build: {
    outDir: 'dist',
    // opencut-wasm initializes through top-level await.
    target: 'esnext',
    chunkSizeWarningLimit: 2000
  }
})
