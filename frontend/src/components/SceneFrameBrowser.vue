<template>
  <div class="frame-browser" role="dialog" aria-modal="true" :aria-label="`分镜${sceneOrder}逐帧浏览器`">
    <header class="frame-browser-head">
      <div>
        <span class="overline">FRAME REVIEW</span>
        <h3>分镜 {{ sceneOrder }} · 查看全部帧</h3>
        <p>从当前正式分镜视频逐帧提取。选择一帧后可将独立图片保存到项目磁盘，原视频不会改变。</p>
      </div>
      <button type="button" class="btn btn-sm btn-ghost" :disabled="extracting || saving" @click="$emit('close')">关闭</button>
    </header>

    <div class="frame-browser-actions">
      <button type="button" class="btn" :disabled="extracting || saving" @click="extract">
        {{ extracting ? '正在提取全部帧…' : frames.length ? '重新提取当前视频全部帧' : '提取当前视频全部帧' }}
      </button>
      <span v-if="frames.length" class="frame-count">共 {{ frames.length }} 帧 · {{ frameRate }} fps</span>
      <span v-if="selectedFrame" class="frame-selection">已选第 {{ selectedFrame.index + 1 }} 帧（{{ timeLabel(selectedFrame.timestamp_ms) }}）</span>
    </div>

    <div v-if="extracting" class="frame-progress">
      <span class="pulse-dot"></span>
      FFmpeg 正在逐帧读取当前视频。长分镜可能需要一些时间，请不要关闭窗口。
    </div>

    <div v-if="cacheDir" class="frame-cache-hint">
      <div>
        <strong>这批帧存放在缓存目录：</strong>
        <code>{{ cacheDir }}</code>
        <button type="button" class="btn btn-xs btn-ghost" @click="copyCacheDir">{{ cacheDirCopied ? '已复制' : '复制路径' }}</button>
      </div>
      <small>
        该目录只存放浏览用的临时帧，不含项目素材，随时可以整个删除。保存过的帧已另存进项目素材目录，不受影响。
        <template v-if="purgedFrames > 0">本次已自动清理上一批 {{ purgedFrames }} 张旧帧。</template>
      </small>
    </div>
    <p v-if="error" class="frame-error">{{ error }}</p>

    <div v-if="selectedFrame" class="selected-frame-preview">
      <img :src="selectedFrame.url" :alt="`当前选中第${selectedFrame.index + 1}帧`" />
      <div><strong>当前选中：第 {{ selectedFrame.index + 1 }} 帧</strong><span>{{ timeLabel(selectedFrame.timestamp_ms) }}</span><a :href="selectedFrame.url" target="_blank" rel="noopener">单独打开原图</a></div>
    </div>

    <nav v-if="pageCount > 1" class="frame-pager" aria-label="视频帧分页">
      <button type="button" class="btn btn-xs btn-ghost" :disabled="page <= 1" @click="page--">上一页</button>
      <span>第 {{ page }} / {{ pageCount }} 页</span>
      <button type="button" class="btn btn-xs btn-ghost" :disabled="page >= pageCount" @click="page++">下一页</button>
    </nav>

    <div v-if="frames.length" class="frame-grid" aria-label="当前分镜全部视频帧">
      <button
        v-for="frame in visibleFrames"
        :key="frame.file"
        type="button"
        class="frame-item"
        :class="{ selected: frame.index === selectedFrameIndex }"
        @click="selectedFrameIndex = frame.index"
      >
        <img :src="frame.url" :alt="`分镜${sceneOrder}第${frame.index + 1}帧`" @error="onFrameImageError(frame)" />
        <span>第 {{ frame.index + 1 }} 帧</span>
        <small>{{ timeLabel(frame.timestamp_ms) }}</small>
      </button>
    </div>
    <div v-else-if="!extracting" class="frame-empty">点击“提取当前视频全部帧”后，这里会按时间顺序显示每一帧。</div>

    <footer class="frame-browser-footer">
      <div v-if="savedFrame" class="saved-frame-result">
        <strong>已保存：{{ savedFrame.file }}</strong>
        <a class="btn btn-sm btn-secondary" :href="`${savedFrame.url}?download=1`" download>下载已保存帧</a>
      </div>
      <div class="frame-save-actions">
        <button type="button" class="btn btn-secondary" :disabled="saving || extracting || !selectedFrame" @click="saveSelected(false)">
          {{ saving ? '保存中…' : '保存所选帧到磁盘' }}
        </button>
        <button type="button" class="btn btn-primary" :disabled="saving || extracting || !selectedFrame" @click="saveSelected(true)">
          {{ saving ? '设置中…' : '设为当前分镜图' }}
        </button>
      </div>
    </footer>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { api } from '../api'

