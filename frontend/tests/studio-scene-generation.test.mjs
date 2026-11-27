import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { computed, effectScope, reactive, ref, watch } from 'vue'
import { parse, compileScript, compileTemplate } from '@vue/compiler-sfc'
import { candidateStoryboardSource, materialStoryboardSource, provenanceObject, storyboardComparison } from '../src/utils/storyboardProvenance.js'
import { materialList } from '../src/utils/materialList.js'
const source = { version: 1, sceneId: 'director-scene:2', revision: 'a'.repeat(64), sceneRevision: 'b'.repeat(64), planRevision: 'c'.repeat(64), sourceFingerprint: 'director-source', samplerVersion: 'script-shot-timeline-v1', frameAspect: '16:9', projectId: 1, sceneIdNumber: 2, shotId: 9, planShotId: 'director-shot:9', sampleSeconds: 0 }
const candidate = () => ({ id: 7, project_id: 1, entity_type: 'scene', entity_id: 2, media_type: 'image', file: 'result.png', is_current: true, review_status: 'pending', provenance_json: JSON.stringify({ source: 'studio_location', project_id: 1, scene_id: 2, shot_id: 9, camera: { storyboard_source: source } }) })
const material = () => ({ project_id: 1, name: 'previs.webm', derivation_manifest: JSON.stringify({ storyboardSource: source }) })
const text = readFileSync(new URL('../src/components/StudioSceneGeneration.vue', import.meta.url), 'utf8')
const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const { descriptor } = parse(text)
const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve() }
function host(api, initial = {}) {
  const props = reactive({ projectId: 1, sceneId: 2, imageFile: 'result.png', videoFile: 'previs.webm', allowAdopt: true, ...initial })
  const scope = effectScope(), events = [], hooks = []
  const code = descriptor.scriptSetup.content.replace(/^import .+$/gm, '')
  const fn = new Function('computed', 'ref', 'watch', 'onBeforeUnmount', 'api', 'materialList', 'candidateStoryboardSource', 'materialStoryboardSource', 'provenanceObject', 'storyboardComparison', 'defineProps', 'defineEmits', 'window', `${code};return { loadSources, adopt, studioCandidates, imageSource, videoSource, comparison, error };`)
  const panel = scope.run(() => fn(computed, ref, watch, hook => hooks.push(hook), api, materialList, candidateStoryboardSource, materialStoryboardSource, provenanceObject, storyboardComparison, () => props, () => (...args) => events.push(args), { confirm: () => true }))
  return { panel, props, events, stop() { hooks.forEach(fn => fn()); scope.stop() } }
}
test('same parent is comparable; absent, malformed and foreign provenance never claims sameness', () => {
  const image = candidateStoryboardSource(candidate(), 1, 2), video = materialStoryboardSource(material(), 1, 2)
  assert.equal(storyboardComparison(image, video).status, 'same')
  assert.equal(storyboardComparison(image, { ...video, revision: 'd'.repeat(64) }).status, 'different')
  for (const item of [{}, { provenance_json: 'bad' }, { ...candidate(), provenance_json: JSON.stringify({ source: 'studio_location', project_id: 99, scene_id: 2 }) }]) assert.equal(candidateStoryboardSource(item, 1, 2), null)
  assert.equal(materialStoryboardSource({ ...material(), project_id: 99 }, 1, 2), null)
  assert.equal(materialStoryboardSource(material(), 1, 3), null)
  assert.equal(storyboardComparison(null, video).status, 'unknown')
})
test('scene panel compiles and is mounted in both scene preparation and video review, preserving existing generator', () => {
  const script = compileScript(descriptor, { id: 'source' })
  assert.deepEqual(compileTemplate({ id: 'source', filename: 'StudioSceneGeneration.vue', source: descriptor.template.content, compilerOptions: { bindingMetadata: script.bindings } }).errors, [])
  assert.equal((detail.match(/<StudioSceneGeneration /g) || []).length, 2)
  assert.match(detail, /:video-file="videoTaskRef3D"/)
  assert.match(text, /:disabled="loading \|\| adopting" @click="adopt\(candidate\)"/)
})
test('opening only reads; real entity_id candidate is visible; manual adoption uses existing select route only', async () => {
  const calls = [], item = { ...candidate(), is_current: false }
  const api = { sceneCandidates: async (...args) => { calls.push(['read', ...args]); return { data: [item, { ...item, id: 8, entity_id: 99 }] } }, materials: async () => ({ data: [material()] }), selectCandidate: async (...args) => calls.push(['select', ...args]) }
  const h = host(api)
  try {
    await flush(); assert.equal(h.panel.studioCandidates.value.length, 1); assert.equal(calls.filter(x => x[0] === 'select').length, 0)
    await h.panel.adopt(h.panel.studioCandidates.value[0]); assert.deepEqual(calls.filter(x => x[0] === 'select'), [['select', 1, 7]]); assert.deepEqual(h.events, [['scene-changed']])
  } finally { h.stop() }
})
test('stale/rejected candidates and read-only video review cannot adopt; differing revisions warn', async () => {
  const stale = { ...candidate(), stale: true }, rejected = { ...candidate(), id: 8, review_status: 'rejected' }
  const api = { sceneCandidates: async () => ({ data: [stale, rejected] }), materials: async () => ({ data: [{ ...material(), derivation_manifest: JSON.stringify({ storyboardSource: { ...source, revision: 'd'.repeat(64) } }) }] }), selectCandidate: () => assert.fail('No adoption authorized') }
  const h = host(api)
  try { await flush(); assert.equal(h.panel.comparison.value.status, 'different'); await h.panel.adopt(h.panel.studioCandidates.value[0]); await h.panel.adopt(h.panel.studioCandidates.value[1]); h.props.allowAdopt = false; h.panel.studioCandidates.value[0].stale = false; await h.panel.adopt(h.panel.studioCandidates.value[0]) } finally { h.stop() }
})
test('late source replies cannot populate another scene or an unmounted panel', async () => {
  let release
  const api = { sceneCandidates: () => new Promise(ok => { release = ok }), materials: async () => ({ data: [material()] }) }
  const h = host(api)
  const old = release
  h.props.sceneId = 3; await flush(); old({ data: [candidate()] }); await flush(); assert.equal(h.panel.studioCandidates.value.length, 0)
  h.stop(); release({ data: [candidate()] }); await flush(); assert.equal(h.panel.studioCandidates.value.length, 0)
})
test('source read errors are visible without any fallback generation or automatic retry', async () => {
  let reads = 0
  const h = host({ sceneCandidates: async () => { reads++; throw Error('offline') }, materials: async () => ({ data: [] }) })
  try { await flush(); assert.match(h.panel.error.value, /来源读取失败/); assert.equal(reads, 1) } finally { h.stop() }
})
