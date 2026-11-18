<template>
  <div class="asset-design">
    <div class="asset-design-row">
      <label>基于</label>
      <select class="input" :value="baseId || 0" @change="emit('update:baseId', Number($event.target.value))">
        <option :value="0">无（全新设计）</option>
        <option v-for="b in baseOptions" :key="b.id" :value="b.id">{{ b.name }}{{ b.image ? '' : '（还没有参考图）' }}</option>
        <option v-if="baseId && !baseOptions.some(b => b.id === baseId)" :value="baseId">派生来源已删除</option>
      </select>
    </div>
    <div v-if="base && !base.image" class="field-hint warn">「{{ base.name }}」还没有参考图；派生资产要以它的图为底用 Qwen 编辑，请先给它出图。</div>
    <div class="asset-design-row">
      <label>长期方向</label>
      <div class="asset-design-chips">
        <span v-for="d in directionList" :key="d" class="asset-design-chip">{{ d }}<button type="button" title="删除这条方向" @click="removeDirection(d)">✕</button></span>
        <small v-if="!directionList.length">暂无。修改时勾选「记为长期方向」会加到这里，之后的 AI 设计和批量补全都会带上。</small>
      </div>
    </div>
    <textarea v-model="ideas" class="textarea" rows="2" placeholder="我的想法：可留空，留空 = AI 做主" />
    <div class="asset-design-actions">
      <button type="button" class="btn btn-sm" :disabled="busy" @click="runDraft">{{ busy === 'draft' ? 'AI 出稿中…' : drafts.length ? 'AI 重新出一稿' : 'AI 生成第一稿' }}</button>
      <button type="button" class="btn btn-sm btn-secondary" :disabled="busy" @click="runOptions">{{ busy === 'options' ? '思考中…' : '先给我 3 个方向' }}</button>
      <button type="button" class="btn btn-sm btn-ghost" :disabled="busy || !description.trim()" title="把下面描述框里手改过的内容放进稿子列表，之后可以在它基础上让 AI 改" @click="importDescription">描述框作为新稿</button>
    </div>
    <div v-if="options.length" class="asset-design-options">
      <button v-for="o in options" :key="o.title" type="button" class="asset-design-option" @click="pickOption(o)"><strong>{{ o.title }}</strong><span>{{ o.detail }}</span></button>
    </div>
    <div v-if="error" class="notice error-notice">{{ error }}</div>

    <template v-if="active">
      <div class="asset-design-tabs">
        <button v-for="(d, i) in drafts" :key="i" type="button" class="btn btn-sm" :class="i === activeIndex ? '' : 'btn-ghost'" @click="selectDraft(i)">稿{{ i + 1 }}{{ d.origin === 'manual' ? '·手改' : '' }}</button>
        <span class="asset-design-view">
          <button type="button" class="btn btn-sm" :class="view === 'sources' ? '' : 'btn-ghost'" @click="view = 'sources'">来源标注</button>
          <button type="button" class="btn btn-sm" :class="view === 'diff' ? '' : 'btn-ghost'" :disabled="activeIndex === 0" @click="view = 'diff'">与上一稿对比</button>
        </span>
      </div>
      <div ref="draftBox" class="asset-design-draft" title="可选中一段文字，只改这一段" @mouseup="captureSelection">
        <template v-if="view === 'diff' && activeIndex > 0">
          <span v-for="(s, i) in diffSegments" :key="i" :class="'diff-' + s.type">{{ s.text }}</span>
        </template>
        <template v-else>
          <span v-for="(s, i) in sourceSegments" :key="i" :class="s.tag ? 'src src-' + tagClass[s.tag] : ''" :title="s.tag">{{ s.text }}</span>
        </template>
      </div>
      <div class="asset-design-legend">
        <template v-if="view === 'diff' && activeIndex > 0"><span class="diff-add">新增/改写</span><span class="diff-del">删去</span></template>
        <template v-else><span v-for="t in ASSET_SOURCE_TAGS" :key="t" :class="'src src-' + tagClass[t]">{{ t }}</span><span v-if="active.warning" class="warn">{{ active.warning }}</span></template>
      </div>
      <div v-if="active.changes?.length" class="asset-design-changes">
        <strong>相对「{{ base?.name || '来源' }}」的变化（派生出图时作为编辑指令）</strong>
        <ol><li v-for="c in active.changes" :key="c">{{ c }}</li></ol>
      </div>
      <div class="asset-design-revise">
        <input v-model="instruction" class="input" placeholder="指方向修改：例如 地面裂缝再多一些" @keydown.enter.prevent="runRevise" />
        <button type="button" class="btn btn-sm" :disabled="busy || !instruction.trim()" @click="runRevise">{{ busy === 'revise' ? '修改中…' : '改' }}</button>
      </div>
      <div class="asset-design-revise-opts">
        <label><input v-model="onlySelection" type="checkbox" :disabled="!selection"> 只改选中文字<small v-if="selection">：「{{ selection.length > 24 ? selection.slice(0, 24) + '…' : selection }}」</small><small v-else>（先在上面稿子里选中一段）</small></label>
        <label><input v-model="remember" type="checkbox"> 记为长期方向</label>
      </div>
      <div class="asset-design-actions">
        <button type="button" class="btn btn-sm" @click="adopt">采用此稿</button>
        <small>填入下面的描述框（仍可手改），点「保存」才会写入</small>
      </div>
    </template>
  </div>
