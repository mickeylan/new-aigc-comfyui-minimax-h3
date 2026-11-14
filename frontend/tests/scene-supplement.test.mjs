import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('supplement insertion is a draft-then-confirm flow on each scene', () => {
  assert.match(detail, /@click="openSceneSupplement\(sc\)">在此后插入补镜</)
  assert.match(api, /sceneSupplementDraft: \(id, sid, brief\) => http\.post\(`\/projects\/\$\{id\}\/scenes\/\$\{sid\}\/supplement\/draft`/)
  assert.match(api, /applySceneSupplement: \(id, sid, data\) => http\.post\(`\/projects\/\$\{id\}\/scenes\/\$\{sid\}\/supplement\/apply`/)
  // Apply sends the reviewed draft with the insertion-point hash so the server can reject stale drafts.
  assert.match(detail, /api\.applySceneSupplement\(id\(\), sc\.id, \{ brief: supplementBrief\.value\.trim\(\), source_hash: draft\.source_hash, draft: draft\.draft \}\)/)
  assert.match(detail, /:disabled="supplementBusy \|\| !supplementDraft" @click="applySceneSupplementDraft">确认插入/)
})

test('supplement flow never starts image or video generation', () => {
  const start = detail.indexOf('async function applySceneSupplementDraft')
  const body = detail.slice(start, detail.indexOf('function toggleMergeScene', start))
  assert.ok(start > 0)
  assert.doesNotMatch(body, /genImage|genVideo|generateAll/)
})
