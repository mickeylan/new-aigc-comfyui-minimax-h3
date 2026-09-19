<template>
  <section class="section director">
    <div class="section-head">
      <div><span class="overline">SHOT DIRECTOR</span><h2>镜头设计</h2><p class="sub">结构化编辑景别、运镜、对白与五段导演提示词；不机械切割场景正文</p></div>
      <div class="section-actions"><button class="btn btn-sm btn-secondary" @click="createFiveActSkeleton">创建五幕骨架</button><button class="btn btn-sm btn-secondary" @click="addShot">＋ 镜头</button><button class="btn btn-sm" :disabled="saving || !shots.length" @click="saveAll">{{ saving ? '保存中…' : '保存并应用到场景' }}</button></div>
    </div>
    <div v-if="loading" class="card empty">加载镜头…</div>
    <div v-else-if="!shots.length" class="card empty">暂无镜头。点击“＋ 镜头”开始导演设计。</div>
    <div v-for="(shot, index) in shots" :key="shot._key" class="card shot-card">
      <div class="shot-head"><strong>镜头 {{ index + 1 }} · {{ actLabel(shot.act_type) }}</strong><button class="btn btn-sm btn-danger" @click="removeShot(index)">删除</button></div>
      <div class="shot-grid five">
        <label>叙事幕<select v-model="shot.act_type" class="input"><option v-for="act in acts" :key="act.value" :value="act.value">{{ act.label }}</option></select></label>
        <label>景别<input v-model="shot.shot_type" class="input" placeholder="全景 / 中景 / 近景 / 特写" /></label>
        <label>机位角度<input v-model="shot.camera_angle" class="input" placeholder="平视 / 俯拍 / 仰拍" /></label>
        <label>运镜<input v-model="shot.camera_movement" class="input" placeholder="固定 / 推 / 拉 / 摇 / 跟" /></label>
        <label>时长（秒）<input v-model.number="shot.duration" class="input" type="number" min="0.1" max="120" step="0.1" /></label>
      </div>
      <label><span class="field-label-actions">简单镜头说明 <button class="btn btn-sm btn-secondary" :disabled="shot._busy || !shot.description.trim()" @click="expandShot(shot)">{{ shot._busy ? 'AI 扩写中…' : 'AI 扩写镜头' }}</button></span><textarea v-model="shot.description" class="textarea" rows="2" placeholder="例如：女主拔剑挡在男主身前" /></label>
      <div class="shot-grid two"><label>对白<textarea v-model="shot.dialogue" class="textarea" rows="2" /></label><label>情绪<textarea v-model="shot.emotion" class="textarea" rows="2" /></label></div>
      <details open><summary>五段导演提示词</summary>
        <div class="prompt-grid">
          <label><span>1 主体与五官</span><textarea v-model="shot.prompt_subject" class="textarea" rows="2" /></label>
          <label><span>2 姿态与动作</span><textarea v-model="shot.prompt_action" class="textarea" rows="2" /></label>
          <label><span>3 摄影机与构图</span><textarea v-model="shot.prompt_camera" class="textarea" rows="2" /></label>
          <label><span>4 光线与氛围</span><textarea v-model="shot.prompt_lighting" class="textarea" rows="2" /></label>
          <label><span>5 视觉风格与质感</span><textarea v-model="shot.prompt_style" class="textarea" rows="2" /></label>
        </div>
        <div class="workbench">
          <div class="prompt-preview">{{ assembledPrompt(shot) || '填写五段内容后可优化、翻译并应用风格' }}</div>
          <div class="tool-row">
            <select v-model="shot._preset" class="input preset-select"><option value="">选择风格预设</option><option v-for="p in (shot._presets || presets)" :key="p.id" :value="p.id">{{ p.name }} · {{ p._match_reason || p.reason }}</option></select>
            <button class="btn btn-sm btn-secondary" :disabled="!shot._preset" @click="applyStyle(shot)">应用风格</button>
            <button class="btn btn-sm btn-secondary" :disabled="shot._busy || !shot.id" @click="buildFivePartPrompt(shot)">构建五段</button>
            <button class="btn btn-sm btn-secondary" :disabled="shot._busy || !assembledPrompt(shot)" @click="runPromptAction(shot, 'optimize')">AI 优化</button>
            <button class="btn btn-sm btn-secondary" :disabled="shot._busy || !assembledPrompt(shot)" @click="runPromptAction(shot, 'translate')">翻译英文</button>
            <button class="btn btn-sm btn-ghost" :disabled="!shot.id" @click="loadHistory(shot)">历史版本</button>
          </div>
          <div v-if="shot._negative" class="negative">负向提示词：{{ shot._negative }}</div>
          <div v-if="shot._history?.length" class="history"><button v-for="v in shot._history" :key="v.id" class="history-item" :disabled="shot._busy" @click="rollbackVersion(shot, v)">{{ actionLabel(v.action) }} · {{ new Date(v.created_at).toLocaleString() }}</button></div>
        </div>
      </details>
    </div>
  </section>
