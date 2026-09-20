import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const view = readFileSync(new URL('../src/views/EpisodeScreenplay.vue', import.meta.url), 'utf8')
const router = readFileSync(new URL('../src/router/index.js', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')
const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')

test('结构化剧本编辑器可达并写回现有剧本流水线', () => {
  assert.match(router, /episodes\/:episode\/screenplay/)
  assert.match(api, /episodeScreenplay:/)
  assert.match(view, /applyScreenplayImport/)
  assert.match(view, /保存并同步流水线/)
  assert.match(view, /Scene \/ Dialogue/)
  assert.match(detail, /结构化剧本编辑器/)
})

test('创意修改后可从项目页和剧本页重新生成方案与剧本', () => {
  assert.match(detail, /重新生成创作方案/)
  assert.match(detail, /重新生成第/)
  assert.match(view, /generateCreativePlan/)
  assert.match(view, /regenerateEpisodeScript/)
  assert.match(view, /1\. 重新生成创作方案/)
  assert.match(view, /2\. 重新生成第/)
})

test('结构化剧本块支持场景动作对白旁白转场及未保存保护', () => {
  for (const label of ['新增场景', '＋动作', '＋角色对白', '＋旁白', '＋转场']) assert.ok(view.includes(label))
  assert.match(view, /onBeforeRouteLeave/)
  assert.match(view, /beforeunload/)
  assert.match(view, /createRevision|修订快照/)
})
