import test from 'node:test'
import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

// 界面文案必须是中文。品牌名、模型名、格式名与通用缩写按约定保留原文。

const viewsDir = resolve(dirname(fileURLToPath(import.meta.url)), '../src/views')
const studioDir = resolve(dirname(fileURLToPath(import.meta.url)), '../src/studio')

const allowed = new Set(['COMFYSTUDIO', 'MINIMAX H3', 'QWEN-IMAGE-2.1', 'BGM', 'SRT'])

test('主站 Vue 视图与共享组件里没有残留的英文小标题', () => {
  const offenders = []
  const dirs = [viewsDir, resolve(viewsDir, '../components')]
  for (const dir of dirs) {
    for (const file of readdirSync(dir)) {
      if (!file.endsWith('.vue')) continue
      const src = readFileSync(resolve(dir, file), 'utf8')
      for (const m of src.matchAll(/(?:overline|eyebrow)"[^>]*>\s*([A-Za-z][A-Za-z0-9 &/.-]{2,})</g)) {
        const label = m[1].trim()
        if (allowed.has(label)) continue
        offenders.push(`${file}: ${label}`)
      }
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

const timelineFiles = [
  'video-editor/lib/timeline/components/timeline-toolbar.tsx',
  'video-editor/lib/timeline/components/index.tsx',
  'video-editor/lib/timeline/components/timeline-playhead.tsx',
  'video-editor/lib/timeline/components/timeline-ruler.tsx',
  'video-editor/lib/timeline/components/timeline-element.tsx',
  'video-editor/lib/timeline/components/audio-volume-line.tsx',
  'video-editor/lib/timeline/bookmarks/components/bookmarks.tsx',
  'video-editor/lib/preview/components/context-menu.tsx',
  'video-editor/lib/preview/components/toolbar.tsx',
  'video-editor/lib/panels/assets/views/assets.tsx',
  'video-editor/lib/media/upload-toast.ts',
  'video-editor/lib/media/processing.ts',
]

const timelineBanned = [
  'Split element',
  'Delete element',
  'Duplicate element',
  'Auto snapping',
  'Ripple editing',
  'Add bookmark',
  'Timeline playhead',
  'Drag playhead',
  'Timeline ruler',
  'Switch to grid view',
  'Switch to list view',
  'Failed to copy snapshot',
  'Failed to save snapshot',
  'No media assets were uploaded',
  'Could not load media',
  'Mute track',
  'Hide track',
]

test('时间线、预览与素材列表不再出现英文界面短语', () => {
  const offenders = []
  for (const file of timelineFiles) {
    const src = readFileSync(resolve(studioDir, file), 'utf8')
    for (const phrase of timelineBanned) {
      if (src.includes(phrase)) offenders.push(`${file}: ${phrase}`)
    }
  }
  assert.deepEqual(offenders, [], `仍是英文：\n${offenders.join('\n')}`)
})

test('遮罩名称、遮罩面板与 2D 画布工具栏是中文', () => {
  const maskNames = {
    'cinematic-bars': '影院黑边',
    diamond: '菱形',
    ellipse: '椭圆',
    heart: '心形',
    rectangle: '矩形',
    split: '分割',
    star: '星形',
    text: '文字',
  }
  for (const [file, name] of Object.entries(maskNames)) {
    const src = readFileSync(
      resolve(studioDir, `video-editor/lib/masks/builtin/definitions/${file}.ts`),
      'utf8',
    )
    assert.match(src, new RegExp(`name: "${name}"`), `${file} 遮罩名不是中文`)
  }

  const masksTab = readFileSync(
    resolve(studioDir, 'video-editor/lib/masks/components/masks-tab.tsx'),
    'utf8',
  )
  for (const label of ['位置', '尺寸', '羽化', '描边', '文字内容']) {
    assert.match(masksTab, new RegExp(`SectionField label="${label}"`), `缺少 ${label}`)
  }
  assert.doesNotMatch(masksTab, /SectionField label="(Position|Size|Feather|Stroke|Content)"/)

  const toolbar = readFileSync(
    resolve(studioDir, 'pagedraw/components/ui/SideToolbar.tsx'),
    'utf8',
  )
  for (const label of ['选择与移动', '添加形状', '上传图片', '笔刷', '橡皮', '画布背景', '撤销', '重做']) {
    assert.match(toolbar, new RegExp(`label: "${label}"`), `画布工具栏缺少 ${label}`)
  }
  assert.doesNotMatch(toolbar, /label: "(Brush|Eraser|Undo|Redo|Mask)"/)
})

test('剪辑器零散文案与浏览器原生提示也是中文', () => {
  const checks = [
    ['video-editor/lib/media/processing.ts', ['不支持的文件类型：', '无法预览 ', '无法处理 ', '处理 ', '失败']],
    ['video-editor/lib/media/rehydrate.ts', ['无法恢复 ']],
    ['video-editor/lib/media/upload-toast.ts', ['没有上传任何素材']],
    ['video-editor/lib/core/managers/media-manager.ts', ['添加媒体失败']],
    ['video-editor/lib/sounds/sounds-store.ts', ['保存音效失败', '移除音效失败', '音效文件不可用']],
    ['video-editor/lib/components/ui/dialog.tsx', ['关闭']],
    ['video-editor/lib/components/ui/sheet.tsx', ['关闭']],
    ['video-editor/lib/components/ui/input.tsx', ['清空输入']],
    ['video-editor/lib/components/ui/number-field.tsx', ['拖动调整数值', '恢复默认']],
    ['video-editor/lib/components/ui/color-picker.tsx', ['选择模式', '自定义', '已收藏']],
    ['pagedraw/shims.tsx', ['确定要继续吗？']],
  ]
  for (const [file, needles] of checks) {
    const src = readFileSync(resolve(studioDir, file), 'utf8')
    for (const needle of needles) {
      assert.ok(src.includes(needle), `${file} 缺少中文文案：${needle}`)
    }
  }

  // 这几个文件里不得再出现已翻译掉的英文原文。
  const banned = {
    'video-editor/lib/media/processing.ts': ['Unsupported file type', 'Failed to process'],
    'video-editor/lib/sounds/sounds-store.ts': [': "Failed to save sound"', ': "Failed to remove sound"', '"Sound file not available"'],
    'video-editor/lib/components/ui/dialog.tsx': ['sr-only">Close'],
    'pagedraw/shims.tsx': ['Are you sure?'],
  }
  const offenders = []
  for (const [file, phrases] of Object.entries(banned)) {
    const src = readFileSync(resolve(studioDir, file), 'utf8')
    for (const phrase of phrases) {
      if (src.includes(phrase)) offenders.push(`${file}: ${phrase}`)
    }
  }
  assert.deepEqual(offenders, [], `仍是英文：\n${offenders.join('\n')}`)
})

// 第二轮中文化：空项目首页能看到的全部提示，以及各面板的次要文案。
const secondPassBanned = {
  'host/mount.tsx': ['Untitled project', 'Main scene'],
  'video-editor/lib/timeline/scenes.ts': ['Main scene', 'Cannot delete main scene'],
  'video-editor/lib/timeline/placement/main-track.ts': ['Main Track'],
  'video-editor/lib/timeline/components/timeline-track.tsx': ['Select ${track.name} track'],
  'video-editor/lib/panels/assets/views/assets.tsx': ['Sort by {sortBy}'],
  'video-editor/lib/panels/assets/drag-overlay.tsx': [
    'Drag and drop videos, photos, and audio files here',
    'Processing your files',
  ],
  'video-editor/lib/panels/properties/empty-view.tsx': [
    'Click an element on the timeline to edit its properties',
  ],
  'video-editor/lib/preview/components/toolbar.tsx': ['? "Fit" :'],
  'video-editor/lib/preview/components/context-menu.tsx': [
    'Fit to screen',
    'Full screen',
    'Save snapshot',
    'Copy snapshot',
  ],
  'video-editor/lib/components/ui/font-picker.tsx': [
    'All fonts',
    'My fonts',
    'Select a font',
    'Loading fonts',
    'Failed to load font previews',
    'No fonts found',
  ],
  'video-editor/lib/graphics/definitions/shared.ts': ['label: "Stroke align"', 'label: "Inside"'],
  'video-editor/lib/graphics/definitions/rectangle.ts': ['Corner radius', 'name: "Rectangle"'],
  'video-editor/lib/graphics/definitions/polygon.ts': ['Corner radius', 'name: "Polygon"'],
  'video-editor/lib/graphics/definitions/star.ts': ['label: "Points"', 'name: "Star"'],
  'video-editor/lib/graphics/definitions/ellipse.ts': ['name: "Ellipse"'],
  'video-editor/lib/timeline/components/graph-editor/easing-presets.ts': [
    'Ease out',
    'Ease in',
    'In out',
  ],
  'video-editor/lib/timeline/components/graph-editor/popover.tsx': ['>\n              Presets', '>\n              Saved'],
  'video-editor/lib/timeline/components/graph-editor/session.ts': [
    'Select a keyframe to edit its curve.',
    'Selected properties do not share a graph-editable channel.',
  ],
  'video-editor/lib/stickers/index.ts': ['Recently used'],
  'video-editor/lib/stickers/components/assets-view.tsx': ['See all', 'No stickers available yet'],
  'video-editor/lib/sounds/components/assets-view.tsx': [
    'Show only commercially licensed',
    'Loading sounds',
    'Loading more sounds',
    'Loading saved sounds',
    'Click the heart icon on any sound',
    'Clear all',
  ],
  'video-editor/lib/sounds/use-sound-search.ts': ['Load more failed', 'Search failed'],
  'video-editor/lib/masks/components/masks-tab.tsx': ['Only one mask is supported', 'Add mask'],
  'video-editor/lib/effects/components/effects-tab.tsx': [
    'Add effects to this layer',
    'Open effects',
  ],
  'video-editor/lib/components/editor/scenes-view.tsx': [
    'No scenes available',
    'Switch between scenes in your project',
    'Select scenes to delete',
  ],
  'video-editor/lib/components/editor/editor-error-boundary.tsx': [
    'The editor ran into a problem',
    'logged to the developer console',
  ],
  'video-editor/lib/timeline/components/index.tsx': ['Paste elements', 'Delete track'],
  'video-editor/lib/timeline/components/timeline-element.tsx': [
    'Reveal media',
    'Replace media',
  ],
  'video-editor/lib/timeline/tracks.ts': ['Video track', 'Audio track', 'Text track'],
  'video-editor/lib/timeline/components/expanded-layout.ts': ['Position X', 'Scale X', 'Corner Radius'],
  'video-editor/lib/timeline/audio-separation/index.ts': ['Extract audio', 'Recover audio'],
  'video-editor/lib/background/blur.ts': ['label: "Light"', 'label: "Heavy"'],
  'video-editor/lib/effects/definitions/blur.ts': ['name: "Blur"'],
  'video-editor/lib/stickers/providers/shapes.ts': ['name: "Square"', 'name: "Hexagon"'],
  'video-editor/lib/components/section.tsx': ['Collapse section', 'Expand section'],
  'video-editor/lib/components/editable-timecode.tsx': ['Click to edit time'],
  'video-editor/lib/panels/assets/draggable-item.tsx': ['Add to timeline or drag to position'],
  'video-editor/lib/adapters/default/download-export-sink.ts': ['Download failed'],
}

test('剪辑器第二轮中文化不留英文原文', () => {
  const offenders = []
  for (const [file, phrases] of Object.entries(secondPassBanned)) {
    const src = readFileSync(resolve(studioDir, file), 'utf8')
    for (const phrase of phrases) {
      if (src.includes(phrase)) offenders.push(`${file}: ${phrase}`)
    }
  }
  assert.deepEqual(offenders, [], `仍是英文：\n${offenders.join('\n')}`)

  // 单个单词的按钮文案用整行匹配，避免把 import 语句、组件名当成文案。
  const lineChecks = [
    ['video-editor/lib/panels/assets/views/assets.tsx', [/^\s+Browse$/m, /^\s+Import$/m]],
    ['video-editor/lib/components/ui/font-picker.tsx', [/^\s+Retry$/m]],
    ['video-editor/lib/components/editor/editor-error-boundary.tsx', [/^\s+Reload editor$/m]],
  ]
  const lineOffenders = []
  for (const [file, patterns] of lineChecks) {
    const src = readFileSync(resolve(studioDir, file), 'utf8')
    for (const pattern of patterns) {
      if (pattern.test(src)) lineOffenders.push(`${file}: ${pattern}`)
    }
  }
  assert.deepEqual(lineOffenders, [], `仍是英文：\n${lineOffenders.join('\n')}`)
})