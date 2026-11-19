import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { ageStagePortraitEngines, normalizeStageEngine, stageReferenceToken, ageStageLabel, groupOutfitsByStage, episodeAssignSummary, validEpisodeRange, stageApproved } from '../src/utils/characterAgeStage.js'

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

test('角色卡提供追加年龄形象入口，且说明不会替换现有形象', () => {
  assert.match(detail, /looks\?mode=stages&new=1/)
  assert.match(detail, /＋ 追加年龄形象/)
  assert.match(detail, /不会替换当前标准像、四视图或已有套装/)
  assert.match(looks, /r\.query\.new==='1'&&mode\.value==='stages'\)openStage\(\)/)
})

test('阶段提示词可只保存不生成，历史四视图保留并可恢复', () => {
  assert.match(looks, /只保存提示词/)
  assert.match(looks, /portrait\/prompt`,jsonOpt\('PUT'/)
  assert.match(looks, /portrait\/restore/)
  // 用户要求阶段形象直接生成四视图，不再单独生成标准像，历史区随之改名。
  // 用户追加要求：历史四视图可以删除不满意的图，标签补上“或删除”。
  assert.match(looks, /历史四视图（保留，可恢复或删除）/)
  assert.match(looks, /if\(created&&row\?\.id\)await openStagePrompt\(row\)/)
})

test('历史四视图可删除不满意的图，删除前二次确认并调用删除接口', () => {
  assert.match(looks, /@click="deleteStagePortrait\(s,f\)">删除<\/button>/)
  // 删除按钮与“恢复”并排显示在每张历史图下面。
  assert.match(looks, /@click="restoreStagePortrait\(s,f\)">恢复<\/button><button class="btn btn-xs btn-danger"[^>]*@click="deleteStagePortrait\(s,f\)">删除<\/button>/)
  assert.match(looks, /async function deleteStagePortrait\(s,file\)\{if\(!confirm\(`删除这张历史四视图？图片文件会一起删除，无法找回。/)
  assert.match(looks, /portrait\/history\/delete`,jsonOpt\('POST',\{file\}\)/)
  // 当前正在使用的那张由后端拒绝，前端说明里明确讲清。
  assert.match(looks, /当前正在使用的四视图不受影响/)
})

// 用户要求：阶段四视图必须有参考图；Moss 与 Krea2 都只支持文生图，因此只提供多图编辑。
// 用户追加要求：在多图编辑之外再给一个 MiniMax H3-Image 编辑选项，默认仍是 Qwen。
test('阶段四视图必须参考主标准像，可选 Qwen 或 MiniMax H3-Image，默认 Qwen，旧取值按 Qwen 显示', () => {
  assert.deepEqual(ageStagePortraitEngines.map(e => e.value), ['qwen_edit', 'minimax_h3_edit'])
  assert.equal(normalizeStageEngine('moss'), 'qwen_edit')
  assert.equal(normalizeStageEngine('qwen_image_2_1'), 'qwen_edit')
  assert.equal(normalizeStageEngine(''), 'qwen_edit')
  assert.equal(normalizeStageEngine('krea2'), 'qwen_edit')
  assert.equal(normalizeStageEngine('minimax_h3_edit'), 'minimax_h3_edit')
  // 两个引擎的参考图绑定写法不同，提示词必须跟着引擎走。
  assert.equal(stageReferenceToken('qwen_edit'), '<image1>')
  assert.equal(stageReferenceToken('minimax_h3_edit'), '<Picture 1>')
  assert.doesNotMatch(looks, /Krea2 四视图编辑/)
  assert.match(looks, /:disabled="!!s\.portrait_task_id\|\|!character\?\.portrait"[^>]*@click="openStagePrompt\(s\)">\{\{s\.portrait_task_id\?'生成中…':'审阅提示词并生成四视图'\}\}/)
  assert.match(looks, /以角色主标准像为参考/)
  // 引擎选择在阶段表单里，由 ageStagePortraitEngines 渲染；提示文案标明该引擎要保留的绑定写法。
  assert.match(looks, /<label>四视图引擎<select v-model="stageForm\.portrait_engine"/)
  assert.match(looks, /v-for="e in ageStagePortraitEngines"/)
  assert.match(looks, /提示词须保留 \$\{stageReferenceToken\(s\?\.portrait_engine\)\}/)
  // 四视图引擎标签直接显示后端保存的取值。
  assert.match(looks, /四视图引擎：\{\{stageEngineLabel\(s\.portrait_engine\)\}\}/)
})

test('编辑年龄阶段可 AI 扩写人物形象，结果只填入文本框且可撤销', () => {
  assert.match(looks, /\$\{stageURL\(\)\}\/expand-appearance`,jsonOpt\('POST',\{name:stageForm\.name\?\.trim\(\)\|\|'',age,brief:before\.trim\(\)\}\)/)
  assert.match(looks, /@click="expandStageAppearance">\{\{stageExpanding\?'AI扩写中…':'AI扩写'\}\}/)
  assert.match(looks, /撤销扩写/)
  assert.match(looks, /stageExpandSkill\.value=d\.skill\|\|''/)
  assert.match(looks, /本次使用 Skill：\{\{stageExpandSkill\}\}/)
  assert.match(looks, /stageAppearanceBefore\.value=before;stageForm\.appearance=d\.appearance/)
})

test('套装旧定妆照/四视图有历史版本入口并可设为当前', () => {
  assert.match(looks, /@click="openOutfitHistory\(o\)">历史版本<\/button>/)
  assert.match(looks, /asset-variants\?entity_type=\$\{t\}&entity_id=\$\{h\.outfit\.id\}/)
  assert.match(looks, /'character_outfit_image','character_outfit_sheet'/)
  assert.match(looks, /asset-variants\/\$\{v\.id\}\/select`/)
  assert.match(detail, /替换下来的旧标准像\/四视图全部保留，不自动清理/)
})
