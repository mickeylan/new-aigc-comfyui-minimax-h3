<template>
  <section class="profile-design">
    <h4>交互设计草稿（不自动保存）</h4>
    <p class="field-hint">只修改勾选字段，其余字段由程序保留。已有参考像/四视图提示词不会自动改写；采用后请检查它们是否仍与档案一致。</p>
    <DesignQuestions kind="character" :disabled="busy || disabled" @answer="text => instruction = [instruction, text].filter(Boolean).join('\n')" />
    <button type="button" class="btn btn-sm btn-ghost" :disabled="busy || disabled" @click="fields = Object.keys(labels)">选择全部档案字段（完整初稿）</button>
    <div class="fields">
      <label v-for="(label, key) in labels" :key="key"><input v-model="fields" type="checkbox" :value="key" :disabled="busy || disabled" /> {{ label }}</label>
    </div>
    <div v-if="fields.length === 1">
      <label>当前字段副本：可选中文字后仅修改选区（要编辑原文请使用下方档案表单）</label>
      <textarea class="textarea" :value="form[fields[0]] || ''" readonly rows="3" @select="capture" @click="capture" />
      <p v-if="selection" class="field-hint">仅改选区：{{ selection.selection }} <button type="button" class="btn btn-sm btn-ghost" @click="selection = null">改为整个字段</button></p>
    </div>
    <textarea v-model="instruction" class="textarea" rows="2" :disabled="busy || disabled" placeholder="这次想怎么改？例如只修改发型，不改变年龄和衣服" />
    <button type="button" class="btn btn-sm btn-secondary" :disabled="busy || disabled || !fields.length || !instruction.trim()" @click="generate">{{ busy ? '生成草稿中…' : '生成所选字段草稿' }}</button>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <template v-if="history.length">
      <div class="fields"><button v-for="(_, index) in history" :key="index" type="button" class="btn btn-sm btn-ghost" @click="activeIndex = index">草稿 {{ index + 1 }}{{ activeIndex === index ? ' ✓' : '' }}</button></div>
      <p class="field-hint">AI 建议，待确认；只有点击采用才填入表单，保存仍需单独操作。</p>
      <div v-for="(value, key) in active.patch" :key="key">
        <strong>{{ labels[key] }}</strong>
        <pre class="preview"><template v-for="(part, i) in diffText(active.before[key] || '', value)" :key="i"><del v-if="part.type === 'del'">{{ part.text }}</del><mark v-else-if="part.type === 'add'">{{ part.text }}</mark><span v-else>{{ part.text }}</span></template></pre>
      </div>
      <button type="button" class="btn btn-sm btn-primary" :disabled="busy || disabled || !canAdopt" @click="adopt">采用此稿到表单</button>
      <button v-if="undo" type="button" class="btn btn-sm btn-ghost" :disabled="busy || disabled || !canUndo" @click="rollback">撤销上次采用</button>
      <p v-if="!canAdopt" class="field-hint">表单已变化；请重新生成，旧稿不会覆盖当前编辑。</p>
    </template>
  </section>
</template>
<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { api } from '../api'
import DesignQuestions from './DesignQuestions.vue'
import { diffText } from '../utils/assetDesign.js'
import { createDesignRequestGate, selectedCodePoints } from '../utils/designRequest.js'
const props = defineProps({ projectId: [Number, String], characterId: Number, form: { type: Object, required: true }, disabled: Boolean })
const emit = defineEmits(['adopt'])
const labels = { gender: '性别', appearance: '外貌', personality: '性格', background: '背景', relationships: '关系', emotions: '情绪', habits: '习惯', wardrobe_detail: '服装', lighting_mood: '光影', color_palette: '配色' }
const fields = ref(['appearance'])
const instruction = ref('')
const selection = ref(null)
const busy = ref(false)
const error = ref('')
const history = ref([])
const activeIndex = ref(0)
const undo = ref(null)
const active = computed(() => history.value[activeIndex.value])
const formSnapshot = () => JSON.stringify(props.form)
const canAdopt = computed(() => active.value?.snapshot === formSnapshot())
const canUndo = computed(() => undo.value && Object.entries(undo.value.applied).every(([key, value]) => props.form[key] === value))
const gate = createDesignRequestGate()
watch(() => JSON.stringify([props.projectId, props.characterId, props.form, props.disabled, fields.value, instruction.value, selection.value]), () => gate.invalidate(), { flush: 'sync' })
watch(() => JSON.stringify([props.projectId, props.characterId]), () => { history.value = []; undo.value = null; selection.value = null })
watch(() => JSON.stringify([fields.value, props.form]), () => { selection.value = null }, { flush: 'sync' })
onBeforeUnmount(() => gate.dispose())
function capture(event) { selection.value = selectedCodePoints(event.target.value, event.target.selectionStart, event.target.selectionEnd) }
async function generate() {
  if (busy.value || props.disabled || !props.characterId || !fields.value.length || !instruction.value.trim()) return
  const before = { ...props.form }
  const snapshot = formSnapshot()
  const token = gate.capture()
  busy.value = true; error.value = ''
  try {
    const current = Object.fromEntries(Object.keys(labels).map(key => [key, props.form[key] || '']))
    const { data } = await api.previewCharacterProfile(props.projectId, props.characterId, { current, fields: [...fields.value], instruction: instruction.value, ...(selection.value || {}) })
    if (!gate.accepts(token)) { error.value = '表单或设计对象已变化，未应用旧请求结果'; return }
    if (!data.patch || Object.keys(data.patch).length !== fields.value.length || Object.entries(data.patch).some(([key, value]) => !fields.value.includes(key) || typeof value !== 'string' || !value.trim())) throw new Error('无效字段草稿')
    history.value.push({ patch: data.patch, before, snapshot })
    activeIndex.value = history.value.length - 1
  } catch (e) {
    if (gate.accepts(token)) error.value = e.response?.data?.error || e.message || '生成失败'
  } finally { busy.value = false }
}
async function generateFull() {
  if (busy.value || props.disabled) return
  fields.value = Object.keys(labels)
  selection.value = null
  if (!instruction.value.trim()) instruction.value = '根据已有资料生成完整档案初稿；沿用已有事实，未知事实明确标注待确认。'
  return generate()
}
defineExpose({ generateFull })
function adopt() {
  if (!canAdopt.value || props.disabled || busy.value) return
  undo.value = { before: Object.fromEntries(Object.keys(active.value.patch).map(key => [key, props.form[key]])), applied: { ...active.value.patch } }
  emit('adopt', { ...active.value.patch })
}
function rollback() {
  if (!canUndo.value || props.disabled || busy.value) return
  emit('adopt', { ...undo.value.before }); undo.value = null
}
</script>
<style scoped>
.profile-design { border: 1px solid var(--border); border-radius: 8px; padding: 12px; margin: 12px 0; }
.profile-design .fields { display: flex; gap: 10px; flex-wrap: wrap; margin: 10px 0; }
.profile-design .textarea { width: 100%; margin: 6px 0; }
.preview { white-space: pre-wrap; max-height: 200px; overflow: auto; font: inherit; }
.preview del { background: #fee2e2; color: #991b1b; }
.preview mark { background: #dcfce7; color: #166534; }
.error { color: #b91c1c; }
</style>