</template>
<script setup>
import { computed, ref, watch } from 'vue'
import { api } from '../api'
import { ASSET_SOURCE_TAGS, diffText, markSources, splitDesignLines } from '../utils/assetDesign.js'
// 资产交互设计面板：AI 出稿 → 指方向 → AI 改稿 → 采用。所有稿子只在面板里，关掉即丢弃。
const props = defineProps({
  projectId: { type: [Number, String], required: true },
  kind: { type: String, required: true },
  assetId: { type: Number, default: 0 },
  name: { type: String, default: '' },
  description: { type: String, default: '' },
  baseId: { type: Number, default: 0 },
  directions: { type: String, default: '' },
  baseOptions: { type: Array, default: () => [] }
})
const emit = defineEmits(['update:baseId', 'update:directions', 'adopt'])
const tagClass = { 沿用: 'keep', 按你的要求: 'user', AI补充: 'ai', 与原文不同: 'diverge' }

const ideas = ref('')
const drafts = ref([])
const activeIndex = ref(0)
const view = ref('sources')
const instruction = ref('')
const onlySelection = ref(false)
const remember = ref(true)
const selection = ref('')
const options = ref([])
const busy = ref('')
const error = ref('')
const draftBox = ref(null)

const base = computed(() => props.baseOptions.find(b => b.id === props.baseId) || null)
const directionList = computed(() => splitDesignLines(props.directions))
const active = computed(() => drafts.value[activeIndex.value] || null)
const diffSegments = computed(() => activeIndex.value > 0 ? diffText(drafts.value[activeIndex.value - 1].description, active.value.description) : [])
const sourceSegments = computed(() => active.value ? markSources(active.value.description, active.value.sources) : [])

// 换了派生来源，旧稿的“沿用/变化”已不成立。
watch(() => props.baseId, () => { drafts.value = []; activeIndex.value = 0; options.value = [] })

