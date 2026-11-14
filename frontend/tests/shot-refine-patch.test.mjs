import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { refineFieldLabel, refineRationaleText, selectedRefineEdits, refineStateLabel } from '../src/utils/shotRefinePatch.js'

test('精修补丁只提交勾选的修改，并把理由拼成可读文本', () => {
  const edits = [{ field: 'blocking' }, { field: 'reaction' }, { field: 'prompt_camera' }]
  assert.deepEqual(selectedRefineEdits(edits, [2, 0]), [edits[0], edits[2]])
  assert.deepEqual(selectedRefineEdits(edits, []), [])
  assert.equal(refineRationaleText({ issue: '距离感', problem: ' ', change_reason: '拉近', result_reason: '' }), '问题：距离感；改法：拉近')
  assert.equal(refineFieldLabel('eye_line'), '视线')
  assert.equal(refineFieldLabel('unknown'), 'unknown')
  assert.equal(refineStateLabel('rolled_back'), '已撤销')
})

test('镜头编辑器提供精修面板，应用前须逐条勾选且附源哈希', () => {
  const source = readFileSync(new URL('../src/components/ShotDirectorEditor.vue', import.meta.url), 'utf8')
  assert.match(source, /补丁式镜头精修/)
  assert.match(source, /v-model="refineSelected" type="checkbox"/)
  assert.match(source, /source_hash: refineSourceHash\.value/)
  assert.match(source, /rollbackRefinePatch\(item\)/)
})
