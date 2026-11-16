import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const editor = readFileSync(new URL('../src/views/ProjectEditor.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('链式预览接口只读 GET', () => {
  assert.match(api, /episodeContinuityChain: \(id, number\) => http\.get\(`\/projects\/\$\{id\}\/episodes\/\$\{number\}\/continuity\/chain`\)/)
})

test('连续性页展示本集链式预览，按显式来源、不提交生成', () => {
  assert.match(editor, /<h2>本集链式预览<\/h2>/)
  assert.match(editor, /只按各 Scene 显式设置的连续性来源排出等待关系，不按顺序或名称推断；只读预览，不提交任何生成。/)
  assert.match(editor, /现在可提交：/)
  assert.match(editor, /waiting_source: '等待上游'/)
  assert.match(editor, /invalid_source: '来源无效'/)
  assert.match(editor, /@click="selectChainScene\(sid\)"/)
  // 换集后丢弃旧响应，刷新连续性时一并刷新链式预览
  assert.match(editor, /if \(key === `\$\{id\(\)\}:\$\{activeEpN\.value\}`\) chainPlan\.value = data/)
  assert.match(editor, /catch \{ continuity\.value = null \}\n  await loadContinuityChain\(\)/)
  const chainBlock = editor.slice(editor.indexOf('async function loadContinuityChain'), editor.indexOf('function selectChainScene'))
  assert.doesNotMatch(chainBlock, /api\.(generate|submit|start|configure)/i)
})
