<template>
  <div class="page fade-up">
    <div class="head-links"><router-link :to="`/projects/${id}`" class="back">← 返回项目</router-link></div>
    <div class="page-head"><div><h1>长篇小说导入</h1><p class="sub">按章节窗口滚动分析和生产，不必等待整本小说分析完成。</p></div><div class="head-actions"><router-link :to="`/projects/${id}/story-bible`" class="btn">故事圣经 →</router-link></div></div>
    <section class="card upload-card">
      <input ref="picker" type="file" accept=".txt,.md,.markdown,text/plain,text/markdown" hidden @change="upload" />
      <button class="btn" :disabled="busy" @click="picker.click()">{{ busy ? '处理中…' : '上传或替换小说' }}</button>
      <span v-if="status">{{ status.word_count || 0 }} 字 · {{ status.chapter_count || 0 }} 章 · {{ status.import_status }}</span>
      <span v-if="error" class="error">{{ error }}</span>
    </section>
    <section v-if="chapters.length" class="card window-panel">
      <div class="window-head"><div><h2>滚动分析窗口</h2><p class="sub">默认分析10章，并向后预读5章判断故事弧是否跨界。只有目标范围会调用AI。</p></div><div v-if="!currentWindow" class="custom-window"><input v-model.number="windowForm.start" type="number" min="1" class="input"><span>至</span><input v-model.number="windowForm.end" type="number" min="1" class="input"><label>预读至<input v-model.number="windowForm.contextEnd" type="number" min="1" class="input"></label><button class="btn btn-secondary" :disabled="busy" @click="loadSuggestion">恢复建议</button><button class="btn" :disabled="busy" @click="createCustomWindow">创建窗口</button></div></div>
      <div v-if="currentWindow" class="window-current">
        <div><strong>窗口{{currentWindow.window_no}}：第{{currentWindow.chapter_start}}–{{currentWindow.chapter_end}}章</strong><span>预读上下文 {{currentWindow.context_start}}–{{currentWindow.context_end}}章 · {{windowStatus(currentWindow.status)}}</span></div>
        <div v-if="windowProgress" class="window-progress"><div class="progress-text"><span>已完成 {{windowProgress.ready_chapters}} / {{windowProgress.total_chapters}}，失败 {{windowProgress.failed_chapters}}</span><strong>{{progressPercent}}%</strong></div><div class="progress-track"><span :style="{width:progressPercent+'%'}"></span></div><small v-if="analyzing">正在分析第 {{Math.min(windowProgress.ready_chapters+windowProgress.failed_chapters+1,windowProgress.total_chapters)}} 章内容，请勿关闭页面…</small></div>
        <div v-if="windowProgress?.boundary" class="boundary-note" :class="{warning:windowProgress.boundary.needs_extension}"><span>{{windowProgress.boundary.reason}}</span><button v-if="windowProgress.boundary.needs_extension" class="btn btn-xs" :disabled="busy" @click="extendWindow">扩展至第{{windowProgress.boundary.suggested_end}}章</button></div><div v-if="failedChapters.length" class="failure-list"><strong>失败章节</strong><div v-for="ch in failedChapters" :key="ch.id"><span>第{{ch.order}}章 {{ch.title}}：{{ch.error}}</span><button class="btn btn-xs" :disabled="busy" @click="retryChapter(ch)">重试</button></div></div><div class="head-actions"><button class="btn btn-secondary" :disabled="busy||currentWindow.status==='committed'" @click="analyzeWindow">{{failedChapters.length?'重试失败章节':'分析当前窗口'}}</button><button class="btn btn-secondary" :disabled="busy||currentWindow.status!=='ready'" @click="generateWindowArcs">生成本窗口故事弧</button><button v-if="bible?.status==='approved'" class="btn btn-secondary" :disabled="busy||currentWindow.status!=='ready'" @click="proposeBibleChange">生成Story Bible增量</button><button class="btn btn-secondary" :disabled="busy||currentWindow.status!=='ready'" @click="approveWindow">审核窗口</button><router-link v-if="currentWindow.status==='approved'&&!currentWindow.planning_batch_id" :to="`/projects/${id}/adaptation`" class="btn btn-secondary">创建本窗口生产批次</router-link><button class="btn" :disabled="busy||currentWindow.status!=='approved'||!currentWindow.planning_batch_id" :title="currentWindow.planning_batch_id?'':'需先创建并审核对应生产批次及状态快照'" @click="commitAndAdvance">完成并推进下一窗口</button></div>
      </div>
      <div v-if="windows.length>1" class="window-history"><span v-for="w in windows" :key="w.id" class="tag">{{w.window_no}} · {{w.chapter_start}}–{{w.chapter_end}} · {{windowStatus(w.status)}}</span></div>
    </section>
    <div class="workspace">
      <aside class="card chapter-list">
        <h2>章节（{{ chapters.length }}）</h2>
        <button v-for="ch in chapters" :key="ch.id" class="chapter" :class="{ active: current?.id === ch.id }" @click="open(ch)">
          <b>{{ ch.order }}. {{ ch.title }}</b><small>{{ ch.word_count }} 字 · {{ ch.analysis_status || 'pending' }}</small>
        </button>
      </aside>
      <section class="card reader">
        <template v-if="current">
          <div class="reader-head"><input v-model="title" class="input" /><button class="btn btn-sm" @click="saveTitle">保存标题</button></div>
          <div v-if="current.analysis_json" class="analysis"><h3>章节分析</h3><pre>{{ prettyAnalysis(current.analysis_json) }}</pre></div>
          <h3>原文</h3><pre>{{ current.content }}</pre>
        </template>
        <div v-else class="empty">上传小说并选择章节后在这里预览原文。</div>
      </section>
    </div>
  </div>
