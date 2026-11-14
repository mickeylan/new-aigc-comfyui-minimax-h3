import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { MODEL_LIMIT_DEFAULTS, mergeModelLimits } from '../src/utils/modelLimits.js'

test('server model limits override the offline defaults key by key', () => {
  const merged = mergeModelLimits({ shot_max_seconds: 12, long_shot_seconds: 8, unknown: 1 })
  assert.equal(merged.shot_max_seconds, 12)
  assert.equal(merged.long_shot_seconds, 8)
  assert.equal(merged.reaction_shot_min_seconds, MODEL_LIMIT_DEFAULTS.reaction_shot_min_seconds)
  assert.equal('unknown' in merged, false)
})

test('missing, zero or invalid server values fall back to defaults', () => {
  assert.deepEqual(mergeModelLimits(null), { ...MODEL_LIMIT_DEFAULTS })
  const merged = mergeModelLimits({ shot_max_seconds: 0, scene_min_seconds: 'x', h3_high_risk_seconds: -1 })
  assert.equal(merged.shot_max_seconds, MODEL_LIMIT_DEFAULTS.shot_max_seconds)
  assert.equal(merged.scene_min_seconds, MODEL_LIMIT_DEFAULTS.scene_min_seconds)
  assert.equal(merged.h3_high_risk_seconds, MODEL_LIMIT_DEFAULTS.h3_high_risk_seconds)
})

test('pacing copy in views renders from model limits instead of literals', () => {
  const settings = readFileSync(new URL('../src/views/Settings.vue', import.meta.url), 'utf8')
  assert.match(settings, /\{\{modelLimits\.h3_reference_gpu_memory_gb\}\}GB/)
  assert.match(settings, /\{\{modelLimits\.h3_high_risk_seconds\}\}秒以上/)
  assert.doesNotMatch(settings, /约12GB|30秒以上/)
  const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
  assert.match(detail, /约 \{\{ longShotSeconds \}\} 秒一段/)
  for (const file of ['../src/components/ShotDirectorEditor.vue', '../src/views/ProjectDetail.vue', '../src/views/ProjectEditor.vue', '../src/views/Settings.vue']) {
    const source = readFileSync(new URL(file, import.meta.url), 'utf8')
    assert.match(source, /mergeModelLimits/, file)
  }
})
