import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { ageStageLabel, groupOutfitsByStage, episodeAssignSummary, validEpisodeRange, stageApproved } from '../src/utils/characterAgeStage.js'

const looks = readFileSync(new URL('../src/views/CharacterLooks.vue', import.meta.url), 'utf8')
const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')

test('年龄阶段标签带年龄，无年龄只显示名称', () => {
  assert.equal(ageStageLabel({ name: '童年', age: 6 }), '童年（6岁）')
  assert.equal(ageStageLabel({ name: '成年后', age: null }), '成年后')
  assert.equal(ageStageLabel(null), '')
  assert.ok(stageApproved({ audit_status: 'approved' }))
  assert.ok(!stageApproved({ audit_status: 'draft' }))
})

test('套装按主形象与年龄阶段分组，阶段按年龄升序', () => {
  const groups = groupOutfitsByStage([
    { id: 1, name: '成人日常', age_stage_id: 0 },
    { id: 2, name: '少年校服', age_stage_id: 20, age_stage: { id: 20, name: '少年', age: 15 } },
    { id: 3, name: '童年日常', age_stage_id: 10, age_stage: { id: 10, name: '童年', age: 6 } },
    { id: 4, name: '童年节日', age_stage_id: 10, age_stage: { id: 10, name: '童年', age: 6 } },
  ])
  assert.deepEqual(groups.map(g => g.label), ['主形象', '童年（6岁）', '少年（15岁）'])
  assert.deepEqual(groups[1].outfits.map(o => o.id), [3, 4])
  assert.deepEqual(groupOutfitsByStage([{ id: 5, age_stage_id: 9, age_stage: { id: 9, name: '老年', age: 70 } }]).map(g => g.label), ['老年（70岁）'])
})

test('批量指定范围校验与摘要说明保留媒体、不自动生成', () => {
  assert.ok(validEpisodeRange(1, 1))
  assert.ok(!validEpisodeRange(0, 3))
  assert.ok(!validEpisodeRange(5, 4))
  assert.ok(!validEpisodeRange('', 2))
  const text = episodeAssignSummary({ assign: 2, unchanged: 1, skipped: 1, items: [{ action: 'assign', has_media: true }, { action: 'assign', has_media: false }] })
  assert.match(text, /将修改 2 个 Scene/)
  assert.match(text, /1 个已手动选择其他套装（未勾选覆盖，跳过）/)
  assert.match(text, /其中 1 个已有图片\/视频：保留不删/)
  assert.match(text, /不会自动重新生成/)
})

test('角色造型页提供年龄阶段区、阶段标准像审阅与按集批量指定', () => {
  assert.match(looks, /mode==='stages'/)
  assert.match(looks, /年龄阶段（\{\{stages\.length\}\}）/)
  assert.match(looks, /\/age-stages/)
  assert.match(looks, /portrait\/prompt/)
  assert.match(looks, /v-model="stagePrompt\.text"/)
  assert.match(looks, /ageStagePortraitEngines/)
  assert.match(looks, /v-model\.number="outfitForm\.age_stage_id"/)
  assert.match(looks, /v-model\.number="designForm\.age_stage_id"/)
  assert.match(looks, /assign-episodes/)
  assert.match(looks, /preview:true/)
  assert.match(looks, /v-model="episodeForm\.overwrite"/)
  assert.match(looks, /episodeAssignSummary/)
  assert.match(looks, /确认应用到这些 Scene/)
})

test('场景造型下拉按主形象与年龄阶段分组', () => {
  assert.match(detail, /groupOutfitsByStage\(sceneOutfitOptions\[ch\.id\]\|\|\[\]\)/)
  assert.match(detail, /<optgroup v-for="g in groupOutfitsByStage/)
})
