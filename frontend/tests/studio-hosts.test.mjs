import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const read = (p) => readFileSync(new URL(p, import.meta.url), 'utf8')
const draw = read('../src/views/StudioDraw.vue')
const scene = read('../src/views/StudioScene.vue')
const video = read('../src/views/StudioVideo.vue')
const router = read('../src/router/index.js')
const app = read('../src/App.vue')

// 画布与 3D 页自己的顶栏是 fixed top-0 h-14，站点导航是 sticky z-index:100。
// 宿主层叠低于导航时，返回/上传/生成这些按钮会被牢牢盖住，页面等于不可用。
test('三个 studio 宿主都全屏接管，不被站点导航遮住', () => {
  for (const [name, source] of [['StudioDraw', draw], ['StudioScene', scene], ['StudioVideo', video]]) {
    assert.ok(/\.studio-[a-z0-9-]+(?:_[a-z0-9]+)?\s*\{/.test(source), `${name} 缺少宿主样式`)
    assert.match(source, /position:\s*fixed/, `${name} 宿主必须固定定位`)
    assert.match(source, /inset:\s*0/, `${name} 宿主必须铺满视口`)
    const values = [...source.matchAll(/z-index:\s*(\d+)\s*;/g)].map((match) => Number(match[1]))
    assert.ok(values.length > 0, `${name} 缺少 z-index 声明`)
    assert.deepEqual([...new Set(values)], [1000], `${name} 宿主 z-index 必须是 1000（导航是 100）`)
  }
})

test('动态 import 未完成就离开路由时不得挂载', () => {
  const mounts = [
    ['StudioDraw', draw, 'mountPageDraw'],
    ['StudioScene', scene, 'mountScene3D'],
    ['StudioVideo', video, 'mountVideoEditor'],
  ]
  for (const [name, source, mount] of mounts) {
    assert.match(source, /let unmounted = false/, `${name} 缺少卸载标记`)
    assert.match(
      source,
      new RegExp(`await import\\([^)]*\\)[\\s\\S]{0,400}?if \\(unmounted \\|\\| ![A-Za-z.]+value\\) return[\\s\\S]{0,120}?${mount}\\(`),
      `${name} 必须在 import 之后、mount 之前检查是否已卸载`,
    )
    assert.match(source, /onBeforeUnmount\(\(\) => \{[\s\S]{0,120}?unmounted = true/, `${name} 卸载时必须置位`)
  }
})

test('三条 studio 路由与导航入口都已登记', () => {
  for (const path of ['/studio/video/:projectId?', '/studio/draw', '/studio/scene']) {
    assert.ok(router.includes(path), `缺少路由 ${path}`)
  }
  for (const label of ['视频剪辑', '创作画布', '3D 场景']) {
    assert.ok(app.includes(label), `导航缺少 ${label}`)
  }
})

// preflight 关闭后，裸 <button> 会保留浏览器浅色 buttonface 底；编辑器里的
// 描边按钮就会白底白字，标签看不见。
test('studio 主题重置了裸 button 的默认底色', () => {
  const css = read('../src/studio/styles/studio.css')
  assert.match(
    css,
    /\.studio-root button,[\s\S]{0,200}?background-color:\s*transparent/,
    '缺少 button 底色重置，深色工具栏会出现白底白字',
  )
})