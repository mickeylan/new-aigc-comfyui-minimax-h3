import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

// 气泡提示 / 下拉 / 弹窗的可见性回归测试。
//
// 背景（2026-11-22）：剪辑器把整个工作台挂成 z-index:1000 的全屏定位容器，
// 而 Radix 的弹层是挂在 <body> 下的（z-index 继承自内容上的 z-50/z-250），
// 于是所有气泡、下拉、弹窗都被工作台压住，用户看到的是「弹窗里没有文字」。
// Radix 还会把内容的 computed z-index 复制成 popper 包裹层的行内样式，
// 所以覆盖规则必须带 !important。

const studioDir = resolve(dirname(fileURLToPath(import.meta.url)), '../src/studio')
const css = readFileSync(resolve(studioDir, 'styles/studio.css'), 'utf8')

test('挂到 body 的弹层被抬到工作台之上，且能盖住 Radix 的行内 z-index', () => {
  assert.match(
    css,
    /body\.studio-theme \[data-radix-popper-content-wrapper\]\s*\{[^}]*z-index:\s*1200\s*!important/,
    'popper 包裹层必须用 !important 抬到工作台之上',
  )
  assert.match(
    css,
    /body\.studio-theme > \[data-state\]\s*\{[^}]*z-index:\s*1200/,
    '遮罩与弹窗内容挂在 body 下，也要抬升',
  )
  // 工作台自身的层级（画布/3D 是 100，剪辑器是 1000）必须低于弹层。
  const stageZ = readFileSync(resolve(studioDir, 'scene3d/Scene3DStage.tsx'), 'utf8')
  assert.match(stageZ, /z-\[100\]/, '3D 工作台仍是 z-[100]，弹层 1200 在其之上')
  const videoPage = readFileSync(resolve(studioDir, '../views/StudioVideo.vue'), 'utf8')
  assert.match(videoPage, /z-index:\s*1000/, '剪辑器仍是 z-index:1000，弹层 1200 在其之上')
})

test('气泡与弹窗用的是工作台自己的深色 token，不依赖系统浅/深色', () => {
  const tooltip = readFileSync(
    resolve(studioDir, 'video-editor/lib/components/ui/tooltip.tsx'),
    'utf8',
  )
  const sidebar = tooltip.match(/sidebar:\s*\n?\s*"([^"]+)"/)
  assert.ok(sidebar, 'tooltip 仍有 sidebar 变体')
  assert.match(sidebar[1], /bg-popover/, 'sidebar 气泡底色必须用 popover token')
  assert.match(sidebar[1], /text-popover-foreground/, 'sidebar 气泡文字必须用 popover-foreground token')
  assert.doesNotMatch(sidebar[1], /bg-white/, '浅色系统下侧栏气泡会变成白底、文字看不见')
  assert.doesNotMatch(sidebar[1], /dark:/, '工作台固定深色，不能用 dark: 前缀随系统切换')

  const popover = readFileSync(
    resolve(studioDir, 'video-editor/lib/components/ui/popover.tsx'),
    'utf8',
  )
  assert.match(popover, /bg-popover text-popover-foreground/, 'PopoverContent 需要同时给出底色与文字色')

  const select = readFileSync(
    resolve(studioDir, 'video-editor/lib/components/ui/select.tsx'),
    'utf8',
  )
  assert.match(select, /bg-popover text-popover-foreground/, 'SelectContent 需要同时给出底色与文字色')
})