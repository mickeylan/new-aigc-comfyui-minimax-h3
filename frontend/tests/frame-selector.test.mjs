import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'

const source = fs.readFileSync(new URL('../src/components/FrameSelector.vue', import.meta.url), 'utf8')

test('输入连续性提取成功后自动选择最接近结尾的候选帧', () => {
  assert.match(source, /const last = frames\.value\[frames\.value\.length - 1\]/)
  assert.match(source, /api\.selectFrame\(props\.projectId, props\.sourceSceneId, Number\(last\.id\)\)/)
  assert.match(source, /selectedFrameId\.value = Number\(selected\.id\)/)
})

test('输入连续性没有候选帧ID时不能保存或提交空frame_id', () => {
  assert.match(source, /!sourceSceneId \|\| !selectedFrameId/)
  assert.match(source, /候选帧ID缺失，请重新提取上一镜末尾帧/)
  assert.match(source, /type="button" class="frame"/)
})
