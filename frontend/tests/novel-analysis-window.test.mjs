import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const view = readFileSync(new URL('../src/views/NovelImport.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')
const bible = readFileSync(new URL('../src/views/StoryBible.vue', import.meta.url), 'utf8')

test('长篇小说按十章窗口滚动分析并展示实时进度', () => {
  assert.match(view, /滚动分析窗口/)
  assert.match(view, /默认分析10章/)
  assert.match(view, /progressPercent/)
  assert.match(view, /progress-track/)
  assert.match(view, /正在分析第/)
  assert.match(view, /setTimeout/)
  assert.match(view, /生成本窗口故事弧/)
  assert.match(view, /完成并推进下一窗口/)
  assert.match(api, /analysisWindowSuggestion:/)
  assert.match(api, /analysisWindowProgress:/)
  assert.match(api, /advanceAnalysisWindow:/)
  assert.match(view, /生成Story Bible增量/)
  assert.match(api, /proposeStoryBibleChange:/)
  assert.match(bible, /待审核增量建议/)
  assert.match(bible, /审核并合并/)
  assert.match(bible, /还没有可审核的故事圣经/)
  assert.match(bible, /生成基础故事圣经/)
  assert.match(bible, /草稿为空或内容不完整，不能审核/)
  assert.match(view, /生成故事圣经 →/)
  assert.match(view, /创建本窗口生产批次/)
  assert.match(view, /状态快照/)
  assert.match(view, /失败章节/)
  assert.match(view, /扩展至第/)
})