const props = defineProps({
  projectId: { type: Number, required: true },
  sceneId: { type: Number, required: true },
  sceneOrder: { type: Number, required: true }
})
const emit = defineEmits(['close', 'saved'])
const frames = ref([])
const extractionId = ref('')
const frameRate = ref(24)
const selectedFrameIndex = ref(-1)
const extracting = ref(false)
const saving = ref(false)
const savedFrame = ref(null)
const error = ref('')
const page = ref(1)
const pageSize = 48
const cacheDir = ref('')
const purgedFrames = ref(0)
const cacheDirCopied = ref(false)

const selectedFrame = computed(() => frames.value.find(frame => frame.index === selectedFrameIndex.value) || null)
const pageCount = computed(() => Math.max(1, Math.ceil(frames.value.length / pageSize)))
const visibleFrames = computed(() => frames.value.slice((page.value - 1) * pageSize, page.value * pageSize))

async function extract() {
  extracting.value = true
  error.value = ''
  savedFrame.value = null
  try {
    const { data } = await api.extractAllSceneFrames(props.projectId, props.sceneId)
    extractionId.value = String(data.extraction_id || '')
    frameRate.value = Number(data.frame_rate || 24)
    cacheDir.value = String(data.cache_dir || '')
    purgedFrames.value = Number(data.purged_frames || 0)
    cacheDirCopied.value = false
    frames.value = Array.isArray(data.frames) ? data.frames : []
    page.value = 1
    selectedFrameIndex.value = frames.value.length ? frames.value[0].index : -1
    if (!frames.value.length) error.value = '当前视频没有提取到可显示的视频帧。'
  } catch (cause) {
    error.value = cause.response?.data?.error || cause.message || '提取视频帧失败'
  } finally {
    extracting.value = false
  }
}

async function saveSelected(setAsSceneImage) {
  if (!selectedFrame.value || !extractionId.value) return
  saving.value = true
  error.value = ''
  try {
    const { data } = await api.saveExtractedSceneFrame(props.projectId, props.sceneId, extractionId.value, selectedFrame.value.index, setAsSceneImage)
    savedFrame.value = data.frame || null
    emit('saved', savedFrame.value, Boolean(data.set_as_scene_image))
  } catch (cause) {
    error.value = cause.response?.data?.error || cause.message || '保存所选帧失败'
  } finally {
    saving.value = false
  }
}

async function copyCacheDir() {
  if (!cacheDir.value) return
  try {
    await navigator.clipboard.writeText(cacheDir.value)
    cacheDirCopied.value = true
  } catch {
    // 剪贴板不可用（非安全上下文等）时路径仍然显示在界面上，用户可手动选取。
    cacheDirCopied.value = false
  }
}

function onFrameImageError(frame) {
  error.value = `第 ${frame.index + 1} 帧图片加载失败：${frame.url}`
}

function timeLabel(ms) {
  return `${(Number(ms || 0) / 1000).toFixed(3)} 秒`
}
</script>

