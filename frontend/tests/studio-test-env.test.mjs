import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync, existsSync } from 'node:fs'

// 移植自 opencut-classic / ArtCraft 的三个 spec 用 vitest 写成，过去没有任何
// 执行环境，等于永远绿。这里锁住「环境还在、还挂在默认 test 上」，
// 避免有人把 script 改回只跑 node:test 而让它们悄悄失效。

const pkg = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8'))

test('默认 npm test 同时执行 node:test 与 vitest', () => {
  assert.match(pkg.scripts.test, /node --test tests\/\*\.test\.mjs/, 'node:test 套件不能丢')
  assert.match(pkg.scripts.test, /vitest run/, 'vitest 必须挂在默认 test 上，否则 spec 又会失活')
  assert.equal(pkg.scripts['test:unit'], 'node --test tests/*.test.mjs', '需要一个只跑 node:test 的快通道')
  assert.ok(pkg.devDependencies.vitest, '缺 vitest 依赖')
  assert.ok(pkg.devDependencies.jsdom, '缺 jsdom 依赖')
})

test('vitest 配置覆盖 studio 的 spec 文件并使用 jsdom', () => {
  const configPath = new URL('../vitest.config.js', import.meta.url)
  assert.ok(existsSync(configPath), '缺 vitest.config.js')
  const config = readFileSync(configPath, 'utf8')
  assert.match(config, /environment:\s*['"]jsdom['"]/)
  assert.match(config, /src\/studio\/\*\*\/\*\.spec\.ts/, 'include 没覆盖 studio 的 spec')
  assert.match(config, /vite-plugin-wasm|wasm\(\)/, '缺 wasm 插件，opencut-wasm 的 top-level await 会失败')
})

test('三个移植 spec 仍然存在', () => {
  const specs = [
    '../src/studio/pagedraw/utilities/render3DModel.spec.ts',
    '../src/studio/video-editor/lib/core/__tests__/editor-core-smoke.spec.ts',
    '../src/studio/video-editor/lib/services/storage/__tests__/serialization.spec.ts',
  ]
  for (const spec of specs) {
    assert.ok(existsSync(new URL(spec, import.meta.url)), `${spec} 不见了`)
  }
})