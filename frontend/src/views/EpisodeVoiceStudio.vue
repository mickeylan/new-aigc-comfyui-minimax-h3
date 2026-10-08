<template>
  <div class="page voice-page">
    <header class="voice-head">
      <div>
        <router-link :to="`/projects/${projectId}/episodes/${episodeN}/screenplay`" class="back">← 返回本集剧本</router-link>
        <span class="overline">VOICE STUDIO</span>
        <h1>第{{ episodeN }}集 · 声音工作台</h1>
        <p class="sub">{{ episodeTitle || '集中完成角色配音、试听、字幕检查与成片混音。' }} · 引擎：{{ providerLabel }}</p>
        <p v-if="studio.tts_provider === 'index_tts_rust' && studio.tts_available === false" class="runtime-warning">本地配音暂不可用：{{ studio.tts_error || '运行时未加载' }}。剧本、图片、视频、导演和混音等其他功能不受影响。</p>
      </div>
      <div class="head-actions">
        <button class="btn btn-ghost" :disabled="loading || busy" @click="refresh">刷新状态</button>
        <button class="btn btn-secondary" :disabled="busy || !pendingCount || ttsUnavailable" @click="generateBatch(true)">生成待更新 ({{ pendingCount }})</button>
        <button class="btn" :disabled="busy || !dialogues.length || ttsUnavailable" @click="generateBatch(false)">全部重新生成</button>
      </div>
    </header>

    <div v-if="loading" class="card empty" role="status">正在加载声音工作台…</div>
    <template v-else>
      <section class="summary-grid" aria-label="配音进度">
        <div class="card metric"><span>对白总数</span><strong>{{ dialogues.length }}</strong></div>
        <div class="card metric ready"><span>已就绪</span><strong>{{ readyCount }}</strong></div>
        <div class="card metric pending"><span>待处理</span><strong>{{ pendingCount }}</strong></div>
        <div class="card metric failed"><span>失败</span><strong>{{ failedCount }}</strong></div>
        <div class="card metric overflow"><span>槽位超时</span><strong>{{ slotQASummary.overflow_count || 0 }}</strong></div>
      </section>
      <div class="progress-track" :aria-label="`配音完成 ${progressPercent}%`"><span :style="{ width: progressPercent + '%' }"></span></div>
      <p class="qa-summary">时长 QA：已核验 {{ slotQASummary.evaluated || 0 }} 句；过期 {{ slotQASummary.stale_count || 0 }} 句；缺失音频 {{ slotQASummary.missing_count || 0 }} 句；缺少实际时长 {{ slotQASummary.duration_missing_count || 0 }} 句。</p>
      <p v-if="studio.asr_available === false" class="qa-summary">ASR QA：{{ studio.asr_status }}</p>

      <section class="section batch-section" aria-label="批量配音任务">
        <div class="section-head"><div><span class="overline">BATCHES</span><h2>批量任务</h2><p class="sub">任务状态由服务端持久保存，离开页面后仍可继续查看和重试。</p></div></div>
        <div v-if="dubBatches.length" class="batch-list">
          <article v-for="batch in dubBatches" :key="batchId(batch)" class="card batch-card">
            <div>
              <strong>批次 #{{ batchId(batch) }}</strong>
              <span class="state" :class="batchStatus(batch)">{{ batchStatusLabel(batch) }}</span>
              <small v-if="batch.created_at || batch.started_at">{{ formatBatchTime(batch.created_at || batch.started_at) }}</small>
            </div>
            <div class="batch-progress">
              <span>{{ batchCompleted(batch) }} / {{ batchTotal(batch) || '—' }} 句</span>
              <span v-if="batchFailed(batch)" class="error-text">{{ batchFailed(batch) }} 句失败</span>
            </div>
            <p v-if="batch.error || batch.last_error" class="error-text">{{ batch.error || batch.last_error }}</p>
            <button v-if="canRetryBatch(batch)" class="btn btn-sm btn-secondary" :disabled="retryingBatchId === batchId(batch) || ttsUnavailable" @click="retryBatch(batch)">{{ retryingBatchId === batchId(batch) ? '重试中…' : '重试失败项' }}</button>
          </article>
        </div>
        <div v-else class="card empty compact">暂无批量配音任务。</div>
      </section>

      <section class="section">
        <div class="section-head"><div><span class="overline">CAST</span><h2>本集角色与音色</h2></div></div>
        <div class="role-grid" v-if="roles.length">
          <article v-for="role in roles" :key="role.name" class="card role-card">
            <strong>{{ role.name }}</strong><span>{{ role.count }} 句</span>
            <small>{{ role.voice || '默认角色映射' }}</small>
            <span class="badge" :class="role.missing ? 'warn' : 'badge-gray'">{{ role.missing ? '音色待配置' : '音色已映射' }}</span>
          </article>
        </div>
        <div v-else class="card empty">本集暂无可发声角色。</div>
      </section>

      <section class="section">
        <div class="section-head dialogue-head">
          <div><span class="overline">DIALOGUES</span><h2>对白制作</h2><p class="sub">语速、情绪与表演说明保存后会使旧音频标记为待更新。{{ studio.tts_emotion_supported ? '当前本地运行时会将情绪与表演说明作为原生情感文本条件。' : '当前运行时不支持情感条件，参数仅用于审核且不会伪装为已生效。' }}</p></div>
          <div class="filters">
            <select v-model="statusFilter" class="input" aria-label="按状态筛选">
              <option value="all">全部状态</option><option value="ready">已就绪</option><option value="pending">待生成</option><option value="synthesizing">生成中</option><option value="failed">失败</option>
            </select>
            <select v-model="roleFilter" class="input" aria-label="按角色筛选"><option value="all">全部角色</option><option v-for="role in roles" :key="role.name" :value="role.name">{{ role.name }}</option></select>
            <input v-model.trim="query" class="input" placeholder="搜索对白" aria-label="搜索对白" />
          </div>
        </div>

        <div class="dialogue-list" v-if="filteredDialogues.length">
          <article v-for="d in filteredDialogues" :key="d.id" class="card dialogue-card" :class="`status-${normalizedStatus(d)}`">
            <div class="dialogue-meta">
              <div><strong>{{ speakerLabel(d) }}</strong><span>场景 {{ d.scene_order || d.scene?.order || '—' }} · #{{ d.order }}</span></div>
              <span class="state" :class="normalizedStatus(d)">{{ statusLabel(d) }}</span>
            </div>
            <p class="dialogue-text">{{ d.text }}</p>
            <div class="edit-grid">
              <label>语速
                <input v-model.number="d.speed" class="input" type="number" min="0.5" max="2" step="0.1" @input="d._dirty = true" />
              </label>
              <label>情绪
                <input v-model="d.emotion" class="input" placeholder="如：克制、愤怒" @input="d._dirty = true" />
              </label>
              <label class="delivery">表演说明
                <input v-model="d.delivery" class="input" placeholder="停顿、重音或语气说明" @input="d._dirty = true" />
              </label>
            </div>
            <p v-if="d.error" class="error-text">{{ d.error }}</p>
            <p v-else-if="d.audio_stale_reason" class="stale-text">{{ d.audio_stale_reason }}</p>
            <p v-if="d._slot_qa" class="slot-qa" :class="`qa-${d._slot_qa.status}`">
              时长 QA：{{ slotQALabel(d._slot_qa) }}
              <span v-if="d._slot_qa.playback_duration != null"> · 实际 {{ formatSeconds(d._slot_qa.audio_duration) }} 秒 ÷ {{ d._slot_qa.speed }}x = {{ formatSeconds(d._slot_qa.playback_duration) }} 秒</span>
              · 槽位 {{ formatSeconds(d._slot_qa.slot_duration) }} 秒
              <span v-if="d._slot_qa.overflow_seconds > 0"> · 超出 {{ formatSeconds(d._slot_qa.overflow_seconds) }} 秒</span>
            </p>
            <div class="dialogue-actions">
              <audio v-if="audioSource(d)" :src="audioSource(d)" controls preload="none">浏览器不支持音频播放。</audio>
              <span v-else class="no-audio">暂无可试听音频</span>
              <button class="btn btn-sm btn-ghost" :disabled="savingId === d.id || busy" @click="saveDialogue(d)">{{ savingId === d.id ? '保存中…' : '保存参数' }}</button>
              <button class="btn btn-sm btn-secondary" :disabled="busy || savingId === d.id || ttsUnavailable" @click="generateOne(d)">生成此句</button>
              <button class="btn btn-sm btn-ghost" :disabled="candidateLoadingId === d.id" @click="toggleCandidates(d)">{{ candidateDialogueId === d.id ? '收起候选' : '试听候选' }}</button>
            </div>
            <div v-if="candidateDialogueId === d.id" class="candidate-list">
              <div v-if="candidateLoadingId === d.id" class="inline-status">正在加载配音候选…</div>
              <article v-for="candidate in audioCandidates" :key="candidate.id" class="candidate" :class="{ current: candidate.is_current, stale: candidate.stale }">
                <div><strong>{{ candidate.provider === 'index_tts_rust' ? '本地 IndexTTS' : candidate.provider }}</strong><span>{{ candidate.duration ? candidate.duration.toFixed(2) + ' 秒' : '时长待探测' }}</span><span>{{ candidate.review_status === 'accepted' ? '已采用' : candidate.review_status === 'rejected' ? '已拒绝' : '待审核' }}</span></div>
                <audio :src="candidate.file" controls preload="none"></audio>
                <div class="candidate-actions"><button class="btn btn-sm" :disabled="candidate.is_current || candidate.stale || candidate.review_status === 'rejected'" @click="selectCandidate(d, candidate)">采用此版本</button><button class="btn btn-sm btn-ghost" :disabled="candidate.is_current" @click="reviewCandidate(d, candidate, 'rejected')">拒绝</button></div>
                <small v-if="candidate.stale" class="stale-text">已过期：{{ candidate.stale_reason }}</small>
              </article>
              <p v-if="!candidateLoadingId && !audioCandidates.length" class="sub">暂无历史候选；下一次生成后会自动保留。</p>
            </div>
          </article>
        </div>
        <div v-else class="card empty">没有符合筛选条件的对白。</div>
      </section>

      <section class="studio-bottom">
        <article class="card srt-panel">
          <div class="section-head"><div><span class="overline">SRT PREFLIGHT</span><h2>SRT 文件预览</h2><p class="sub">仅检查，不会覆盖现有对白。</p></div></div>
          <label class="file-picker">选择 .srt 文件<input type="file" accept=".srt,application/x-subrip,text/plain" @change="previewSRT" /></label>
          <div v-if="srtLoading" class="inline-status" role="status">正在分析字幕…</div>
          <template v-if="srtPreview">
            <div class="diagnostics" :class="{ clean: !srtDiagnostics.length }">
              <strong>{{ srtDiagnostics.length ? `发现 ${srtDiagnostics.length} 项诊断` : '未发现字幕格式问题' }}</strong>
              <ul v-if="srtDiagnostics.length"><li v-for="(item, i) in srtDiagnostics" :key="i">{{ diagnosticText(item) }}</li></ul>
            </div>
            <h3>角色映射</h3>
            <div v-if="roleMappings.length" class="mapping-list"><div v-for="(mapping, i) in roleMappings" :key="i"><strong>{{ mapping.source }}</strong><span>→</span><span>{{ mapping.target || '未匹配' }}</span></div></div>
            <p v-else class="sub">未检测到角色映射。</p>
            <h3>字幕条目</h3>
            <div class="cue-list"><div v-for="(cue, i) in srtCues.slice(0, 30)" :key="cue.index || i"><time>{{ cue.start || cue.start_time }} → {{ cue.end || cue.end_time }}</time><span>{{ cue.character ? cue.character + '：' : '' }}{{ cue.text }}</span></div></div>
            <button class="btn" :disabled="srtApplying || srtDiagnostics.length || !srtPreview.preview_token" @click="applySRTPreview">{{ srtApplying ? '正在应用…' : '应用配音参数' }}</button>
            <p class="sub">仅应用语速、情绪、表达和显式音色，不覆盖结构化 Dialogue 原文、角色、顺序或场景。</p>
          </template>
        </article>

        <article class="card mix-panel">
          <div class="section-head"><div><span class="overline">MIX & EXPORT</span><h2>混音与导出</h2><p class="sub">使用本集已就绪场景创建成片，或单独下载字幕。</p></div></div>
          <label><input v-model="mix.subtitles" type="checkbox" /> 烧录字幕</label>
          <label><input v-model="mix.dub" type="checkbox" /> 保留视频原声</label>
          <label>原声音量 <input v-model.number="mix.nativeVolume" class="input" type="number" min="0" max="4" step="0.1" /></label>
          <label>对白音量 <input v-model.number="mix.dialogueVolume" class="input" type="number" min="0" max="4" step="0.1" /></label>
          <label>BGM 音量 <input v-model.number="mix.bgmVolume" class="input" type="number" min="0" max="4" step="0.1" /></label>
          <div class="mix-actions">
            <button class="btn" :disabled="mixing || !mixSceneIds.length" @click="mixEpisode">{{ mixing ? '正在提交…' : '创建本集混音成片' }}</button>
            <a class="btn btn-secondary" :href="api.srtUrl(projectId, episodeN)" download>导出 SRT</a>
            <button class="btn btn-secondary" :disabled="exporting" @click="exportAudio(false)">{{ exporting === 'dialogue' ? '正在导出…' : '导出对白 WAV' }}</button>
            <button class="btn btn-secondary" :disabled="exporting" @click="exportAudio(true)">{{ exporting === 'stems' ? '正在导出…' : '导出角色分轨' }}</button>
            <router-link class="btn btn-ghost" :to="`/projects/${projectId}/editor?episode=${episodeN}`">进入剪辑台</router-link>
          </div>
          <div v-if="exportFiles.length" class="export-files">
            <strong>导出文件</strong>
            <a v-for="file in exportFiles" :key="file.url" :href="file.url" download>{{ file.character ? `${file.character} 分轨` : '对白 WAV' }}</a>
          </div>
          <p v-if="!mixSceneIds.length" class="sub">暂无可用于混音的已就绪场景。</p>
        </article>
      </section>
    </template>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
