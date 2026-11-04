import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const view = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const apiSource = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')
const stateSource = view.slice(view.indexOf('const combatLoraTarget = ref('), view.indexOf('const sceneError = ref('))
const helperSource = view.slice(view.indexOf('function parseCombatLoraConfig'), view.indexOf('const pendingScenes = computed('))

function fixture({ raw = '', response, projectId = 41 } = {}) {
  const calls = []
  const context = {
    ref: value => ({ value }),
    route: { params: { id: String(projectId) } },
    project: { value: { combat_loras_json: raw } },
    scenes: { value: [] },
    loadRequestEpoch: 0,
    api: {
      getProjectCombatLoras: async () => ({ data: await response }),
      getSceneCombatLoras: async () => ({ data: await response }),
      updateProjectCombatLoras: async (id, payload) => { calls.push(['project', id, payload]); return { data: payload } },
      updateSceneCombatLoras: async (id, sid, payload) => { calls.push(['scene', id, sid, payload]); return { data: payload } }
    }
  }
  const make = new Function(...Object.keys(context), `${stateSource}\nconst id = () => route.params.id\n${helperSource}\nreturn { openCombatLoras, saveCombatLoras, addCombatLora, removeCombatLora, moveCombatLora, combatLoraTarget, combatLoraLoaded, combatLoraMode, combatLoraEntries, combatLoraFiles }`)
  return { ...make(...Object.values(context)), calls }
}

const sixLoras = Array.from({ length: 6 }, (_, i) => ({ on: i !== 2, lora: `folder/model-${i + 1}.safetensors`, strength: i + 0.25 }))
const resolved = { config: { mode: 'custom', loras: sixLoras }, source: 'template', files: sixLoras.map(item => item.lora) }

test('renders dynamic ordered editable Power LoRA controls without fixed slots', () => {
  assert.match(view, /v-for="\(entry, index\) in combatLoraEntries"/)
  assert.match(view, /list="combat-lora-files"/)
  assert.match(view, /\+ 添加新 LoRA/)
  assert.match(view, /moveCombatLora\(index, -1\)/)
  assert.match(view, /removeCombatLora\(index\)/)
  assert.match(view, /min="-100" max="100"/)
  assert.match(view, /rgthree 必须由 worker 安装，且所填模型文件必须已存在于 worker/)
  assert.doesNotMatch(view, /combatLoraSlots|combatLoraSlotList|\['156'\]|\['159'\]/)
})

test('GET runtime defaults clone all six entries and configured names feed only the datalist', async () => {
  assert.match(apiSource, /getProjectCombatLoras: id => http\.get\(`\/projects\/\$\{id\}\/combat-loras`\)/)
  const f = fixture({ response: resolved })
  await f.openCombatLoras('project')
  assert.equal(f.combatLoraLoaded.value, true)
  assert.equal(f.combatLoraEntries.value.length, 6)
  assert.notEqual(f.combatLoraEntries.value[0], sixLoras[0])
  assert.deepEqual(f.combatLoraFiles.value, resolved.files)
  assert.equal(f.combatLoraEntries.value[2].on, false)
  assert.equal(f.combatLoraEntries.value[2].strength, 2.25)
})

test('helpers preserve disabled strength, removal, ordering, and explicit empty custom', async () => {
  const f = fixture({ raw: JSON.stringify({ mode: 'custom', loras: [] }), response: resolved })
  await f.openCombatLoras('project')
  assert.equal(f.combatLoraMode.value, 'custom')
  const disabledWeight = f.combatLoraEntries.value[2].strength
  f.moveCombatLora(2, -1)
  assert.equal(f.combatLoraEntries.value[1].lora, 'folder/model-3.safetensors')
  assert.equal(f.combatLoraEntries.value[1].on, false)
  assert.equal(f.combatLoraEntries.value[1].strength, disabledWeight)
  f.removeCombatLora(0)
  assert.equal(f.combatLoraEntries.value.length, 5)
  f.combatLoraEntries.value.splice(0)
  await f.saveCombatLoras()
  assert.deepEqual(f.calls, [['project', 41, { mode: 'custom', loras: [] }]])
})

test('scene inherit remains distinct and PATCH endpoints are unchanged', () => {
  assert.match(apiSource, /updateProjectCombatLoras: \(id, data\) => http\.patch/)
  assert.match(apiSource, /updateSceneCombatLoras: \(id, sid, data\) => http\.patch/)
  assert.match(view, /\{ mode: 'inherit', loras: \[\] \}/)
  assert.doesNotMatch(helperSource, /promptStale|clear.*media|clear.*draft/i)
})