function request(extra = {}) {
  return { asset_id: props.assetId, name: props.name.trim(), base_id: props.baseId || 0, directions: directionList.value, ...extra }
}
function pushDraft(draft, origin) {
  drafts.value.push({ description: draft.description, sources: draft.sources || [], changes: draft.changes || [], warning: draft.warning || '', origin })
  selectDraft(drafts.value.length - 1)
  view.value = drafts.value.length > 1 ? 'diff' : 'sources'
}
function selectDraft(i) {
  activeIndex.value = i
  selection.value = ''
  onlySelection.value = false
  if (i === 0) view.value = 'sources'
}
async function call(step, payload, onDone) {
  if (!props.name.trim()) { error.value = '请先填写名称，AI需要名称来识别设计对象'; return }
  busy.value = step
  error.value = ''
  try {
    const { data } = await api.assetDesign(props.projectId, props.kind, step, payload)
    onDone(data)
  } catch (e) {
    error.value = e.response?.data?.error || e.message || 'AI 设计失败'
  } finally {
    busy.value = ''
  }
}
function runDraft() {
  call('draft', request({ ideas: ideas.value.trim(), current: active.value ? '' : props.description.trim() }), data => pushDraft(data, 'ai'))
}
function runOptions() {
  call('options', request({ ideas: ideas.value.trim() }), data => { options.value = data.options || [] })
}
function pickOption(o) {
  ideas.value = `${o.title}：${o.detail}`
  options.value = []
}
function runRevise() {
  const text = instruction.value.trim()
  if (!text || !active.value || busy.value) return
  const payload = request({ current: active.value.description, instruction: text, selection: onlySelection.value ? selection.value : '' })
  call('revise', payload, data => {
    // 派生变化以最新稿为准；模型没给时沿用上一稿的变化清单。
    if (!data.changes?.length && props.baseId) data.changes = active.value.changes
    pushDraft(data, 'ai')
    if (remember.value) addDirection(text)
    instruction.value = ''
  })
}
function importDescription() {
  pushDraft({ description: props.description.trim(), sources: [], changes: active.value?.changes || [] }, 'manual')
}
function captureSelection() {
  const sel = window.getSelection()
  const text = sel && draftBox.value?.contains(sel.anchorNode) ? sel.toString().trim() : ''
  // 对比视图里含删去的字，选区可能不在当前稿中，交给后端校验并提示。
  selection.value = text
  onlySelection.value = !!text
}
function addDirection(text) {
  emit('update:directions', splitDesignLines([...directionList.value, text].join('\n')).join('\n'))
}
function removeDirection(d) {
  emit('update:directions', directionList.value.filter(x => x !== d).join('\n'))
}
function adopt() {
  if (active.value) emit('adopt', { description: active.value.description, changes: active.value.changes || [] })
}
</script>
<style scoped>
.asset-design { border: 1px solid var(--border); border-radius: 8px; padding: 10px; display: flex; flex-direction: column; gap: 8px; }
.asset-design-row { display: flex; align-items: center; gap: 8px; }
.asset-design-row > label { flex: none; width: 64px; font-size: 13px; color: var(--text-secondary); }
.asset-design-row .input { flex: 1; }
.asset-design-chips { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.asset-design-chips small, .asset-design-actions small, .asset-design-revise-opts small { color: var(--text-secondary); font-size: 12px; }
.asset-design-chip { display: inline-flex; align-items: center; gap: 4px; padding: 2px 8px; border-radius: 12px; background: var(--bg-secondary, #f2f3f5); font-size: 12px; }
.asset-design-chip button { border: none; background: none; cursor: pointer; padding: 0; color: var(--text-secondary); }
.asset-design-actions, .asset-design-tabs, .asset-design-revise-opts { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; }
.asset-design-view { margin-left: auto; display: inline-flex; gap: 4px; }
.asset-design-options { display: grid; gap: 6px; }
.asset-design-option { text-align: left; border: 1px dashed var(--border); border-radius: 6px; background: none; padding: 6px 8px; cursor: pointer; display: flex; flex-direction: column; gap: 2px; font-size: 13px; }
.asset-design-draft { border: 1px solid var(--border); border-radius: 6px; padding: 8px; font-size: 13px; line-height: 1.8; max-height: 220px; overflow: auto; white-space: pre-wrap; }
.asset-design-legend { display: flex; flex-wrap: wrap; gap: 8px; font-size: 12px; }
.asset-design-legend .warn { color: #c2410c; }
.asset-design-changes { font-size: 12px; }
.asset-design-changes ol { margin: 4px 0 0 18px; padding: 0; }
.asset-design-revise { display: flex; gap: 6px; }
.asset-design-revise .input { flex: 1; }
.diff-add { background: #dcfce7; color: #166534; }
.diff-del { background: #fee2e2; color: #991b1b; text-decoration: line-through; }
.src { border-bottom: 2px solid transparent; }
.src-keep { border-color: #94a3b8; }
.src-user { border-color: #2563eb; background: #eff6ff; }
.src-ai { border-color: #16a34a; }
.src-diverge { border-color: #dc2626; background: #fef2f2; }
</style>