import { useToastStore } from '../stores/toast'

const route = useRoute()
const toast = useToastStore()
const projectId = Number(route.params.id)
const episodeN = Number(route.params.episode)
const loading = ref(true)
const busy = ref(false)
const savingId = ref(0)
const mixing = ref(false)
const exporting = ref('')
const exportFiles = ref([])
const studio = ref({})
const dialogues = ref([])
const dubBatches = ref([])
const retryingBatchId = ref(0)
const statusFilter = ref('all')
const roleFilter = ref('all')
const query = ref('')
const srtLoading = ref(false)
const srtApplying = ref(false)
const srtPreview = ref(null)
const candidateDialogueId = ref(0)
const candidateLoadingId = ref(0)
const audioCandidates = ref([])
const mix = reactive({ subtitles: true, dub: true, nativeVolume: 1, dialogueVolume: 1, bgmVolume: 1 })
let pollTimer = 0

const episodeTitle = computed(() => studio.value.episode?.title || studio.value.episode_title || '')
const providerLabel = computed(() => studio.value.tts_provider === 'index_tts_rust' ? (studio.value.tts_available === false ? '本地 IndexTTS-2.5（不可用）' : '本地 IndexTTS-2.5 CUDA') : '兼容语音服务')
const ttsUnavailable = computed(() => studio.value.tts_provider === 'index_tts_rust' && studio.value.tts_available === false)
const normalizedStatus = d => d.status === 'ready' && !d.audio_stale && (d.audio_url || d.audio_file) ? 'ready' : d.status === 'synthesizing' ? 'synthesizing' : d.status === 'failed' ? 'failed' : 'pending'
const readyCount = computed(() => dialogues.value.filter(d => normalizedStatus(d) === 'ready').length)
const failedCount = computed(() => dialogues.value.filter(d => normalizedStatus(d) === 'failed').length)
const pendingCount = computed(() => dialogues.value.filter(d => normalizedStatus(d) !== 'ready').length)
const progressPercent = computed(() => dialogues.value.length ? Math.round(readyCount.value * 100 / dialogues.value.length) : 0)
const slotQASummary = computed(() => studio.value.slot_qa_summary || {})
const slotQALabel = qa => ({ ok: '通过', overflow: '超出槽位', stale: '音频已过期', missing: '音频缺失/未就绪', duration_missing: '实际时长缺失' })[qa.status] || qa.message || '待核验'
const formatSeconds = value => Number(value || 0).toFixed(3)
const speakerLabel = d => d.speech_type === 'narration' ? '旁白' : d.speech_type === 'offscreen_dialogue' ? `${d.character || '未指定'}（画外）` : d.character || (d.speech_type === 'monologue' ? '内心独白' : '未指定说话人')
const statusLabel = d => ({ ready: '已就绪', pending: d.audio_stale ? '待更新' : '待生成', synthesizing: '生成中', failed: '生成失败' })[normalizedStatus(d)]
const audioSource = d => {
  const value = d.audio_url || d.audio_file || ''
  if (!value || /^https?:\/\//.test(value) || value.startsWith('/')) return value
  return api.dubAudioUrl(projectId, value)
}
const rawRoles = computed(() => studio.value.roles || studio.value.role_summary || studio.value.cast || [])
const roles = computed(() => {
  if (rawRoles.value.length) return rawRoles.value.map(r => ({ name: r.name || r.character || r.role || '未指定说话人', count: r.dialogue_count ?? r.count ?? 0, voice: r.voice_name || r.voice || r.voice_id || '', missing: r.voice_configured === false || r.missing_voice === true }))
  const grouped = new Map()
  for (const d of dialogues.value) { const name = speakerLabel(d); const row = grouped.get(name) || { name, count: 0, voice: d.voice || '', missing: !d.voice }; row.count++; grouped.set(name, row) }
  return [...grouped.values()]
})
const filteredDialogues = computed(() => dialogues.value.filter(d => (statusFilter.value === 'all' || normalizedStatus(d) === statusFilter.value) && (roleFilter.value === 'all' || speakerLabel(d) === roleFilter.value) && (!query.value || `${speakerLabel(d)} ${d.text}`.toLowerCase().includes(query.value.toLowerCase()))))
const mixSceneIds = computed(() => {
  const source = studio.value.mix_scene_ids || studio.value.ready_scene_ids
  if (Array.isArray(source)) return source
  return (studio.value.scenes || []).filter(s => s.status === 'video_ready' || s.video_ready || s.video_url || s.video_file).map(s => s.id)
})
const srtDiagnostics = computed(() => srtPreview.value?.diagnostics || srtPreview.value?.issues || srtPreview.value?.errors || [])
const roleMappings = computed(() => {
  const value = srtPreview.value?.role_mapping || srtPreview.value?.role_mappings || srtPreview.value?.mapping || []
  if (Array.isArray(value)) return value.map(row => ({ source: row.source || row.srt_role || row.name || '', target: row.target || row.character || row.matched_role || '' }))
  return Object.entries(value).map(([source, target]) => ({ source, target: typeof target === 'string' ? target : target?.character || target?.target || '' }))
})
const srtCues = computed(() => srtPreview.value?.cues || srtPreview.value?.entries || srtPreview.value?.subtitles || [])
const diagnosticText = item => typeof item === 'string' ? item : [item.line ? `第 ${item.line} 行` : '', item.message || item.error || item.code].filter(Boolean).join('：')
const batchId = batch => batch.id ?? batch.batch_id ?? batch.uuid
const batchStatus = batch => String(batch.status || batch.state || 'pending').toLowerCase()
const batchStatusLabel = batch => ({ queued: '排队中', pending: '等待中', running: '生成中', processing: '生成中', completed: '已完成', succeeded: '已完成', partial: '部分失败', failed: '失败', cancelled: '已取消', canceled: '已取消' })[batchStatus(batch)] || batch.status || batch.state || '等待中'
const batchTotal = batch => Number(batch.total_items ?? batch.total_count ?? batch.total ?? batch.count ?? 0)
const batchFailed = batch => Number(batch.failed_items ?? batch.failed_count ?? batch.failed ?? 0)
const batchCompleted = batch => Number(batch.completed_items ?? batch.completed_count ?? batch.succeeded_count ?? batch.ready_count ?? batch.completed ?? Math.max(0, batchTotal(batch) - batchFailed(batch) - Number(batch.pending_count ?? 0)))
const batchActive = batch => ['queued', 'pending', 'running', 'processing'].includes(batchStatus(batch))
const canRetryBatch = batch => batchFailed(batch) > 0 || ['failed', 'partial', 'cancelled', 'canceled'].includes(batchStatus(batch))
const formatBatchTime = value => value ? new Date(value).toLocaleString() : ''

async function load({ quiet = false } = {}) {
  if (!quiet) loading.value = true
  try {
    const { data } = await api.episodeVoiceStudio(projectId, episodeN)
    studio.value = data || {}
    const rows = data?.dialogues || (data?.scenes || []).flatMap(scene => (scene.dialogues || []).map(d => ({ ...d, scene_order: d.scene_order || scene.order })))
    const current = new Map(dialogues.value.map(d => [d.id, d]))
    const slotQA = new Map((data?.dialogue_slot_qa || []).map(row => [row.dialogue_id, row]))
    dialogues.value = rows.map(d => {
      const draft = current.get(d.id)
      const next = { ...d, speed: Number(d.speed) || 1, emotion: d.emotion || '', delivery: d.delivery || '', _slot_qa: slotQA.get(d.id) }
      return draft?._dirty ? { ...next, speed: draft.speed, emotion: draft.emotion, delivery: draft.delivery, _dirty: true } : next
    })
  } catch (e) { toast.error(e.response?.data?.error || '加载声音工作台失败') }
  finally { if (!quiet) loading.value = false }
}
async function loadBatchStatus({ quiet = false } = {}) {
  try {
    const { data } = await api.episodeDubBatches(projectId, episodeN)
    const rows = Array.isArray(data) ? data : data?.batches || data?.items || (data?.batch ? [data.batch] : [])
    dubBatches.value = rows.slice(0, 10)
  } catch (e) {
    if (!quiet && e.response?.status !== 404) toast.error(e.response?.data?.error || '加载批量任务失败')
  }
}
async function refresh({ quiet = false } = {}) {
  await Promise.all([load({ quiet }), loadBatchStatus({ quiet })])
}
async function saveDialogue(d, notify = true) {
  savingId.value = d.id
  try {
    const { data } = await api.updateDialogue(projectId, d.id, { speed: Number(d.speed), emotion: d.emotion.trim(), delivery: d.delivery.trim() })
    Object.assign(d, data, { speed: Number(data.speed) || 1, emotion: data.emotion || '', delivery: data.delivery || '', _dirty: false })
    if (notify) toast.success('对白表演参数已保存')
    return true
  } catch (e) { toast.error(e.response?.data?.error || '保存对白参数失败'); return false }
  finally { savingId.value = 0 }
}
async function generateOne(d) {
  busy.value = true
  try {
    if (!await saveDialogue(d, false)) return
    await api.redubDialogue(projectId, d.id)
    d.status = 'synthesizing'; d.error = ''
    toast.show('单句配音已提交')
  } catch (e) { toast.error(e.response?.data?.error || '单句配音提交失败') }
  finally { busy.value = false }
}
async function generateBatch(staleOnly) {
  busy.value = true
  try { const { data } = await api.generateEpisodeDub(projectId, episodeN, staleOnly); toast.show(data.message || `已提交 ${data.count || 0} 条配音`); await refresh() }
  catch (e) { toast.error(e.response?.data?.error || '批量配音提交失败') }
  finally { busy.value = false }
}
async function retryBatch(batch) {
  const id = batchId(batch)
  retryingBatchId.value = id
  try { const { data } = await api.retryEpisodeDubBatch(projectId, episodeN, id); toast.show(data?.message || '失败配音已重新提交'); await refresh({ quiet: true }) }
  catch (e) { toast.error(e.response?.data?.error || '重试批量配音失败') }
  finally { retryingBatchId.value = 0 }
}
async function loadCandidates(dialogue) {
  candidateDialogueId.value = dialogue.id; candidateLoadingId.value = dialogue.id
  try { const { data } = await api.dialogueAudioCandidates(projectId, dialogue.id); audioCandidates.value = data || [] }
  catch (e) { toast.error(e.response?.data?.error || '加载配音候选失败') }
  finally { candidateLoadingId.value = 0 }
}
async function toggleCandidates(dialogue) {
  if (candidateDialogueId.value === dialogue.id) { candidateDialogueId.value = 0; audioCandidates.value = []; return }
  audioCandidates.value = []; await loadCandidates(dialogue)
}
async function selectCandidate(dialogue, candidate) {
  try { await api.selectDialogueAudioCandidate(projectId, dialogue.id, candidate.id); toast.success('已采用所选配音版本'); await load({ quiet: true }); await loadCandidates(dialogue) }
  catch (e) { toast.error(e.response?.data?.error || '采用配音候选失败') }
}
async function reviewCandidate(dialogue, candidate, status) {
  try { await api.reviewDialogueAudioCandidate(projectId, dialogue.id, candidate.id, { status, reason: status === 'rejected' ? '声音工作台人工拒绝' : '' }); await loadCandidates(dialogue) }
  catch (e) { toast.error(e.response?.data?.error || '审核配音候选失败') }
}
async function previewSRT(event) {
  const file = event.target.files?.[0]
  if (!file) return
  srtLoading.value = true; srtPreview.value = null
  try { const { data } = await api.previewEpisodeSRT(projectId, episodeN, file); srtPreview.value = data }
  catch (e) { toast.error(e.response?.data?.error || 'SRT 预览失败') }
  finally { srtLoading.value = false; event.target.value = '' }
}
async function applySRTPreview() {
  if (!srtPreview.value?.preview_token || srtDiagnostics.value.length) return
  if (!window.confirm('仅将已预览的语速、情绪、表达和显式音色应用到当前 Dialogue，原文与角色不会改变。是否继续？')) return
  srtApplying.value = true
  try { const { data } = await api.applyEpisodeSRT(projectId, episodeN, srtPreview.value.preview_token); toast.success(`已应用 ${data.applied || 0} 条配音参数`); srtPreview.value = null; await load() }
  catch (e) { toast.error(e.response?.data?.error || '应用 SRT 配音参数失败') }
  finally { srtApplying.value = false }
}
async function mixEpisode() {
  mixing.value = true
  try {
    await api.mergeAudioScenes(projectId, { scene_ids: mixSceneIds.value, dub: mix.dub, subtitles: mix.subtitles, native_volume: Number(mix.nativeVolume), dialogue_volume: Number(mix.dialogueVolume), bgm_volume: Number(mix.bgmVolume) })
    toast.success(`第${episodeN}集混音成片已提交`)
  } catch (e) { toast.error(e.response?.data?.error || '提交混音失败') }
  finally { mixing.value = false }
}
async function exportAudio(stems) {
  exporting.value = stems ? 'stems' : 'dialogue'
  exportFiles.value = []
  try {
    const { data } = stems ? await api.exportEpisodeDialogueStems(projectId, episodeN) : await api.exportEpisodeDialogue(projectId, episodeN)
    exportFiles.value = stems ? (data?.stems || []) : (data?.dialogue ? [data.dialogue] : [])
    toast.success(stems ? '角色分轨已导出' : '对白 WAV 已导出')
  } catch (e) { toast.error(e.response?.data?.error || '导出对白音频失败') }
  finally { exporting.value = '' }
}

onMounted(async () => {
  await refresh()
  pollTimer = window.setInterval(() => {
    if (dialogues.value.some(d => normalizedStatus(d) === 'synthesizing') || dubBatches.value.some(batchActive)) refresh({ quiet: true })
  }, 4000)
})
onUnmounted(() => window.clearInterval(pollTimer))
</script>

<style scoped>
.voice-page{max-width:1240px;margin:0 auto;padding-bottom:70px}.runtime-warning{max-width:760px;padding:9px 12px;border-radius:8px;color:var(--orange);background:var(--chip-bg)}.voice-head,.section-head,.dialogue-meta,.dialogue-actions,.head-actions,.filters,.mix-actions{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap}.voice-head{align-items:flex-end;margin-bottom:20px}.batch-section{margin-bottom:28px}.batch-list{display:grid;gap:10px}.batch-card{display:grid;grid-template-columns:minmax(220px,1fr) auto auto;align-items:center;gap:12px}.batch-card>div{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.batch-card small{color:var(--text-secondary)}.batch-progress{justify-content:flex-end}.empty.compact{padding:20px}.voice-head h1,.section-head h2{margin:6px 0}.back{display:block;margin-bottom:8px}.summary-grid{display:grid;grid-template-columns:repeat(5,1fr);gap:12px}.metric{display:flex;justify-content:space-between;align-items:center}.metric span{color:var(--text-secondary)}.metric strong{font-size:26px}.metric.ready{border-left:4px solid var(--green)}.metric.pending{border-left:4px solid var(--orange)}.metric.failed{border-left:4px solid var(--red)}.metric.overflow{border-left:4px solid var(--orange)}.progress-track{height:9px;margin:12px 0 30px;border-radius:99px;overflow:hidden;background:var(--chip-bg)}.progress-track span{display:block;height:100%;background:linear-gradient(90deg,var(--indigo),var(--green));transition:width .3s}.section{margin-top:28px}.role-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(190px,1fr));gap:12px}.role-card{display:grid;grid-template-columns:1fr auto;gap:7px}.role-card small,.role-card .badge{grid-column:1/-1}.dialogue-head{align-items:flex-end}.filters .input{width:auto;min-width:145px}.dialogue-list{display:grid;gap:12px}.dialogue-card{border-left:4px solid var(--border)}.dialogue-card.status-ready{border-left-color:var(--green)}.dialogue-card.status-failed{border-left-color:var(--red)}.dialogue-card.status-synthesizing{border-left-color:var(--indigo)}.dialogue-meta>div{display:flex;gap:10px;align-items:center}.dialogue-meta span,.no-audio{color:var(--text-secondary)}.state{padding:4px 9px;border-radius:99px;background:var(--chip-bg)}.state.ready{color:var(--green)}.state.failed,.error-text{color:var(--red)}.state.synthesizing{color:var(--indigo)}.dialogue-text{font-size:16px;line-height:1.7}.edit-grid{display:grid;grid-template-columns:140px minmax(180px,1fr) 2fr;gap:12px}.edit-grid label,.mix-panel>label{display:grid;gap:5px;color:var(--text-secondary);font-size:13px}.stale-text{color:var(--orange)}.qa-summary{margin:-18px 0 28px;color:var(--text-secondary)}.slot-qa{margin:8px 0;color:var(--text-secondary)}.slot-qa.qa-overflow,.slot-qa.qa-stale,.slot-qa.qa-missing,.slot-qa.qa-duration_missing{color:var(--orange)}.slot-qa.qa-ok{color:var(--green)}.dialogue-actions{justify-content:flex-start}.dialogue-actions audio{height:36px;max-width:360px}.candidate-list{display:grid;gap:10px;margin-top:14px;padding-top:14px;border-top:1px solid var(--border)}.candidate{display:grid;grid-template-columns:1fr minmax(220px,360px) auto;align-items:center;gap:12px;padding:10px;border:1px solid var(--border);border-radius:9px}.candidate.current{border-color:var(--green)}.candidate.stale{opacity:.65}.candidate>div{display:flex;gap:10px;align-items:center;flex-wrap:wrap}.candidate audio{width:100%;height:34px}.candidate-actions{justify-content:flex-end}.studio-bottom{display:grid;grid-template-columns:1.4fr 1fr;gap:18px;margin-top:30px}.file-picker{display:inline-flex;cursor:pointer;padding:9px 12px;border:1px dashed var(--border);border-radius:9px}.file-picker input{margin-left:10px}.inline-status,.diagnostics{margin:14px 0;padding:12px;border-radius:9px;background:var(--accent-soft)}.diagnostics.clean{color:var(--green)}.mapping-list,.cue-list{display:grid;gap:7px}.mapping-list>div{display:grid;grid-template-columns:1fr auto 1fr;gap:8px;padding:7px;background:var(--chip-bg);border-radius:7px}.cue-list{max-height:300px;overflow:auto}.cue-list>div{display:grid;grid-template-columns:180px 1fr;gap:10px;border-bottom:1px solid var(--border);padding:8px 0}.cue-list time{color:var(--text-secondary)}.mix-panel{display:flex;flex-direction:column;gap:12px}.export-files{display:flex;gap:10px;flex-wrap:wrap;padding:10px;border-radius:8px;background:var(--chip-bg)}.empty{text-align:center;padding:36px}@media(max-width:800px){.summary-grid{grid-template-columns:repeat(2,1fr)}.studio-bottom{grid-template-columns:1fr}.edit-grid{grid-template-columns:1fr}.dialogue-actions audio{width:100%;max-width:none}.cue-list>div{grid-template-columns:1fr}.voice-head{align-items:stretch;flex-direction:column}}
</style>
