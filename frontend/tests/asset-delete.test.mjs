import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const stateSource = detail.slice(detail.indexOf('const deletingAssets = ref('), detail.indexOf('let assetRenameEpoch'))
const characterSource = detail.slice(detail.indexOf('async function removeCharacter'), detail.indexOf('// ---------- 角色档案'))
const assetSource = detail.slice(detail.indexOf('async function removeAsset'), detail.indexOf('// ---------- 对白配音'))

function fixture({ confirmed = true, characterError, assetError } = {}) {
  const calls = [], messages = []
  let loads = 0
  const context = {
    ref: value => ({ value }),
    confirm: () => confirmed,
    id: () => 41,
    api: {
      deleteCharacter: async (...args) => { calls.push(['character', ...args]); if (characterError) throw characterError },
      deleteAsset: async (...args) => { calls.push(['asset', ...args]); if (assetError) throw assetError }
    },
    load: async () => { loads++ },
    toast: {
      success: message => messages.push(['success', message]),
      error: message => messages.push(['error', message])
    }
  }
  const make = new Function(...Object.keys(context), `${stateSource}\n${characterSource}\n${assetSource}\nreturn { removeCharacter, removeAsset, isAssetDeleting }`)
  return { ...make(...Object.values(context)), calls, messages, get loads() { return loads } }
}

test('删除入口与改名并列，底部不再重复，且确认文案诚实说明资产与已有成片', () => {
  assert.match(detail, /openAssetRename\('character', ch\)[\s\S]{0,300}removeCharacter\(ch\)/)
  assert.match(detail, /openAssetRename\('asset', a\)[\s\S]{0,300}removeAsset\(a\)/)
  assert.equal((detail.match(/@click="removeCharacter\(ch\)"/g) || []).length, 1)
  assert.equal((detail.match(/@click="removeAsset\(a\)"/g) || []).length, 1)
  assert.match(characterSource, /角色记录及其标准像、四视图等角色资产将被删除；已有分镜和视频保留/)
  assert.match(assetSource, /资产记录及其参考图、四视图等资产将被删除；已有分镜和视频保留/)
})

test('执行真实删除函数：取消不请求，确认后按 kind/id 请求并刷新', async () => {
  const cancelled = fixture({ confirmed: false })
  await cancelled.removeCharacter({ id: 7, name: '甲' })
  await cancelled.removeAsset({ id: 8, kind: 'prop', name: '剑' })
  assert.deepEqual(cancelled.calls, [])

  const accepted = fixture()
  await accepted.removeCharacter({ id: 7, name: '甲' })
  await accepted.removeAsset({ id: 8, kind: 'location', name: '山门' })
  assert.deepEqual(accepted.calls, [['character', 41, 7], ['asset', 41, 'location', 8]])
  assert.equal(accepted.loads, 2)
  assert.equal(accepted.messages.filter(([type]) => type === 'success').length, 2)
})

test('失败使用常驻 error toast、释放单项状态并可重试，不调用生成清理', async () => {
  const responseError = { response: { data: { error: '仍被引用' } } }
  const failed = fixture({ characterError: responseError })
  const character = { id: 9, name: '乙' }
  await failed.removeCharacter(character)
  await failed.removeCharacter(character)
  assert.equal(failed.calls.length, 2)
  assert.equal(failed.loads, 0)
  assert.deepEqual(failed.messages, [['error', '仍被引用'], ['error', '仍被引用']])
  assert.equal(failed.isAssetDeleting('character', 9), false)
  assert.doesNotMatch(characterSource + assetSource, /generate|clearGeneration|resetPortrait|clearVideo/)
})
