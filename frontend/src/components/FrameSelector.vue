<template>
  <div class="continuity-panel">
    <div class="panel-head"><h3>镜头连续性 · 第{{ sceneOrder }}镜</h3><button class="btn btn-sm btn-ghost" @click="$emit('close')">关闭</button></div>

    <section class="block">
      <h4>本镜输出的衔接帧</h4>
      <p class="hint">视频完成后自动提取最后22帧。选择其中一帧，供后续镜头续接。</p>
      <div class="actions"><button class="btn btn-sm" :disabled="loading" @click="extract">{{ loading ? '提取中…' : '重新提取最后22帧' }}</button></div>
      <div v-if="frames.length" class="frame-grid">
        <button v-for="f in frames" :key="f.id" class="frame" :class="{ selected: f.selected }" @click="select(f)">
          <img :src="f.image_url" :alt="`候选帧${f.frame_index + 1}`" />
          <span>{{ f.frame_index + 1 }}/{{ frames.length }} · {{ timeLabel(f.timestamp_ms) }}</span>
        </button>
      </div>
      <p v-else class="empty">尚无候选帧</p>
    </section>

    <section class="block">
      <h4>本镜生成方式</h4>
      <select v-model="mode" class="input">
        <option value="independent">独立多参考生成</option>
        <option value="continue_from_previous">从上一镜选定衔接帧继续</option>
        <option value="bridge_to_storyboard">上一镜衔接帧 → 当前分镜图</option>
      </select>
      <p v-if="mode !== 'independent'" class="hint">默认使用同集内顺序最近的上一镜及其已选衔接帧。</p>
      <div v-if="continuity?.status === 'source_invalidated'" class="warning">上一镜视频已变化，请重新保存连续性配置。</div>
      <div v-if="continuity?.selected_frame" class="source-frame">
        <img :src="`/api/input/${projectId}/${continuity.selected_frame.image_file}`" />
        <span>来源帧：{{ timeLabel(continuity.selected_frame.timestamp_ms) }}</span>
      </div>
      <div class="actions"><button class="btn" :disabled="saving" @click="saveMode">{{ saving ? '保存中…' : '保存生成方式' }}</button></div>
    </section>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { api } from '../api'

const props = defineProps({ projectId: { type: Number, required: true }, sceneId: { type: Number, required: true }, sceneOrder: { type: Number, required: true } })
const emit = defineEmits(['close', 'frame-selected', 'applied'])
const frames = ref([])
const continuity = ref(null)
const mode = ref('independent')
const loading = ref(false)
const saving = ref(false)

async function load() {
  const [fr, cfg] = await Promise.all([api.getFrameCandidates(props.projectId, props.sceneId), api.getContinuity(props.projectId, props.sceneId)])
  frames.value = fr.data.frames || []
  continuity.value = cfg.data.continuity || null
  mode.value = continuity.value?.mode || 'independent'
}
async function extract() { loading.value = true; try { frames.value = (await api.extractFrameCandidates(props.projectId, props.sceneId)).data.frames || [] } finally { loading.value = false } }
async function select(frame) { const selected = (await api.selectFrame(props.projectId, props.sceneId, frame.id)).data.frame; frames.value = frames.value.map(f => ({ ...f, selected: f.id === selected.id })); emit('frame-selected', selected) }
async function saveMode() { saving.value = true; try { continuity.value = (await api.configureContinuity(props.projectId, props.sceneId, { mode: mode.value, source_mode: 'auto_previous' })).data.continuity; emit('applied', continuity.value) } finally { saving.value = false } }
function timeLabel(ms) { const value = Number(ms || 0); return value < 0 ? `${(value / 1000).toFixed(3)}s（距结尾）` : `${(value / 1000).toFixed(3)}s` }
onMounted(() => load().catch(() => {}))
</script>

<style scoped>
.continuity-panel{padding:20px;background:var(--card);color:var(--text-primary);border-radius:12px}.panel-head{display:flex;justify-content:space-between;align-items:center}.block{margin-top:18px;padding:14px;border:1px solid var(--border);border-radius:8px}.block h4{margin:0 0 8px}.hint,.empty{color:var(--text-secondary);font-size:13px}.actions{margin-top:12px;display:flex;gap:8px}.frame-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(135px,1fr));gap:10px;margin-top:12px}.frame{padding:0;overflow:hidden;border:2px solid transparent;border-radius:6px;background:var(--bg-secondary);color:var(--text-secondary);cursor:pointer}.frame.selected{border-color:#4caf50}.frame img{display:block;width:100%;aspect-ratio:16/9;object-fit:cover}.frame span{display:block;padding:5px;font-size:11px}.input{width:100%;padding:8px;background:var(--bg-secondary);color:var(--text-primary);border:1px solid var(--border);border-radius:6px}.warning{margin-top:10px;padding:8px;color:#ffb74d;background:rgba(255,152,0,.12);border-radius:6px}.source-frame{display:flex;align-items:center;gap:12px;margin-top:12px}.source-frame img{width:160px;aspect-ratio:16/9;object-fit:cover;border-radius:6px}
</style>
