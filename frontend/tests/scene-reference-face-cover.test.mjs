import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const editor = readFileSync(new URL('../src/components/FaceCoverEditor.vue', import.meta.url), 'utf8')
const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('遮脸只对补充参考图开放，人物/造型/新形象不显示入口', () => {
  assert.match(detail, /const faceCoverAllowed = \(ref\) => ref\.source_type === 'asset' \|\| ref\.source_type === 'shared_material'/)
  assert.match(detail, /v-if="faceCoverAllowed\(ref\)"/)
  // 缩略图显示遮脸版，但编辑器始终在原图上画框。
  assert.match(detail, /api\.inputUrl\(id\(\), ref\.covered_image \|\| ref\.image\)/)
  assert.match(detail, /:image-url="api\.inputUrl\(id\(\), faceCoverTarget\.image\)"/)
})

test('保存带原图哈希、框数上限 30，恢复原图需确认', () => {
  assert.match(editor, /const maxBoxes = 30/)
  assert.match(editor, /source_sha256: source\.value\.source_sha256, boxes: boxes\.value, color: color\.value/)
  assert.match(editor, /confirm\('恢复原图后/)
  assert.match(editor, /原图保持不变/)
  assert.match(editor, /不调用图像模型/)
  assert.match(editor, /image-orientation: from-image/)
})

test('遮脸后只刷新候选，不覆盖未保存的参考图勾选', () => {
  assert.match(detail, /async function onFaceCoverChanged\(message\) \{[\s\S]*?sceneReferenceCandidates\.value = data\.candidates \|\| \[\][\s\S]*?\n\}/)
  const body = detail.match(/async function onFaceCoverChanged\(message\) \{[\s\S]*?\n\}/)[0]
  assert.doesNotMatch(body, /selectedSceneReferences\.value =/)
  assert.match(api, /saveSceneReferenceFaceCover: \(id, sid, data\) => http\.put\(`\/projects\/\$\{id\}\/scenes\/\$\{sid\}\/references\/face-cover`, data\)/)
  assert.match(api, /restoreSceneReferenceFaceCover: \(id, sid, key\) => http\.delete\(`\/projects\/\$\{id\}\/scenes\/\$\{sid\}\/references\/face-cover`, \{ params: \{ key \} \}\)/)
})
