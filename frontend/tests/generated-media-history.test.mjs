import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const drawer = readFileSync(new URL('../src/components/GeneratedMediaHistoryDrawer.vue', import.meta.url), 'utf8')
const editor = readFileSync(new URL('../src/views/ProjectEditor.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('统一生成历史支持筛选预览和安全批量删除', () => {
  assert.match(editor, /生成历史管理/)
  assert.match(editor, /GeneratedMediaHistoryDrawer/)
  assert.match(drawer, /标准照/)
  assert.match(drawer, /四视图/)
  assert.match(drawer, /道具\/场景/)
  assert.match(drawer, /分镜图\/视频/)
  assert.match(drawer, /delete_blocked_reason/)
  assert.match(drawer, /删除所选/)
  assert.match(drawer, /设为当前最佳/)
  assert.match(drawer, /收藏保留/)
  assert.match(api, /generatedMedia:/)
  assert.match(api, /bulkDeleteGeneratedMedia:/)
})
