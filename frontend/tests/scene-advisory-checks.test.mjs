import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('Scene卡提供两个只读建议检查且共用弹窗', () => {
  assert.match(detail, />故事源检查</)
  assert.match(detail, />分镜图检查</)
  assert.match(detail, /openSceneAdvisory\(sc, 'story'\)/)
  assert.match(detail, /openSceneAdvisory\(sc, 'storyboard'\)/)
  assert.match(detail, /不检查视频，也不替换提示词或任何媒体/)
  assert.match(detail, /advisoryState==='loading'/)
  assert.match(detail, /advisoryState==='error'/)
  assert.match(detail, /check\.evidence/)
  assert.match(detail, /check\.suggestion/)
  assert.match(detail, /advisoryReport\.uncertainty/)
})

test('建议检查API路径正确且无法评估不会伪装通过', () => {
  assert.match(api, /sceneStoryConsistency:.*get\(`\/projects\/\$\{id\}\/scenes\/\$\{sid\}\/story\/consistency`\)/)
  assert.match(api, /checkSceneStoryboard:.*post\(`\/projects\/\$\{id\}\/scenes\/\$\{sid\}\/storyboard\/check`/)
  assert.match(detail, /unable_to_assess:'无法评估'/)
  assert.match(detail, /data\.reason\|\|data\.error\|\|'检查失败，不能视为通过'/)
})

test('关闭重开和来源变化会废弃迟到检查结果', () => {
  assert.match(detail, /let advisoryEpoch = 0/)
  assert.match(detail, /function closeSceneAdvisory\(\) \{ advisoryEpoch\+\+/)
  assert.match(detail, /const snapshot = sceneAdvisorySnapshot\(sc\)/)
  for (const field of ['content', 'image_file', 'blocking_json']) assert.match(detail, new RegExp(`snapshot\\.${field}`))
  assert.match(detail, /epoch!==advisoryEpoch/)
  assert.match(detail, /检查来源已过期，请重新检查/)
})

test('空间关系使用后端schema v2自然字段并保留来源哈希', () => {
  assert.match(detail, /人物站位 · 空间与动作关系/)
  for (const field of ['kind','source','subject','reference','relation','functional_end','start','end','trajectory']) assert.match(detail, new RegExp(`v-model="relation\\.${field}"`))
  assert.match(detail, /schema_version: 2/)
  assert.match(detail, /source_content_hash: blockingDraft\.source_content_hash/)
  assert.match(detail, /relations: blockingDraft\.relations\.slice\(0,40\)/)
  assert.match(detail, /evidence:lines\(evidenceText/)
  assert.match(detail, /sc\.blocking_content_hash/)
})