</template>
<script setup>
import { ref, watch } from 'vue'
import { api } from '../api'
import { useToastStore } from '../stores/toast'
const props = defineProps({ projectId: { type: [String, Number], required: true }, sceneId: { type: [String, Number], required: true }, genre: { type: String, default: '' }, tone: { type: String, default: '' }, sceneTitle: { type: String, default: '' }, sceneContent: { type: String, default: '' } })
const toast = useToastStore(); const shots = ref([]); const presets = ref([]); const loading = ref(false); const saving = ref(false); let key = 0
const acts = [{ value: 'setup', label: '建置' }, { value: 'rising', label: '发展' }, { value: 'midpoint', label: '转折' }, { value: 'falling', label: '回落' }, { value: 'resolution', label: '收束' }]
const actLabel = value => acts.find(a => a.value === value)?.label || '建置'
const blank = (actType = 'setup') => ({ _key: `new-${++key}`, _preset: '', _history: [], act_type: actType, shot_type: '中景', camera_angle: '平视', camera_movement: '固定', duration: 2, description: '', dialogue: '', emotion: '', prompt_subject: '', prompt_action: '', prompt_camera: '', prompt_lighting: '', prompt_style: '' })
const hydrate = s => ({ ...s, _key: `shot-${s.id}`, _preset: '', _history: [], _presets: [] })
function assembledPrompt(s) { return [s.prompt_subject, s.prompt_action, s.prompt_camera, s.prompt_lighting, s.prompt_style].map(v => (v || '').trim()).filter(Boolean).join(', ') }
function setPrompt(shot, text) { const lines = String(text || '').split(/\r?\n/).map(v => v.trim()); if (lines.length === 5) { [shot.prompt_subject, shot.prompt_action, shot.prompt_camera, shot.prompt_lighting, shot.prompt_style] = lines; return } const parts = String(text || '').split(/\n|；|;/).map(v => v.trim()).filter(Boolean); if (parts.length >= 5) [shot.prompt_subject, shot.prompt_action, shot.prompt_camera, shot.prompt_lighting, shot.prompt_style] = [parts[0], parts[1], parts[2], parts[3], parts.slice(4).join(', ')]; else shot.prompt_style = text }
async function load() { loading.value = true; try { const [{ data }, presetRes] = await Promise.all([api.sceneShots(props.projectId, props.sceneId), api.presetRecommendations({ genre: props.genre, tone: props.tone, scene_type: props.sceneTitle, tags: props.sceneContent, limit: 5 })]); shots.value = (data.shots || []).map(hydrate); presets.value = (presetRes.data || []).map(r => ({ ...r.preset, _match_reason: (r.reasons || []).join('；'), _score: r.score })); await Promise.all(shots.value.filter(s => s.id).map(async shot => { const { data: recs } = await api.shotStyleRecommendations(props.projectId, shot.id, 3); shot._presets = (recs || []).map(r => ({ ...r.preset, _match_reason: (r.reasons || []).join('；'), _score: r.score })) })) } catch (e) { toast.error(e.response?.data?.error || '加载镜头失败') } finally { loading.value = false } }
function addShot() { shots.value.push(blank(shots.value.at(-1)?.act_type || 'setup')) }
function createFiveActSkeleton() { if (shots.value.length && !window.confirm('创建五幕骨架会替换当前尚未保存的镜头，是否继续？')) return; shots.value = acts.map((act, index) => ({ ...blank(act.value), shot_type: index === 0 ? '全景' : index === 2 ? '特写' : '中景', description: `${act.label}：`, duration: 2 })) }
function removeShot(index) { shots.value.splice(index, 1) }
async function saveAll() { saving.value = true; try { const payload = shots.value.map(({ _key, _preset, _presets, _history, _negative, _busy, scene_id, created_at, updated_at, order, ...s }) => s); const { data } = await api.replaceSceneShots(props.projectId, props.sceneId, payload); shots.value = data.shots.map(hydrate); toast.success('五幕镜头已保存，并应用到场景生成提示词') } catch (e) { toast.error(e.response?.data?.error || '保存镜头失败') } finally { saving.value = false } }
async function applyStyle(shot) { try { if (!shot.id) { toast.error('请先保存镜头'); return } const preview = await api.applyShotStylePreset(props.projectId, shot.id, shot._preset, true); const p = preview.data.preset; if (!window.confirm(`将追加：${p.prompt_tail || '无'}\n负向词：${p.negative_tail || '无'}\n原因：${p.reason || preview.data.metadata?.preset_name || ''}\n是否应用？`)) return; const { data } = await api.applyShotStylePreset(props.projectId, shot.id, shot._preset, false); shot.prompt_style = data.prompt_style; shot._negative = data.preset?.negative_tail || ''; await loadHistory(shot); toast.success(data.changed ? '已应用风格预设并记录来源' : '该预设已应用，无需重复追加') } catch (e) { toast.error(e.response?.data?.error || '应用预设失败') } }
async function buildFivePartPrompt(shot) { shot._busy = true; try { const template = '{{subject}}\n{{action}}\n{{camera}}\n{{lighting}}\n{{style}}'; const { data } = await api.buildPrompt({ project_id: Number(props.projectId), entity_type: 'shot', entity_id: shot.id, template, params: { subject: shot.prompt_subject || '', action: shot.prompt_action || '', camera: shot.prompt_camera || '', lighting: shot.prompt_lighting || '', style: shot.prompt_style || '' } }); setPrompt(shot, data.prompt); await loadHistory(shot); toast.success('已按五段结构构建提示词') } catch (e) { toast.error(e.response?.data?.error || '构建提示词失败') } finally { shot._busy = false } }
async function expandShot(shot) { shot._busy = true; try { if (shot.id) { const { data } = await api.expandShotDirectorPrompt(props.projectId, shot.id, { description: shot.description, asset_context: `${props.sceneTitle}；${props.sceneContent}`, start_state: '', end_state: '', }); [shot.prompt_subject, shot.prompt_action, shot.prompt_camera, shot.prompt_lighting, shot.prompt_style] = [data.subject, data.action, data.camera, data.lighting, data.style]; } else { const { data } = await api.optimizePrompt({ project_id: Number(props.projectId), entity_type: 'shot', entity_id: 0, prompt: `请依据简单说明扩写单个分镜：${shot.description}`, context: `叙事幕：${actLabel(shot.act_type)}；必须严格输出五行，依次为主体与五官；姿态与动作；摄影机与构图；光线与氛围；视觉风格与质感。每行只写对应内容，不要编号、标题或解释。` }); const parts = data.prompt.split(/\n|；|;/).map(v => v.replace(/^\s*\d+[.、:)：-]?\s*/, '').trim()).filter(Boolean); if (parts.length < 5) throw new Error('AI 未按五段结构返回，请重试'); [shot.prompt_subject, shot.prompt_action, shot.prompt_camera, shot.prompt_lighting, shot.prompt_style] = [parts[0], parts[1], parts[2], parts[3], parts.slice(4).join(', ')]; } toast.success('镜头五段提示词已扩写') } catch (e) { toast.error(e.response?.data?.error || e.message || '镜头扩写失败') } finally { shot._busy = false } }
async function runPromptAction(shot, action) { if (!shot.id) { toast.error('请先保存镜头'); return } shot._busy = true; try { const payload = { project_id: Number(props.projectId), entity_type: 'shot', entity_id: shot.id, prompt: assembledPrompt(shot), context: `${shot.description}；情绪：${shot.emotion}` }; const { data } = action === 'optimize' ? await api.optimizePrompt(payload) : await api.translatePrompt({ ...payload, target_lang: 'en' }); setPrompt(shot, data.prompt); await loadHistory(shot); toast.success(action === 'optimize' ? '提示词已优化' : '提示词已翻译') } catch (e) { toast.error(e.response?.data?.error || '提示词处理失败') } finally { shot._busy = false } }
async function loadHistory(shot) { try { const { data } = await api.promptHistory({ project_id: props.projectId, entity_type: 'shot', entity_id: shot.id, limit: 10 }); shot._history = data || [] } catch (e) { toast.error(e.response?.data?.error || '加载历史失败') } }
async function rollbackVersion(shot, version) { shot._busy = true; try { const { data } = await api.rollbackPrompt({ project_id: Number(props.projectId), entity_type: 'shot', entity_id: shot.id, version_id: version.id }); setPrompt(shot, data.prompt); await loadHistory(shot); toast.success('已回滚到该历史版本，保存镜头后应用到场景') } catch (e) { toast.error(e.response?.data?.error || '回滚失败') } finally { shot._busy = false } }
const actionLabel = a => ({ build: '构建', optimize: '优化', translate: '翻译', manual: '回滚' }[a] || a)
watch(() => props.sceneId, load, { immediate: true })
</script>
<style scoped>
.director{grid-column:1/-1}.shot-card{padding:18px;margin-bottom:14px}.shot-head{display:flex;justify-content:space-between;align-items:center;margin-bottom:14px}.shot-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px}.shot-grid.five{grid-template-columns:repeat(5,minmax(0,1fr))}.shot-grid.two{grid-template-columns:1fr 1fr}.shot-card label{display:grid;gap:6px;color:var(--text-secondary);font-size:12px;margin-bottom:12px}summary{cursor:pointer;font-weight:600;margin:4px 0 12px}.prompt-grid{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:10px}.empty{padding:24px;color:var(--text-secondary)}.workbench{border-top:1px solid var(--border);padding-top:12px}.prompt-preview{padding:12px;border-radius:8px;background:var(--surface-secondary);font-size:13px;line-height:1.6}.tool-row{display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin-top:10px}.preset-select{min-width:260px;flex:1}.negative{margin-top:8px;color:var(--text-secondary);font-size:12px}.history{display:flex;gap:6px;flex-wrap:wrap;margin-top:10px}.history-item{border:1px solid var(--border);background:transparent;color:var(--text-secondary);border-radius:6px;padding:5px 8px;cursor:pointer;font-size:11px}@media(max-width:900px){.shot-grid,.prompt-grid{grid-template-columns:1fr 1fr}}@media(max-width:560px){.shot-grid,.shot-grid.two,.prompt-grid{grid-template-columns:1fr}}
</style>
