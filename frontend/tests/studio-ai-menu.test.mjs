import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')

// 顶栏一行塞不下：AI 三件套收进「AI 生成」下拉，其余动作留在外面。
const detailsOpen = stage.indexOf('<details ref={aiMenuRef}')
const detailsShut = stage.indexOf('</details>', detailsOpen)

test('顶栏有一个「AI 生成」下拉', () => {
  assert.ok(detailsOpen >= 0, '缺少 details 容器')
  assert.ok(detailsShut > detailsOpen, 'details 没有闭合')
  assert.match(stage.slice(detailsOpen, detailsShut), /AI 生成 ▾/, 'summary 文案应可辨认')
})

test('三件套在面板里，导出/存档留在外面', () => {
  const panel = stage.slice(detailsOpen, detailsShut)
  for (const label of ['AI 图生 3D', 'AI 图生 3D 世界', '身份迁移出图']) {
    assert.ok(panel.includes(label), `${label} 应在面板内`)
  }
  assert.match(panel, /value=\{splatMatteEngine\}/, '抠图下拉应与图生世界同处面板')
  assert.match(panel, /id="identity-prompt"/, '身份迁移提示词输入框应在面板内')
  const after = stage.slice(detailsShut)
  const before = stage.slice(0, detailsOpen)
  for (const label of ['导出 GLB', '存入素材库', '保存场景', '载入场景']) {
    assert.ok(after.includes(label), `${label} 应留在面板外`)
  }
  assert.ok(before.includes('导入模型'), '导入模型留在面板左边')
})

test('点动作先收起面板，再开文件选择器', () => {
  assert.match(stage, /const closeAiMenu = useCallback\(/, '缺少收起工具函数')
  for (const ref of ['imageInputRef', 'splatInputRef', 'identityInputRef']) {
    const call = new RegExp(`closeAiMenu\\(\\);\\s*${ref}\\.current\\?\\.click\\(\\);`)
    assert.match(stage, call, `${ref} 应先收起面板`)
  }
})

test('点面板外面也收起，否则下拉会一直盖着画布', () => {
  assert.match(stage, /onPointerDownCapture=\{/)
  assert.match(stage, /!aiMenuRef\.current\.contains\(target\)/)
  assert.match(stage, /aiMenuRef\.current\.open = false;/)
})