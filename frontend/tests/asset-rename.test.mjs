import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('角色、道具和场景卡片提供独立改名入口', () => {
  assert.match(detail, /@click="openAssetRename\('character', ch\)">改名<\/button>/)
  assert.match(detail, /@click="openAssetRename\('asset', a\)">改名<\/button>/)
  assert.match(detail, /maxlength="100"/)
  assert.match(detail, /:disabled="assetRenameBusy \|\| !assetRenameName\.trim\(\)"/)
})

test('改名 API 使用精确 PATCH endpoint 和仅 name payload', () => {
  assert.match(api, /renameCharacter: \(id, cid, name\) => http\.patch\(`\/projects\/\$\{id\}\/characters\/\$\{cid\}\/name`, \{ name \}\)/)
  assert.match(api, /renameAsset: \(id, kind, aid, name\) => http\.patch\(`\/projects\/\$\{id\}\/assets\/\$\{kind\}\/\$\{aid\}\/name`, \{ name \}\)/)
  assert.match(detail, /api\.renameCharacter\(projectID, captureID, name\)/)
  assert.match(detail, /api\.renameAsset\(projectID, target\.assetKind, captureID, name\)/)
})

test('改名保存有请求快照、防覆盖和刷新，且不触发 GPU 或旧更新接口', () => {
  const saveBody = detail.match(/async function saveAssetRename\(\) \{([\s\S]*?)\n\}\n\nasync function load/)
  assert.ok(saveBody)
  assert.match(saveBody[1], /const requestEpoch = \+\+assetRenameEpoch/)
  assert.match(saveBody[1], /const captureID = target\.id/)
  assert.match(saveBody[1], /const projectID = target\.projectID/)
  assert.match(saveBody[1], /await load\(\)/)
  assert.doesNotMatch(saveBody[1], /updateCharacter|updateAsset|generate|gpu|GPU/)
  assert.match(detail, /if \(assetRenameBusy\.value\) return/)
})
