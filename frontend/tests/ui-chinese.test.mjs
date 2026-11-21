import test from 'node:test'
import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

// 界面文案必须是中文。品牌名、模型名、格式名与通用缩写按约定保留原文。

const viewsDir = resolve(dirname(fileURLToPath(import.meta.url)), '../src/views')
const studioDir = resolve(dirname(fileURLToPath(import.meta.url)), '../src/studio')

const allowed = new Set(['COMFYSTUDIO', 'MINIMAX H3', 'QWEN-IMAGE-2.1', 'BGM', 'SRT'])

test('主站 Vue 视图里没有残留的英文小标题', () => {
  const offenders = []
  for (const file of readdirSync(viewsDir)) {
    if (!file.endsWith('.vue')) continue
    const src = readFileSync(resolve(viewsDir, file), 'utf8')
    for (const m of src.matchAll(/(?:overline|eyebrow)"[^>]*>\s*([A-Za-z][A-Za-z0-9 &/.-]{2,})</g)) {
      const label = m[1].trim()
      if (allowed.has(label)) continue
      offenders.push(`${file}: ${label}`)
    }
  }
  assert.deepEqual(offenders, [], `仍是英文的小标题：\n${offenders.join('\n')}`)
})

// 剪辑器外壳已翻译的文件：出现下列英文界面短语即视为回退。
const shellFiles = [
  'video-editor/lib/components/editor/editor-header.tsx',
  'video-editor/lib/components/editor/mobile-gate.tsx',
  'video-editor/lib/components/editor/onboarding.tsx',
  'video-editor/lib/project/components/rename-project-dialog.tsx',
  'video-editor/lib/project/components/delete-project-dialog.tsx',
  'video-editor/lib/actions/shortcuts-dialog.tsx',
]

const bannedPhrases = [
  'Export project',
  'Exporting project',
  'Save to disk',
  'Save to my media library',
  'Pick at least one destination',
  'Export finished with errors',
  'Rename project',
  'Enter a new name',
  'Keyboard shortcuts',
  'Reset to default',
  'Click to edit shortcut',
  'Desktop only',
  'Take a look anyway',
  'Go back',
  'Welcome to the editor',
  'Please try again',
]

test('剪辑器外壳不再出现英文界面短语', () => {
  const offenders = []
  for (const file of shellFiles) {
    const src = readFileSync(resolve(studioDir, file), 'utf8')
    for (const phrase of bannedPhrases) {
      if (src.includes(phrase)) offenders.push(`${file}: ${phrase}`)
    }
  }
  assert.deepEqual(offenders, [], `仍是英文：\n${offenders.join('\n')}`)
})

test('素材面板分类页签是中文', () => {
  const src = readFileSync(
    resolve(studioDir, 'video-editor/lib/panels/assets/assets-panel-store.tsx'),
    'utf8',
  )
  for (const label of ['素材', '音效', '文字', '贴纸', '特效', '字幕', '设置']) {
    assert.match(src, new RegExp(`label: "${label}"`), `缺少中文页签 ${label}`)
  }
  assert.doesNotMatch(src, /label: "(Media|Sounds|Text|Stickers|Effects|Captions|Settings)"/)
})

test('属性面板页签与参数名是中文', () => {
  const tabs = readFileSync(
    resolve(studioDir, 'video-editor/lib/panels/properties/registry.tsx'),
    'utf8',
  )
  for (const label of ['变换', '混合', '音频', '速度', '遮罩', '特效', '文字', '图形']) {
    assert.match(tabs, new RegExp(`label: "${label}"`), `缺少中文页签 ${label}`)
  }
  assert.doesNotMatch(
    tabs,
    /label: "(Transform|Blending|Audio|Speed|Masks|Effects|Text|Graphic)"/,
  )

  const params = readFileSync(
    resolve(studioDir, 'video-editor/lib/params/registry.ts'),
    'utf8',
  )
  for (const banned of ['Opacity', 'Blend Mode', 'Position X', 'Font Family', 'Line Height']) {
    assert.ok(!params.includes(`label: "${banned}"`), `参数名仍是英文：${banned}`)
  }
  assert.match(params, /label: "不透明度"/)
})