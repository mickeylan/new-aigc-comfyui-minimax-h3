import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import wasm from 'vite-plugin-wasm'

// 这些 spec 来自移植代码（opencut-classic / ArtCraft），用 vitest 的 API 写成，
// 一直没有执行环境。这里单独给它们一个 jsdom 环境，与应用自身的 node:test
// 套件（tests/*.test.mjs）分开跑，互不影响。
export default defineConfig({
  plugins: [react(), wasm()],
  resolve: {
    dedupe: ['react', 'react-dom']
  },
  test: {
    environment: 'jsdom',
    include: ['src/studio/**/*.spec.ts', 'src/studio/**/*.spec.tsx'],
    // 这些 spec 依赖 WebGL / Canvas，jsdom 没有实现，用例内部自行 mock。
    globals: true
  }
})