<style scoped>
.frame-browser{display:flex;flex-direction:column;box-sizing:border-box;width:100%;height:100%;min-height:0;overflow:hidden;padding:20px;background:var(--card);color:var(--text-primary);border-radius:14px}.frame-browser-head{display:flex;justify-content:space-between;gap:20px;align-items:flex-start}.frame-browser-head h3{margin:4px 0 6px}.frame-browser-head p{margin:0;color:var(--text-secondary);font-size:13px;line-height:1.6}.frame-browser-actions{display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin:16px 0}.frame-count,.frame-selection{font-size:12px;color:var(--text-secondary)}.frame-progress,.frame-error,.frame-empty{padding:12px;border-radius:9px}.frame-progress{background:rgba(33,150,243,.1);color:var(--accent)}.frame-error{background:rgba(244,67,54,.12);color:var(--red)}.frame-cache-hint{margin-bottom:10px;padding:10px 12px;border:1px dashed var(--border);border-radius:9px;background:var(--bg-secondary)}.frame-cache-hint>div{display:flex;align-items:center;gap:8px;flex-wrap:wrap}.frame-cache-hint code{padding:2px 6px;border-radius:5px;background:#000;color:#ddd;font-size:12px;word-break:break-all}.frame-cache-hint small{display:block;margin-top:6px;color:var(--text-tertiary);font-size:11px;line-height:1.6}.frame-empty{border:1px dashed var(--border);color:var(--text-tertiary);text-align:center}.selected-frame-preview{display:grid;grid-template-columns:minmax(360px,560px) 1fr;gap:14px;align-items:center;flex:0 0 auto;margin-bottom:10px;padding:10px;border:1px solid var(--border);border-radius:10px;background:#050505}.selected-frame-preview img{display:block;width:100%;max-height:260px;object-fit:contain;background:#000}.selected-frame-preview div{display:flex;flex-direction:column;gap:8px;color:#fff}.selected-frame-preview span,.selected-frame-preview a{font-size:12px;color:#ddd}.frame-pager{display:flex;justify-content:center;align-items:center;gap:10px;flex:0 0 auto;margin:2px 0 8px;font-size:12px}.pulse-dot{display:inline-block;width:8px;height:8px;margin-right:6px;border-radius:50%;background:var(--accent);animation:pulse 1s infinite}.frame-grid{display:grid;grid-template-columns:repeat(auto-fill,420px);grid-auto-rows:max-content;justify-content:start;align-content:start;gap:14px;flex:1 1 auto;min-height:0;padding:6px 14px 18px 6px;overflow-y:scroll;overflow-x:auto;overscroll-behavior:contain;scrollbar-gutter:stable both-edges}.frame-item{display:grid;grid-template-columns:1fr auto;width:420px;align-self:start;padding:0;overflow:hidden;border:2px solid transparent;border-radius:9px;background:var(--bg-secondary);color:var(--text-secondary);cursor:pointer;text-align:left}.frame-item:hover{border-color:var(--border-strong)}.frame-item.selected{border-color:var(--accent);box-shadow:0 0 0 2px rgba(33,150,243,.15)}.frame-item img{grid-column:1/-1;display:block;width:100%;height:auto;object-fit:contain;background:#000}.frame-grid::-webkit-scrollbar{width:14px;height:14px}.frame-grid::-webkit-scrollbar-thumb{border:3px solid transparent;border-radius:999px;background:var(--text-tertiary);background-clip:padding-box}.frame-grid::-webkit-scrollbar-track{background:var(--bg-secondary)}.frame-item span,.frame-item small{padding:7px;font-size:11px}.frame-item small{text-align:right;color:var(--text-tertiary)}.frame-browser-footer{display:flex;justify-content:space-between;align-items:center;gap:12px;margin-top:14px;padding-top:14px;border-top:1px solid var(--border)}.frame-save-actions{display:flex;gap:8px;flex-wrap:wrap}.saved-frame-result{display:flex;align-items:center;gap:10px;min-width:0}.saved-frame-result strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:12px}@keyframes pulse{50%{opacity:.35}}@media(max-width:720px){.frame-browser{height:100%;padding:14px}.frame-browser-head{gap:10px}.frame-grid{grid-template-columns:minmax(280px,1fr)}.frame-item{width:100%}.selected-frame-preview{grid-template-columns:1fr}.frame-browser-footer,.saved-frame-result{align-items:stretch;flex-direction:column}}
</style>
