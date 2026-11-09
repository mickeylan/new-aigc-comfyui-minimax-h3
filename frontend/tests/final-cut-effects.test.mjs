import test from 'node:test'
import assert from 'node:assert/strict'
import { buildEditSequence, clipOutputDuration, normalizeClip, sequenceDuration, splitClip } from '../src/utils/finalCutEditing.js'

const clip = (id, extra = {}) => ({ id, scene_id: id, duration: 4, source_start: 0, source_end: 4, ...extra })

test('effect ranges and defaults are normalized', () => {
  const value = normalizeClip(clip(1), 4)
  assert.equal(value.speed, 1)
  assert.equal(value.overlay_width, .25)
  assert.equal(clipOutputDuration(clip(1, { speed: 2 })), 2)
  for (const bad of [{ speed: 0 }, { volume: 5 }, { brightness: 2 }, { contrast: 0 }, { saturation: -1 }, { overlay_x: 2 }]) assert.throws(() => normalizeClip(clip(1, bad), 4))
  assert.throws(() => normalizeClip(clip(1, { title: '😀'.repeat(201) }), 4))
})

test('payload only emits changed effect values and known names', () => {
  const payload = buildEditSequence([clip(1, { speed: 2, title: '片名', overlay_material_id: 8 }), clip(2)])
  assert.deepEqual(payload[0], { id: 1, label: '', source_start: 0, source_end: null, scene_id: 1, speed: 2, title: '片名', overlay_material_id: 8 })
  assert.equal('video_url' in payload[0], false)
})

test('transitions use output time and adjacent half-duration limits', () => {
  const clips = [clip(1, { speed: 2, transition: 'fade', transition_duration: .5 }), clip(2, { speed: 4 })]
  assert.equal(sequenceDuration(clips), 2.5)
  assert.throws(() => buildEditSequence([clip(1, { transition: 'fade', transition_duration: .6 }), clip(2, { speed: 4 })]))
  assert.throws(() => buildEditSequence([clip(1), clip(2, { transition: 'fade' })]))
})

test('split makes the new left boundary a cut and preserves one outbound transition', () => {
  const [left, right] = splitClip(clip(1, { transition: 'wipeleft', transition_duration: .4, fade_in: 1, fade_out: 1 }), 2)
  assert.equal(left.transition, 'cut')
  assert.equal(right.transition, 'wipeleft')
  assert.notEqual(left.id, right.id)
  assert.equal(left.scene_id, right.scene_id)
})
