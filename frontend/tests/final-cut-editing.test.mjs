import test from 'node:test'
import assert from 'node:assert/strict'
import { buildEditSequence, normalizeClip, splitClip } from '../src/utils/finalCutEditing.js'

test('trim validates finite bounds and supports full-file null end', () => {
  const full = normalizeClip({ id: 'x', source_start: 0, source_end: null }, 3)
  assert.equal(full.source_end, 3)
  for (const bad of ['NaN', NaN, Infinity]) assert.throws(() => normalizeClip({ source_start: bad }, 3))
  assert.throws(() => normalizeClip({ source_start: -1 }, 3))
  assert.throws(() => normalizeClip({ source_start: 2.98 }, 3))
})

test('split preserves source identity, creates unique occurrences, and keeps order', () => {
  const source = { id: 'occurrence', scene_id: 7, material_id: 9, label: 'same source', duration: 10, source_start: 2, source_end: 8, video_url: '/private/not-exported' }
  const [left, right] = splitClip(source, 5)
  assert.equal(left.scene_id, source.scene_id)
  assert.equal(right.material_id, source.material_id)
  assert.notEqual(left.id, right.id)
  assert.deepEqual([left.source_start, left.source_end, right.source_start, right.source_end], [2, 5, 5, 8])
  assert.deepEqual(source, { id: 'occurrence', scene_id: 7, material_id: 9, label: 'same source', duration: 10, source_start: 2, source_end: 8, video_url: '/private/not-exported' })
})

test('payload is whitelisted and represents full-file end as null', () => {
  const payload = buildEditSequence([
    { id: 'a', scene_id: 1, label: 'A', duration: 4, source_start: 0, source_end: 4, video_url: '/secret' },
    { id: 'b', material_id: 2, label: 'B', duration: 5, source_start: 1, source_end: 3, extra: true },
  ])
  assert.deepEqual(payload, [
    { id: 'a', label: 'A', source_start: 0, source_end: null, scene_id: 1 },
    { id: 'b', label: 'B', source_start: 1, source_end: 3, material_id: 2 },
  ])
  assert.equal(JSON.stringify(payload).includes('secret'), false)
})
