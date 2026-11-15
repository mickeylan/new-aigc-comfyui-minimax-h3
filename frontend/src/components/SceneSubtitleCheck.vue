<template>
  <details class="subtitle-check" @toggle="$event.target.open && !loaded && load()">
    <summary>烧录字幕检测（本地，仅提示）</summary>
    <p class="hint">本地抽帧检查画面下方居中区域是否有内容变化的文字条带，不调用模型、不收费，也不会修改视频。结果只给时间提示；“不确定”需要人工判断。</p>
    <p v-if="loading" class="hint">读取中…</p>
    <template v-else-if="check">
      <p><span class="badge" :class="badgeClass">{{ statusLabel }}</span> <small>{{ check.detail }}（抽帧 {{ check.samples }} 张）</small></p>
      <p v-if="check.stale" class="warning">Scene 视频已更换，这次结果已过期，请重新检测。</p>
      <ul v-if="check.hints.length" class="hints">
        <li v-for="(h, i) in check.hints" :key="i"><button type="button" class="btn btn-sm btn-ghost" @click="$emit('seek', h.start)">{{ h.start.toFixed(1) }}s – {{ h.end.toFixed(1) }}s</button></li>
      </ul>
      <div v-if="check.status === 'uncertain' && !check.stale" class="decision">
        <span>人工判断：{{ decisionLabel }}</span>
        <button type="button" class="btn btn-sm btn-secondary" :disabled="busy" @click="decide('has_subtitles')">确有字幕</button>
        <button type="button" class="btn btn-sm btn-secondary" :disabled="busy" @click="decide('no_subtitles')">不是字幕</button>
      </div>
    </template>
    <p v-else-if="loaded" class="hint">尚未检测。</p>
    <p v-if="error" class="warning">{{ error }}</p>
    <button type="button" class="btn btn-sm btn-secondary" :disabled="busy" @click="run">{{ busy ? '检测中…' : check ? '重新检测' : '开始检测' }}</button>
  </details>
</template>

<script setup>
import { computed, ref } from 'vue'
import { api } from '../api'

const props = defineProps({
  projectId: { type: Number, required: true },
  sceneId: { type: Number, required: true }
})
defineEmits(['seek'])
const check = ref(null)
const loading = ref(false)
const loaded = ref(false)
const busy = ref(false)
const error = ref('')

const statusLabel = computed(() => ({ clean: '未检出字幕', subtitles: '疑似有字幕', uncertain: '不确定' })[check.value?.status] || check.value?.status)
const badgeClass = computed(() => check.value?.status === 'clean' ? 'badge-gray' : 'warn')
const decisionLabel = computed(() => ({ has_subtitles: '确有字幕', no_subtitles: '不是字幕' })[check.value?.decision] || '待确认')

async function load() {
  loading.value = true
  error.value = ''
  try {
    const { data } = await api.sceneSubtitleCheck(props.projectId, props.sceneId)
    check.value = data.check
    loaded.value = true
  } catch (e) {
    error.value = e.response?.data?.error || '读取字幕检测结果失败'
  } finally {
    loading.value = false
  }
}
async function run() {
  busy.value = true
  error.value = ''
  try {
    const { data } = await api.runSceneSubtitleCheck(props.projectId, props.sceneId)
    check.value = data.check
    loaded.value = true
  } catch (e) {
    error.value = e.response?.data?.error || '字幕检测失败'
  } finally {
    busy.value = false
  }
}
async function decide(decision) {
  busy.value = true
  error.value = ''
  try {
    const { data } = await api.decideSceneSubtitleCheck(props.projectId, props.sceneId, decision)
    check.value = data.check
  } catch (e) {
    error.value = e.response?.data?.error || '保存人工判断失败'
  } finally {
    busy.value = false
  }
}
</script>

<style scoped>
.subtitle-check { display: grid; gap: 6px; margin-top: 6px; font-size: 13px; }
.subtitle-check p { margin: 0; }
.hint { font-size: 12px; color: var(--text-secondary); }
.warning { font-size: 12.5px; color: var(--red); }
.hints { display: flex; flex-wrap: wrap; gap: 4px; margin: 0; padding: 0; list-style: none; }
.decision { display: flex; align-items: center; gap: 8px; }
</style>
