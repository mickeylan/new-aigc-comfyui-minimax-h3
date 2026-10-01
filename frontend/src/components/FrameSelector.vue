<template>
  <div class="continuity-panel">
    <div class="panel-head"><h3>第{{ sceneOrder }}镜 · 输入连续性</h3><button class="btn btn-sm btn-ghost" @click="$emit('close')">关闭</button></div>

    <section class="block">
      <h4>本镜生成方式</h4>
      <select v-model="mode" class="input">
        <option value="independent">独立多参考生成</option>
        <option value="continue_from_previous" :disabled="!sourceSceneId">从上一镜末尾帧继续</option>
        <option value="bridge_to_storyboard" :disabled="!sourceSceneId">上一镜末尾帧 → 当前分镜图</option>
      </select>
      <p v-if="!sourceSceneId" class="hint">当前是本集第一镜，没有可衔接的上一镜，只能独立生成。</p>
      <p v-else-if="mode !== 'independent'" class="hint">当前第{{ sceneOrder }}镜将使用第{{ sourceSceneOrder }}镜选定的末尾帧作为起点。</p>
      <div v-if="continuity?.status === 'source_invalidated'" class="warning">上一镜视频已变化，请重新选择末尾帧并保存。</div>
    </section>

    <section v-if="mode !== 'independent' && sourceSceneId" class="block">
      <h4>选择第{{ sourceSceneOrder }}镜的末尾帧</h4>
      <p class="hint">这里展示的是上一镜视频末尾约1秒的22帧候选。选中的图片将作为当前第{{ sceneOrder }}镜的起始衔接帧。</p>
      <p v-if="!sourceVideoReady" class="warning">第{{ sourceSceneOrder }}镜视频尚未完成，请先生成上一镜视频。</p>
      <div v-else class="actions"><button class="btn btn-sm" :disabled="loading" @click="extract">{{ loading ? '提取中…' : '重新提取上一镜末尾22帧' }}</button></div>
      <div v-if="frames.length" class="frame-grid">
        <button v-for="f in frames" :key="f.id" class="frame" :class="{ selected: f.id === selectedFrameId }" @click="select(f)">
          <img :src="f.image_url" :alt="`上一镜候选帧${f.frame_index + 1}`" />
          <span>{{ f.frame_index + 1 }}/{{ frames.length }} · {{ timeLabel(f.timestamp_ms) }}</span>
        </button>
      </div>
      <p v-else-if="sourceVideoReady" class="empty">尚无候选帧，可点击上方按钮重新提取。</p>
      <div v-if="selectedOutput" class="replace-row">
        <span>已选上一镜末尾帧：{{ selectedOutput.source === 'manual_upload' ? '用户高清替换图' : timeLabel(selectedOutput.timestamp_ms) }}</span>
        <label class="btn btn-sm btn-secondary">上传高清图替换<input hidden type="file" accept="image/png,image/jpeg,image/webp" @change="replaceFrame" /></label>
      </div>
    </section>

    <p v-if="error" class="warning">{{ error }}</p>
    <div class="actions"><button class="btn btn-secondary" :disabled="saving || (mode !== 'independent' && !sourceSceneId)" @click="saveMode(false)">{{ saving ? '保存中…' : '仅保存' }}</button><button class="btn" :disabled="saving || (mode !== 'independent' && (!sourceSceneId || !selectedFrameId))" @click="saveMode(true)">{{ saving ? '处理中…' : '保存并编辑视频提示词' }}</button></div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { api } from '../api'

const props = defineProps({
  projectId: { type: Number, required: true },
  sceneId: { type: Number, required: true },
  sceneOrder: { type: Number, required: true },
  sourceSceneId: { type: Number, default: 0 },
  sourceSceneOrder: { type: Number, default: 0 },
  sourceVideoReady: { type: Boolean, default: false }
})
const emit = defineEmits(['close', 'frame-selected', 'applied'])
const frames = ref([])
const continuity = ref(null)
const mode = ref('independent')
const loading = ref(false)
const saving = ref(false)
const selectedOutput = ref(null)
const selectedFrameId = ref(0)
const error = ref('')

