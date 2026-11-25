import { readFileSync } from 'node:fs'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import react from '@vitejs/plugin-react'
import wasm from 'vite-plugin-wasm'

const apiTarget = process.env.VITE_API_PROXY_TARGET || 'http://127.0.0.1:18000'

export default defineConfig({
  // Vue runs the app shell; React only mounts the ArtCraft video editor
  // (src/studio). opencut-wasm ships a .wasm asset and top-level await;
  // build.target=esnext keeps that native, so no TLA transform is needed.
  plugins: [vue(), react(), wasm(), {
    name: 'studio-third-party-notices',
    apply: 'build',
    generateBundle() {
      // Keep one authoritative source; dist is also the input to Go's embedded static bundle.
      this.emitFile({ type: 'asset', fileName: 'THIRD_PARTY_NOTICES.md', source: readFileSync(new URL('./src/studio/THIRD_PARTY_NOTICES.md', import.meta.url), 'utf8') })
    }
  }],
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
