import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

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