async function load() {
  error.value = ''
  const cfg = await api.getContinuity(props.projectId, props.sceneId)
  continuity.value = cfg.data.continuity || null
  mode.value = continuity.value?.mode || 'independent'
  if (!props.sourceSceneId) {
    mode.value = 'independent'
    return
  }
  const fr = await api.getFrameCandidates(props.projectId, props.sourceSceneId)
  frames.value = fr.data.frames || []
  const configuredFrameId = Number(continuity.value?.selected_frame_id || 0)
  selectedOutput.value = frames.value.find(f => f.id === configuredFrameId) || frames.value.find(f => f.selected) || null
  selectedFrameId.value = selectedOutput.value?.id || 0
}
async function extract() {
  if (!props.sourceSceneId) return
  loading.value = true
  error.value = ''
  try {
    frames.value = (await api.extractFrameCandidates(props.projectId, props.sourceSceneId)).data.frames || []
    selectedOutput.value = null
    selectedFrameId.value = 0
  } catch (e) {
    error.value = e.response?.data?.error || e.message
  } finally { loading.value = false }
}
async function select(frame) {
  error.value = ''
  try {
    const selected = (await api.selectFrame(props.projectId, props.sourceSceneId, frame.id)).data.frame
    frames.value = frames.value.map(f => f.id === selected.id ? selected : ({ ...f, selected: false }))
    selectedOutput.value = selected
    selectedFrameId.value = selected.id
    emit('frame-selected', selected)
  } catch (e) { error.value = e.response?.data?.error || e.message }
}
async function replaceFrame(event) {
  const file = event.target.files?.[0]
  event.target.value = ''
  if (!file || !props.sourceSceneId) return
  error.value = ''
  try {
    const form = new FormData()
    form.append('file', file)
    const replaced = (await api.replaceSelectedFrame(props.projectId, props.sourceSceneId, form)).data.frame
    frames.value = frames.value.map(f => f.id === replaced.id ? replaced : f)
    selectedOutput.value = replaced
    selectedFrameId.value = replaced.id
    emit('frame-selected', replaced)
  } catch (e) { error.value = e.response?.data?.error || e.message }
}
async function saveMode(openPrompt) {
  saving.value = true
  error.value = ''
  try {
    const payload = { mode: mode.value, source_mode: 'auto_previous' }
    if (mode.value !== 'independent') {
      payload.source_scene_id = props.sourceSceneId
      if (selectedFrameId.value) payload.frame_id = selectedFrameId.value
    }
    continuity.value = (await api.configureContinuity(props.projectId, props.sceneId, payload)).data.continuity
    emit('applied', continuity.value, openPrompt)
  } catch (e) { error.value = e.response?.data?.error || e.message }
  finally { saving.value = false }
}
function timeLabel(ms) { const value = Number(ms || 0); return value < 0 ? `${(value / 1000).toFixed(3)}s（距结尾）` : `${(value / 1000).toFixed(3)}s` }
onMounted(() => load().catch(e => { error.value = e.response?.data?.error || e.message }))
</script>

<style scoped>
.continuity-panel{padding:20px;background:var(--card);color:var(--text-primary);border-radius:12px}.panel-head{display:flex;justify-content:space-between;align-items:center}.block{margin-top:18px;padding:14px;border:1px solid var(--border);border-radius:8px}.block h4{margin:0 0 8px}.hint,.empty{color:var(--text-secondary);font-size:13px}.actions{margin-top:12px;display:flex;gap:8px}.frame-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(135px,1fr));gap:10px;margin-top:12px}.frame{padding:0;overflow:hidden;border:2px solid transparent;border-radius:6px;background:var(--bg-secondary);color:var(--text-secondary);cursor:pointer}.frame.selected{border-color:#4caf50}.frame img{display:block;width:100%;aspect-ratio:16/9;object-fit:cover}.frame span{display:block;padding:5px;font-size:11px}.input{width:100%;padding:8px;background:var(--bg-secondary);color:var(--text-primary);border:1px solid var(--border);border-radius:6px}.warning{margin-top:10px;padding:8px;color:#ffb74d;background:rgba(255,152,0,.12);border-radius:6px}.replace-row{display:flex;justify-content:space-between;align-items:center;gap:12px;margin-top:12px;padding:10px;background:var(--bg-secondary);border-radius:6px;font-size:13px}
</style>