</template>
<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
const route = useRoute(); const id = route.params.id
const picker = ref(null), busy = ref(false), analyzing = ref(false), error = ref(''), status = ref(null), chapters = ref([]), current = ref(null), title = ref(''), windows = ref([]), windowProgress = ref(null), bible = ref(null)
let progressTimer = null
const currentWindow = computed(() => [...windows.value].reverse().find(w => w.status !== 'committed' && w.status !== 'stale') || null)
const failedChapters=computed(()=>(windowProgress.value?.chapters||[]).filter(ch=>ch.status==='failed'))
const windowForm=ref({start:1,end:10,contextEnd:15})
const progressPercent = computed(() => !windowProgress.value?.total_chapters ? 0 : Math.round(100*(windowProgress.value.ready_chapters+windowProgress.value.failed_chapters)/windowProgress.value.total_chapters))
async function load() { try { const [s,c,w]=await Promise.all([api.novelImportStatus(id),api.novelChapters(id),api.analysisWindows(id)]);status.value=s.data;chapters.value=c.data.chapters||[];windows.value=w.data.windows||[];windowProgress.value=currentWindow.value?(await api.analysisWindowProgress(id,currentWindow.value.id)).data:null;try{bible.value=(await api.storyBible(id)).data}catch{bible.value=null} } catch (e) { error.value = e.response?.data?.error || e.message } }
async function upload(e) { const file=e.target.files?.[0]; if(!file)return; busy.value=true; error.value=''; try { const {data}=await api.uploadNovel(id,file); chapters.value=data.chapters||[]; await load() } catch(e2){error.value=e2.response?.data?.error||e2.message} finally {busy.value=false;e.target.value=''} }
async function open(ch) { try { current.value=(await api.novelChapter(id,ch.id)).data; title.value=current.value.title } catch(e){error.value=e.response?.data?.error||e.message} }
async function saveTitle(){ if(!current.value||!title.value.trim())return; try { current.value=(await api.updateNovelChapter(id,current.value.id,{title:title.value.trim()})).data; await load() } catch(e){error.value=e.response?.data?.error||e.message} }
const windowStatus=value=>({pending:'待分析',analysing:'分析中',ready:'待审核',approved:'已审核',committed:'已完成',stale:'已过期'}[value]||value)
async function run(action){busy.value=true;error.value='';try{await action();await load()}catch(e){error.value=e.response?.data?.error||e.message}finally{busy.value=false}}
async function loadSuggestion(){try{const{data}=await api.analysisWindowSuggestion(id,10);windowForm.value={start:data.suggested_start,end:data.suggested_end,contextEnd:data.suggested_context_end}}catch(e){error.value=e.response?.data?.error||e.message}}
const createCustomWindow=()=>run(()=>api.createAnalysisWindow(id,{chapter_start:windowForm.value.start,chapter_end:windowForm.value.end,context_start:Math.max(1,windowForm.value.start-2),context_end:windowForm.value.contextEnd,window_size:windowForm.value.end-windowForm.value.start+1}))
async function analyzeWindow(){busy.value=true;analyzing.value=true;error.value='';const wid=currentWindow.value.id;try{await api.analyzeAnalysisWindow(id,wid);for(;;){await new Promise(resolve=>progressTimer=setTimeout(resolve,1200));windowProgress.value=(await api.analysisWindowProgress(id,wid)).data;if(windowProgress.value.status!=='analysing')break}await load()}catch(e){error.value=e.response?.data?.error||e.message}finally{clearTimeout(progressTimer);progressTimer=null;analyzing.value=false;busy.value=false}}
const generateWindowArcs=()=>run(()=>api.generateNovelArcs(id,8,currentWindow.value.chapter_start,currentWindow.value.chapter_end))
const proposeBibleChange=()=>run(()=>api.proposeStoryBibleChange(id,currentWindow.value.id))
const extendWindow=()=>run(()=>api.extendAnalysisWindow(id,currentWindow.value.id,windowProgress.value.boundary.suggested_end))
const retryChapter=ch=>run(()=>api.retryNovelChapter(id,ch.id))
const approveWindow=()=>run(()=>api.approveAnalysisWindow(id,currentWindow.value.id))
async function commitAndAdvance(){await run(async()=>{const wid=currentWindow.value.id;await api.commitAnalysisWindow(id,wid);try{await api.advanceAnalysisWindow(id,wid,10)}catch(e){if(!String(e.response?.data?.error||'').includes('no more chapters'))throw e}})}
function prettyAnalysis(raw){try{return JSON.stringify(JSON.parse(raw),null,2)}catch{return raw}}
onMounted(async()=>{await load();if(!currentWindow.value)await loadSuggestion()});onBeforeUnmount(()=>clearTimeout(progressTimer))
</script>
<style scoped>
.page-head{display:flex;justify-content:space-between;gap:16px;margin-bottom:18px}.window-panel{padding:18px;margin-bottom:18px}.window-head,.window-current>div:first-child,.progress-text{display:flex;justify-content:space-between;gap:12px;align-items:center}.window-current{display:grid;gap:12px}.window-current>div:first-child{align-items:flex-start}.window-current>div:first-child span{color:var(--text-tertiary)}.window-history{display:flex;gap:8px;flex-wrap:wrap;margin-top:14px}.window-progress{display:grid;gap:7px}.progress-track{height:9px;background:var(--border);border-radius:99px;overflow:hidden}.progress-track span{display:block;height:100%;background:linear-gradient(90deg,var(--accent),#52b8e8);transition:width .35s ease}.window-progress small{color:var(--text-tertiary)}.custom-window{display:flex;gap:7px;align-items:center;flex-wrap:wrap}.custom-window>.input{width:74px}.custom-window label{display:flex;gap:6px;align-items:center}.custom-window label .input{width:74px}.boundary-note{display:flex;justify-content:space-between;gap:10px;padding:9px;border:1px solid var(--border);border-radius:8px}.boundary-note.warning{border-color:#f59e0b88;color:#f59e0b}.failure-list{padding:10px;border:1px solid #ef444455;border-radius:8px;color:var(--red);display:grid;gap:6px}.failure-list>div{display:flex;justify-content:space-between;gap:10px;align-items:center}.head-actions{display:flex;gap:10px;align-items:center;flex-wrap:wrap}.upload-card{display:flex;align-items:center;gap:16px;padding:18px;margin-bottom:18px}.error{color:var(--red)}.workspace{display:grid;grid-template-columns:300px 1fr;gap:18px}.chapter-list,.reader{padding:18px;max-height:72vh;overflow:auto}.chapter-list h2{margin-top:0}.chapter{display:flex;width:100%;justify-content:space-between;gap:8px;padding:10px;border:0;border-radius:8px;background:transparent;color:var(--text);text-align:left;cursor:pointer}.chapter:hover,.chapter.active{background:var(--accent-soft);color:var(--accent)}.chapter small{white-space:nowrap;color:var(--text-tertiary)}.reader-head{display:flex;gap:10px}.reader-head .input{flex:1}.reader pre{white-space:pre-wrap;line-height:1.8;font-family:inherit}.empty{padding:60px 20px;text-align:center;color:var(--text-tertiary)}@media(max-width:780px){.workspace{grid-template-columns:1fr}.chapter-list{max-height:280px}}
</style>
