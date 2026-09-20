import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')
const editor = readFileSync(new URL('../src/views/ProjectEditor.vue', import.meta.url), 'utf8')
const shots = readFileSync(new URL('../src/components/ShotDirectorEditor.vue', import.meta.url), 'utf8')

const apiRoute = (method, route) => new RegExp(`${method}\\(\`\\/projects\\/\\$\\{id\\}${route}`).test(api)

test('Phase 1 frontend API exposes existing shot selector and candidate routes', () => {
  assert.ok(apiRoute('http\\.get', '/shots/\\$\\{shotId\\}/looks'))
  assert.ok(apiRoute('http\\.put', '/shots/\\$\\{shotId\\}/looks'))
  assert.ok(apiRoute('http\\.get', '/shots/\\$\\{shotId\\}/outfits'))
  assert.ok(apiRoute('http\\.put', '/shots/\\$\\{shotId\\}/outfits'))
  assert.ok(apiRoute('http\\.delete', '/candidates/\\$\\{cid\\}'))
})

test('Phase 1 editing surfaces are reachable', () => {
  for (const handler of ['saveSharedAsset', 'removeSharedAsset', 'saveAudioLayer', 'removeAudioLayer', 'removeCandidate']) assert.match(editor, new RegExp(`@(?:click|submit\\.prevent)=["']${handler}`))
  assert.match(editor, /CharacterHistoryDrawer/)
  assert.match(shots, /saveShotAssets/)
  assert.match(shots, /镜头造型覆盖/)
})
