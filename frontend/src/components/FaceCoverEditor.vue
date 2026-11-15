<template>
  <div class="face-cover-panel">
      <div class="panel-head"><h3>参考图局部遮脸</h3><button class="btn btn-sm btn-ghost" @click="$emit('close')">关闭</button></div>
      <p class="hint">{{ label }}。在图片上拖动画框，框内会填成纯色色块（本地处理，不调用图像模型）。保存后另存一张遮脸版，只用于本Scene；原图保持不变，可随时恢复。</p>
      <p v-if="loading" class="hint">加载中…</p>
      <template v-else-if="source">
        <p v-if="source.stale" class="warning">已有的遮脸是在旧图上画的，当前参考图已更换，旧遮脸不再生效；请重新画框保存，或恢复原图清除旧记录。</p>
        <div class="stage" :style="{ aspectRatio: `${source.width} / ${source.height}` }">
          <img :src="imageUrl" alt="参考原图" draggable="false" />
          <div class="overlay" @pointerdown="startBox" @pointermove="moveBox" @pointerup="endBox" @pointerleave="endBox">
            <div v-for="(b, i) in boxes" :key="i" class="box" :style="boxStyle(b)"><button type="button" title="删除此框" @pointerdown.stop @click="boxes.splice(i, 1)">✕</button></div>
            <div v-if="drawing" class="box drawing" :style="boxStyle(drawing)"></div>
          </div>
        </div>
        <div class="form-row">
          <label>色块颜色 <input v-model="color" type="color" /></label>
          <span class="hint">{{ boxes.length }} / {{ maxBoxes }} 个框</span>
          <button type="button" class="btn btn-sm btn-ghost" :disabled="!boxes.length" @click="boxes = []">清空框</button>
        </div>
      </template>
      <p v-if="error" class="warning">{{ error }}</p>
      <div class="modal-actions">
        <button v-if="source?.cover" class="btn btn-secondary" :disabled="busy" @click="restore">恢复原图</button>
        <button class="btn" :disabled="busy || !source || !boxes.length" @click="save">{{ busy ? '处理中…' : '保存遮脸版' }}</button>
      </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { api } from '../api'

const props = defineProps({
  projectId: { type: Number, required: true },
  sceneId: { type: Number, required: true },
  referenceKey: { type: String, required: true },
  label: { type: String, default: '' },
  imageUrl: { type: String, required: true }
})
const emit = defineEmits(['close', 'changed'])

const maxBoxes = 30
const source = ref(null)
const boxes = ref([])
const color = ref('#808080')
const drawing = ref(null)
const loading = ref(true)
const busy = ref(false)
const error = ref('')
let origin = null

onMounted(load)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const { data } = await api.sceneReferenceFaceCover(props.projectId, props.sceneId, props.referenceKey)
    source.value = data
    if (data.cover && !data.stale) {
      boxes.value = (data.cover.boxes || []).map(b => ({ ...b }))
      color.value = data.cover.color || color.value
    }
  } catch (e) {
    error.value = e.response?.data?.error || '加载参考图失败'
  } finally {
    loading.value = false
  }
}

// Boxes are normalized 0-1 coordinates of the displayed (EXIF-oriented) image, matching the server.
function point(e) {
  const rect = e.currentTarget.getBoundingClientRect()
  return { x: Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width)), y: Math.min(1, Math.max(0, (e.clientY - rect.top) / rect.height)) }
}
function startBox(e) {
  if (boxes.value.length >= maxBoxes) { error.value = `最多 ${maxBoxes} 个框`; return }
  origin = point(e)
  drawing.value = { x: origin.x, y: origin.y, w: 0, h: 0 }
  e.currentTarget.setPointerCapture?.(e.pointerId)
}
function moveBox(e) {
  if (!origin) return
  const p = point(e)
  drawing.value = { x: Math.min(origin.x, p.x), y: Math.min(origin.y, p.y), w: Math.abs(p.x - origin.x), h: Math.abs(p.y - origin.y) }
}
function endBox() {
  if (!origin) return
  const b = drawing.value
  origin = null
  drawing.value = null
  if (b && b.w > 0.005 && b.h > 0.005 && b.x < 1 && b.y < 1) boxes.value.push(b)
}
function boxStyle(b) {
  return { left: `${b.x * 100}%`, top: `${b.y * 100}%`, width: `${b.w * 100}%`, height: `${b.h * 100}%`, background: color.value }
}

async function save() {
  busy.value = true
  error.value = ''
  try {
    await api.saveSceneReferenceFaceCover(props.projectId, props.sceneId, { key: props.referenceKey, source_sha256: source.value.source_sha256, boxes: boxes.value, color: color.value })
    emit('changed', '已保存遮脸版参考图，原图保持不变')
  } catch (e) {
    error.value = e.response?.data?.error || '保存遮脸失败'
  } finally {
    busy.value = false
  }
}

async function restore() {
  if (!confirm('恢复原图后，本Scene将重新使用未遮挡的参考图。确定恢复？')) return
  busy.value = true
  error.value = ''
  try {
    await api.restoreSceneReferenceFaceCover(props.projectId, props.sceneId, props.referenceKey)
    emit('changed', '已恢复原图')
  } catch (e) {
    error.value = e.response?.data?.error || '恢复原图失败'
  } finally {
    busy.value = false
  }
}
</script>

<style scoped>
.face-cover-panel { display: grid; gap: 10px; }
.panel-head { display: flex; align-items: center; justify-content: space-between; }
.panel-head h3 { margin: 0; }
.hint { margin: 0; font-size: 12.5px; color: var(--text-secondary); }
.warning { margin: 0; font-size: 12.5px; color: var(--red); }
.stage { position: relative; width: 100%; max-height: 64vh; margin: 0 auto; user-select: none; }
.stage img { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: fill; image-orientation: from-image; }
.overlay { position: absolute; inset: 0; cursor: crosshair; touch-action: none; }
.box { position: absolute; outline: 1px solid rgba(255, 255, 255, 0.9); }
.box.drawing { opacity: 0.6; }
.box button { position: absolute; top: -10px; right: -10px; width: 20px; height: 20px; border-radius: 50%; border: none; background: rgba(0, 0, 0, 0.7); color: #fff; font-size: 11px; cursor: pointer; }
.form-row { display: flex; align-items: center; gap: 12px; font-size: 13px; }
.modal-actions { display: flex; justify-content: flex-end; gap: 8px; }
</style>
