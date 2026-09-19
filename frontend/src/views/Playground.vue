<template>
  <div class="page fade-up">
    <div class="section-head"><div><span class="overline">PLAYGROUND</span><h1>生成试验场</h1><p class="sub">不创建项目，直接使用现有模板批量试验；满意结果可显式加入素材库。</p></div><button class="btn btn-ghost" @click="load">刷新</button></div>
    <section class="card form-card">
      <div class="field-row"><label>模式<select v-model="form.mode" class="input"><option v-for="mode in modes" :key="mode" :value="mode">{{ mode }}</option></select></label><label>模型模板<select v-model="form.template" class="input"><option v-for="item in templates" :key="item.template_code" :value="item.template_code">{{ item.name }}</option></select></label><label>批量<input v-model.number="form.batch_count" type="number" min="1" max="4" class="input" /></label></div>
      <label>提示词<textarea v-model="form.prompt" class="textarea" rows="5" /></label>
      <label>参数 JSON<textarea v-model="paramsText" class="textarea mono" rows="4" placeholder='{"width":1024,"height":1024}' /></label>
      <label>参考图片（可多选）<input type="file" accept="image/*" multiple @change="uploadReferences($event, 'ref_images')" /></label>
      <label>首帧图片<input type="file" accept="image/*" @change="uploadReferences($event, 'first_frame')" /></label>
      <label>尾帧图片<input type="file" accept="image/*" @change="uploadReferences($event, 'last_frame')" /></label>
      <p class="sub" v-if="uploadedCount">已上传 {{ uploadedCount }} 个输入文件</p>
      <button class="btn" :disabled="busy || !form.template" @click="generate">{{ busy ? '提交中…' : '批量生成' }}</button>
    </section>
    <section class="section"><div class="section-head"><h2>历史结果</h2><span class="sub">勾选两项可比较</span></div><div class="catalog-grid">
      <article v-for="run in runs" :key="run.id" class="card run-card">
        <label><input type="checkbox" :value="run.id" v-model="compareIds" :disabled="!compareIds.includes(run.id) && compareIds.length >= 2" /> 比较</label>
        <h3>{{ run.template }}</h3><p>{{ run.prompt }}</p><span class="badge badge-gray">{{ run.status }}</span>
        <pre class="mono result">{{ run.result }}</pre>
        <div class="section-actions"><router-link :to="`/tasks/${run.task_id}`" class="btn btn-sm btn-ghost">任务详情</router-link><button class="btn btn-sm btn-ghost" @click="star(run)">{{ run.starred ? '★' : '☆' }}</button><button class="btn btn-sm btn-secondary" :disabled="run.status !== 'success'" @click="promote(run)">加入素材库</button></div>
      </article>
    </div></section>
    <section v-if="compared.length === 2" class="card compare"><div v-for="run in compared" :key="run.id"><h3>{{ run.template }}</h3><pre>{{ run.prompt }}</pre><pre class="mono">{{ run.result }}</pre></div></section>
  </div>
</template>
<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api } from '../api'
import { useToastStore } from '../stores/toast'
const toast = useToastStore(); const catalog = ref([]); const runs = ref([]); const busy = ref(false); const compareIds = ref([]); const paramsText = ref('{}'); const files = ref({})
const form = reactive({ mode: 'text-to-image', template: '', prompt: '', batch_count: 1 })
const modes = computed(() => [...new Set(catalog.value.map(v => v.mode))])
const templates = computed(() => catalog.value.filter(v => v.mode === form.mode))
const compared = computed(() => runs.value.filter(v => compareIds.value.includes(v.id)))
const uploadedCount = computed(() => Object.values(files.value).reduce((n, rows) => n + rows.length, 0))
watch(() => form.mode, () => { form.template = templates.value[0]?.template_code || '' })
async function load(){ try { const [cat,res]=await Promise.all([api.modelCatalog(),api.playgroundRuns()]); catalog.value=cat.data?.items||[]; runs.value=res.data?.items||[]; if(!form.template){form.mode=modes.value[0]||'';form.template=templates.value[0]?.template_code||''} } catch(e){toast.error(e.response?.data?.error||'加载失败')} }
async function uploadReferences(event, slot){ const selected=[...(event.target.files||[])]; if(!selected.length)return; busy.value=true; try{const taskId=`playground-${Date.now()}`; const rows=[]; for(const file of selected){const {data}=await api.upload(file,'image',taskId);rows.push({task_id:data.task_id,name:data.name})} files.value={...files.value,[slot]:rows};toast.success('输入图片已上传')}catch(e){toast.error(e.response?.data?.error||'上传失败')}finally{busy.value=false} }
async function generate(){ busy.value=true; try{const params=JSON.parse(paramsText.value||'{}');await api.createPlaygroundRun({...form,params,files:files.value});toast.success('试验任务已提交');files.value={};await load()}catch(e){toast.error(e.response?.data?.error||e.message||'提交失败')}finally{busy.value=false} }
async function star(run){await api.starPlaygroundRun(run.id,!run.starred);run.starred=!run.starred}
async function promote(run){try{await api.promotePlaygroundRun(run.id,0);toast.success('结果已加入素材库')}catch(e){toast.error(e.response?.data?.error||'提升失败')}}
onMounted(load)
</script>
<style scoped>.form-card{display:grid;gap:14px;padding:18px}.field-row{display:grid;grid-template-columns:1fr 1fr 120px;gap:12px}.field-row label,.form-card>label{display:grid;gap:6px}.catalog-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(280px,1fr));gap:14px}.run-card{padding:16px}.result{max-height:130px;overflow:auto}.compare{display:grid;grid-template-columns:1fr 1fr;gap:16px;padding:18px}@media(max-width:700px){.field-row,.compare{grid-template-columns:1fr}}</style>
