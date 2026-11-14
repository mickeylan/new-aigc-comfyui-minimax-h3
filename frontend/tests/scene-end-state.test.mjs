import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

test('站位弹窗提供本场结尾人物状态编辑，提取按钮在 Skill 未启用时禁用', () => {
  const source = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
  assert.match(source, /setBlockingDraft\(value\); loadEndState\(\)/)
  assert.match(source, /:disabled="endStateBusy \|\| !endState\.skillReady"/)
  assert.match(source, /if \(blockingScene\.value\?\.id === sceneId\) applyEndStateView\(data\)/)
  assert.match(source, /只延续伤势/)
  const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')
  assert.match(api, /scenes\/\$\{sid\}\/end-state`, state\)/)
  assert.match(api, /scenes\/\$\{sid\}\/end-state\/extract`/)
})
