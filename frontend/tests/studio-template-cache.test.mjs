import test from 'node:test'
import assert from 'node:assert/strict'
import { findTemplateId, resetTemplateCache } from '../src/studio/host/template-cache.js'

const TEMPLATES = [
  { id: 7, code: 'image_remove_background' },
  { id: 9, code: 'image_to_3d_hunyuan3d' },
  { id: 12, code: 'image_inpaint' },
]

function countingFetch(payload = TEMPLATES) {
  const calls = { count: 0 }
  calls.fetch = async () => {
    calls.count += 1
    return { ok: true, json: async () => payload }
  }
  return calls
}

test('模板清单只请求一次，多个 code 共用同一份结果', async () => {
  resetTemplateCache()
  const calls = countingFetch()

  assert.equal(await findTemplateId('image_remove_background', { fetchImpl: calls.fetch }), 7)
  assert.equal(await findTemplateId('image_to_3d_hunyuan3d', { fetchImpl: calls.fetch }), 9)
  assert.equal(await findTemplateId('image_inpaint', { fetchImpl: calls.fetch }), 12)
  assert.equal(calls.count, 1, '同一页面内应复用模板清单')
})

test('并发查询只发起一次请求', async () => {
  resetTemplateCache()
  const calls = countingFetch()

  const [a, b] = await Promise.all([
    findTemplateId('image_remove_background', { fetchImpl: calls.fetch }),
    findTemplateId('image_to_3d_hunyuan3d', { fetchImpl: calls.fetch }),
  ])
  assert.equal(a, 7)
  assert.equal(b, 9)
  assert.equal(calls.count, 1)
})

test('找不到 code 时报中文错误，且不影响后续查询', async () => {
  resetTemplateCache()
  const calls = countingFetch()

  await assert.rejects(
    () => findTemplateId('image_not_exist', { fetchImpl: calls.fetch }),
    /缺少工作流模板：image_not_exist/,
  )
  // 清单本身是有效的，不应因为某个 code 缺失而被迫重拉。
  assert.equal(await findTemplateId('image_inpaint', { fetchImpl: calls.fetch }), 12)
  assert.equal(calls.count, 1)
})

test('请求失败不缓存，下一次可以重试成功', async () => {
  resetTemplateCache()
  let attempt = 0
  const fetchImpl = async () => {
    attempt += 1
    if (attempt === 1) return { ok: false, status: 503, json: async () => ({}) }
    return { ok: true, json: async () => TEMPLATES }
  }

  await assert.rejects(() => findTemplateId('image_inpaint', { fetchImpl }), /读取模板失败：503/)
  assert.equal(await findTemplateId('image_inpaint', { fetchImpl }), 12)
  assert.equal(attempt, 2, '失败后必须重新请求，而不是复用已 reject 的 promise')
})

test('resetTemplateCache 后重新请求，能拿到后端新装的模板', async () => {
  resetTemplateCache()
  const before = countingFetch(TEMPLATES)
  await assert.rejects(() => findTemplateId('image_segment_sam3', { fetchImpl: before.fetch }))

  resetTemplateCache()
  const after = countingFetch([...TEMPLATES, { id: 33, code: 'image_segment_sam3' }])
  assert.equal(await findTemplateId('image_segment_sam3', { fetchImpl: after.fetch }), 33)
  assert.equal(after.count, 1)
})