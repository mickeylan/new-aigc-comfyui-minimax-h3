<template>
  <div class="page fade-up">
    <div class="head-links"><router-link :to="`/projects/${id}`" class="back">← 返回项目</router-link></div>
    <div class="page-head"><div><h1>长篇小说导入</h1><p class="sub">上传、确认章节，再逐章分析并归并故事圣经。</p></div><div class="head-actions"><button class="btn btn-secondary" :disabled="busy || !chapters.length" @click="analyzeAll">分析待处理章节</button><button class="btn btn-secondary" :disabled="busy || !allReady" @click="generateArcs">生成剧情单元</button><router-link :to="`/projects/${id}/story-bible`" class="btn">故事圣经 →</router-link></div></div>
    <section class="card upload-card">
      <input ref="picker" type="file" accept=".txt,.md,.markdown,text/plain,text/markdown" hidden @change="upload" />
      <button class="btn" :disabled="busy" @click="picker.click()">{{ busy ? '处理中…' : '上传或替换小说' }}</button>
      <span v-if="status">{{ status.word_count || 0 }} 字 · {{ status.chapter_count || 0 }} 章 · {{ status.import_status }}</span>
      <span v-if="error" class="error">{{ error }}</span>
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
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
const route = useRoute(); const id = route.params.id
const picker = ref(null), busy = ref(false), error = ref(''), status = ref(null), chapters = ref([]), current = ref(null), title = ref('')
async function load() { try { status.value = (await api.novelImportStatus(id)).data; chapters.value = (await api.novelChapters(id)).data.chapters || [] } catch (e) { error.value = e.response?.data?.error || e.message } }
async function upload(e) { const file=e.target.files?.[0]; if(!file)return; busy.value=true; error.value=''; try { const {data}=await api.uploadNovel(id,file); chapters.value=data.chapters||[]; await load() } catch(e2){error.value=e2.response?.data?.error||e2.message} finally {busy.value=false;e.target.value=''} }
async function open(ch) { try { current.value=(await api.novelChapter(id,ch.id)).data; title.value=current.value.title } catch(e){error.value=e.response?.data?.error||e.message} }
async function saveTitle(){ if(!current.value||!title.value.trim())return; try { current.value=(await api.updateNovelChapter(id,current.value.id,{title:title.value.trim()})).data; await load() } catch(e){error.value=e.response?.data?.error||e.message} }
const allReady = computed(() => chapters.value.length > 0 && chapters.value.every(ch => ch.analysis_status === 'ready'))
async function analyzeAll(){busy.value=true;error.value='';try{await api.analyzeNovelChapters(id);await load()}catch(e){error.value=e.response?.data?.error||e.message}finally{busy.value=false}}
async function generateArcs(){busy.value=true;error.value='';try{await api.generateNovelArcs(id,8)}catch(e){error.value=e.response?.data?.error||e.message}finally{busy.value=false}}
function prettyAnalysis(raw){try{return JSON.stringify(JSON.parse(raw),null,2)}catch{return raw}}
onMounted(load)
</script>
<style scoped>
.page-head{display:flex;justify-content:space-between;gap:16px;margin-bottom:18px}.head-actions{display:flex;gap:10px;align-items:center;flex-wrap:wrap}.upload-card{display:flex;align-items:center;gap:16px;padding:18px;margin-bottom:18px}.error{color:var(--red)}.workspace{display:grid;grid-template-columns:300px 1fr;gap:18px}.chapter-list,.reader{padding:18px;max-height:72vh;overflow:auto}.chapter-list h2{margin-top:0}.chapter{display:flex;width:100%;justify-content:space-between;gap:8px;padding:10px;border:0;border-radius:8px;background:transparent;color:var(--text);text-align:left;cursor:pointer}.chapter:hover,.chapter.active{background:var(--accent-soft);color:var(--accent)}.chapter small{white-space:nowrap;color:var(--text-tertiary)}.reader-head{display:flex;gap:10px}.reader-head .input{flex:1}.reader pre{white-space:pre-wrap;line-height:1.8;font-family:inherit}.empty{padding:60px 20px;text-align:center;color:var(--text-tertiary)}@media(max-width:780px){.workspace{grid-template-columns:1fr}.chapter-list{max-height:280px}}
</style>
