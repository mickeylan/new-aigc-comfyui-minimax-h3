import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('检查记录 API 使用独立只读历史、显式保存、预览和应用路由', () => {
  assert.match(api, /sceneAdvisoryReports:.*get\(`\/projects\/\$\{id\}\/scenes\/\$\{sid\}\/storyboard\/reports`\)/)
  assert.match(api, /saveSceneAdvisoryReport:.*post\(`\/projects\/\$\{id\}\/scenes\/\$\{sid\}\/storyboard\/reports`, data\)/)
  assert.match(api, /previewSceneAdvisoryCorrection:.*reports\/\$\{rid\}\/correction\/preview/)
  assert.match(api, /applySceneAdvisoryCorrection:.*reports\/\$\{rid\}\/correction\/apply/)
})

test('历史入口不运行检查且记录展示来源、模型、时间和过期状态', () => {
  assert.match(detail, /openSceneAdvisory\(sc, 'history'\).*查看检查记录/)
  assert.match(detail, /if\(kind==='history'\) loadAdvisoryHistory\(\); else runSceneAdvisory\(\)/)
  assert.match(detail, /item\.report\.provider/)
  assert.match(detail, /formatAdvisoryTime\(item\.report\.created_at\)/)
  assert.match(detail, /item\.stale\?'已过期':'与当前画面一致'/)
  assert.match(detail, /来源：用户保存检查记录/)
})

test('保存检查记录严格构造白名单且允许带 source hash 的 unable result', () => {
  assert.match(detail, /canSaveAdvisory.*source_hash.*status/)
  assert.match(detail, /const payload=\{source_hash:report\.source_hash,status:report\.status,reason:report\.reason\|\|'',checks:Array\.isArray\(report\.checks\).*uncertainty:/)
  assert.doesNotMatch(detail, /saveSceneAdvisoryReport\([^\n]+advisoryReport\.value/)
})

test('修正仅从未过期保存记录预览，显式确认后只合并 image_prompt 与 prompt_stale', () => {
  assert.match(detail, /selectedAdvisory && !selectedAdvisory\.stale/)
  assert.match(detail, /原分镜图提示词（只读）/)
  assert.match(detail, /整理后的修正提示词预览（可编辑）/)
  assert.match(detail, /仅保存分镜图提示词，不修改故事、不重画/)
  assert.match(detail, /local\.image_prompt=data\.image_prompt;local\.prompt_stale=!!data\.prompt_stale/)
  assert.doesNotMatch(detail, /applyAdvisoryCorrection[\s\S]{0,1200}api\.updateScene/)
})

test('历史相关异步操作使用项目场景来源快照和 epoch，缓存有界', () => {
  assert.match(detail, /image_prompt: sc\.image_prompt/)
  assert.match(detail, /epoch!==advisoryEpoch/)
  assert.match(detail, /advisorySnapshotCurrent\(snapshot\)/)
  assert.match(detail, /slice\(-20\)\.reverse\(\)/)
  assert.doesNotMatch(detail, /setInterval\([^)]*Advisory/i)
})
