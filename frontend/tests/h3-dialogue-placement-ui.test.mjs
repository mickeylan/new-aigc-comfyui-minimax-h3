import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')

function extractExpression(name, nextMarker) {
  const start = detail.indexOf(`const ${name} =`)
  assert.notEqual(start, -1, `${name} must exist`)
  const end = detail.indexOf(nextMarker, start)
  assert.notEqual(end, -1, `${name} end marker must exist`)
  return detail.slice(start, end)
}

test('H3 对白定位结果在视频提示词弹窗中清楚展示且不设门禁', () => {
  assert.match(detail, /当前保存稿尚未完整编排以下对白；点击“AI 重新生成”，系统会结合剧情自动安排/)
  assert.doesNotMatch(detail, /请确认时间或手动放置/)
  assert.match(detail, /item\.character \|\| '未知说话人'/)
  assert.match(detail, /「\{\{ item\.text \}\}」/)
  assert.match(detail, /item\.reason/)
  assert.match(detail, /已定位对白（/)
  assert.match(detail, /→ Shot \{\{ item\.shot_number \}\}/)
  assert.match(detail, /item\.time_segment/)
  assert.match(detail, /无需维护内部对白映射或手工填写时间/)
  assert.match(detail, /重新生成会先补齐并核对对白，再返回完整预览/)
  assert.doesNotMatch(detail, /:disabled="[^"]*videoTaskDialoguePlacement/)
})

test('GET 和异步重新生成均按 epoch 后写入定位状态，并在场景生命周期重置', () => {
  assert.match(detail, /videoTaskDialoguePlacement\.value = normalizeDialoguePlacement\(preview\.dialogue_placement\)/)
  assert.match(detail, /videoTaskDialoguePlacement\.value = normalizeDialoguePlacement\(data\.dialogue_placement\)/)
  assert.match(detail, /if \(epoch !== videoPromptRequestEpoch \|\| videoTaskScene\.value\?\.id !== sceneId\) return[\s\S]*videoTaskDialoguePlacement\.value = normalizeDialoguePlacement\(data\.dialogue_placement\)/)
  assert.match(detail, /function closeVideoPrompt\(force = false\)[\s\S]*videoTaskDialoguePlacement\.value = emptyDialoguePlacement\(\)/)
  assert.match(detail, /async function viewVideoPrompt\(sc\)[\s\S]*videoTaskDialoguePlacement\.value = emptyDialoguePlacement\(\)/)
})

test('定位规范化函数真实处理缺失和畸形响应', () => {
  const source = `${extractExpression('emptyDialoguePlacement', 'const normalizeDialoguePlacement')}\n${extractExpression('normalizeDialoguePlacement', 'const videoTaskDialoguePlacement')}; return { emptyDialoguePlacement, normalizeDialoguePlacement }`
  const { emptyDialoguePlacement, normalizeDialoguePlacement } = new Function(source)()
  assert.deepEqual(emptyDialoguePlacement(), { unplaced: [], placed: [] })
  assert.deepEqual(normalizeDialoguePlacement(null), { unplaced: [], placed: [] })
  assert.deepEqual(normalizeDialoguePlacement({ unplaced: 'bad', placed: null }), { unplaced: [], placed: [] })
  const payload = { unplaced: [{ dialogue_id: 7, character: '阿青', text: '等等', reason: '无时间' }], placed: [{ dialogue_id: 8, character: '老周', shot_number: 2, time_segment: '1.0–2.0s' }] }
  assert.deepEqual(normalizeDialoguePlacement(payload), payload)
})
