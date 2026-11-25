import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const root = new URL('../src/studio/', import.meta.url)
const read = (rel) => readFileSync(new URL(rel, root), 'utf8')

test('第三方声明覆盖三个移植目录与全部随包依赖', () => {
  const notices = read('THIRD_PARTY_NOTICES.md')

  // 移植来源：少了任何一条都意味着许可证声明不完整。
  assert.match(notices, /opencut-classic/, '缺少 opencut-classic 的移植声明')
  assert.match(notices, /opencut-classic[\s\S]{0,200}?MIT|MIT[\s\S]{0,200}?opencut-classic/)
  assert.match(notices, /ArtCraft/, '缺少 ArtCraft 的移植声明')
  assert.match(notices, /Apache-2\.0/)

  // 运行时依赖：这几个是真正的许可证义务（MPL 与 ISC 都不能只靠 MIT 根许可覆盖）。
  for (const dep of ['mediabunny', 'opencut-wasm', 'three', 'lucide-react', '@hugeicons/core-free-icons']) {
    assert.ok(notices.includes(dep), `声明里缺少依赖 ${dep}`)
  }
  assert.match(notices, /MPL-2\.0/, 'mediabunny 是 MPL-2.0，必须写明')
  assert.match(notices, /ISC/, 'lucide-react 是 ISC，必须写明')
  // Hugeicons 的 React 渲染器包属于 Pro 条款，声明里必须区分清楚，避免误以为全部可自由分发。
  assert.match(notices, /@hugeicons\/react[\s\S]{0,120}?Pro/, 'Hugeicons Pro 条款未区分')
})

test('Toonflow登记逐项保留MIT版权、动作CC0与BSD/MIT依赖，不把未迁入引擎当分发内容', () => {
  const notices = read('THIRD_PARTY_NOTICES.md')
  assert.match(notices, /scene3d\/toonflow\/[\s\S]{0,300}HBAI-Ltd/)
  assert.match(notices, /Copyright \(c\) 2026 HBAI-Ltd[\s\S]*Permission is hereby granted/)
  assert.match(notices, /characterAnimationData\.json[\s\S]*quaternius\.com\/packs\/universalanimationlibrary\.html/)
  assert.match(notices, /CC0 1\.0 Universal[\s\S]{0,200}不把动作数据重新授权/)
  assert.match(notices, /Copyright \(c\) 2026 Matthew Kissinger[\s\S]*Permission is hereby granted/)
  assert.match(notices, /SunCalc 2\.1\.1（BSD-2-Clause）/)
  assert.match(notices, /Zod[\s\S]*MIT/)
  assert.match(notices, /未迁入 Holodeck examples 或 fzstd 解压器/)
  assert.match(notices, /Kiln[\s\S]{0,80}不随本次迁移打包/)
})

test('生产构建原样携带唯一来源声明，不依赖当前工作目录或维护副本', async () => {
  // Load exactly as Vite does; its loader also resolves config-relative import.meta.url.
  const { loadConfigFromFile } = await import('vite')
  const loaded = await loadConfigFromFile({ command: 'build', mode: 'production' }, fileURLToPath(new URL('../vite.config.js', import.meta.url)))
  assert.ok(loaded, 'Vite未能加载生产配置')
  const { config } = loaded
  const plugin = config.plugins.find(item => item?.name === 'studio-third-party-notices')
  assert.ok(plugin, '缺少声明分发插件')
  assert.equal(plugin.apply, 'build')
  const emitted = []
  plugin.generateBundle.call({ emitFile: asset => emitted.push(asset) })
  assert.equal(emitted.length, 1)
  assert.equal(emitted[0].type, 'asset')
  assert.equal(emitted[0].fileName, 'THIRD_PARTY_NOTICES.md')
  assert.equal(emitted[0].source, read('THIRD_PARTY_NOTICES.md'))
  assert.ok(emitted[0].source.includes('Copyright (c) 2026 HBAI-Ltd'))
  assert.ok(emitted[0].source.includes('Copyright 2025-2026 OpenCut'))
})

test('三个移植入口都指向声明文件', () => {
  const entries = {
    'video-editor/index.ts': 'opencut-classic',
    'pagedraw/adapter.ts': 'ArtCraft',
    'scene3d/Scene3DStage.tsx': 'ArtCraft',
  }
  for (const [file, upstream] of Object.entries(entries)) {
    const head = read(file).split('\n').slice(0, 4).join('\n')
    assert.ok(head.includes('THIRD_PARTY_NOTICES.md'), `${file} 的开头没有指向声明文件`)
    assert.ok(head.includes(upstream), `${file} 的开头没有写明移植来源`)
  }
})

test('声明文件列出的依赖确实在 package.json 里', () => {
  const pkg = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8'))
  const deps = { ...pkg.dependencies, ...pkg.devDependencies }
  for (const dep of ['mediabunny', 'opencut-wasm', 'three', 'lucide-react', '@hugeicons/core-free-icons']) {
    assert.ok(deps[dep], `package.json 里没有 ${dep}，声明与依赖不一致`)
  }
})