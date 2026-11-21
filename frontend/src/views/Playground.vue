<template>
  <div class="page fade-up">
    <div class="section-head"><div><span class="overline">PLAYGROUND</span><h1>生成试验场</h1><p class="sub">不创建项目，直接使用现有模板批量试验；满意结果可显式加入素材库。</p></div><button class="btn btn-ghost" @click="load">刷新</button></div>
    <section class="card form-card">
      <div class="field-row"><label>模式<select v-model="form.mode" class="input"><option v-for="mode in modes" :key="mode" :value="mode">{{ mode }}</option></select></label><label>模型模板<select v-model="form.template" class="input"><option v-for="item in templates" :key="item.template_code" :value="item.template_code">{{ item.name }}</option></select></label><label>批量<input v-model.number="form.batch_count" type="number" min="1" max="4" class="input" /></label></div>
      <label>提示词<textarea v-model="form.prompt" class="textarea" rows="5" /></label>
      <label>参数 JSON<textarea v-model="paramsText" class="textarea mono" rows="4" placeholder='{"width":1024,"height":1024}' /></label>
      <div class="ref-block">
        <label>参考图片（可多选，顺序即提示词里的 &lt;image1&gt;…&lt;imageN&gt;）<input type="file" accept="image/*" multiple @change="uploadReferences($event, 'ref_images')" /></label>
        <p class="sub" v-if="refSlots.length">已上传 {{ refSlots.length }} 张（最多 {{ maxReferences }} 张）：第 1 张是 &lt;image1&gt;，第 2 张是 &lt;image2&gt;，以此类推（MiniMax H3 编辑模板写 &lt;Picture 1&gt;、&lt;Picture 2&gt;，顺序相同）；可上移、下移调整顺序，提示词里用这些序号指名图片。</p>
        <ol class="ref-list">
          <li v-for="(slot, i) in refSlots" :key="slot.key" class="ref-row">
            <span class="ref-index">{{ slot.label }}</span>
            <img class="ref-thumb" :src="slot.url" :alt="slot.name" />
            <span class="ref-name">{{ slot.name }}</span>
            <span class="ref-actions">
              <button class="btn btn-xs btn-ghost" :disabled="busy || i === 0" @click="moveRef(i, -1)">上移</button>
              <button class="btn btn-xs btn-ghost" :disabled="busy || i === refSlots.length - 1" @click="moveRef(i, 1)">下移</button>
              <button class="btn btn-xs btn-danger" :disabled="busy" @click="removeRef(i)">删除</button>
            </span>
          </li>
        </ol>
      </div>
      <label>首帧图片<input type="file" accept="image/*" @change="uploadReferences($event, 'first_frame')" /></label>
      <label>尾帧图片<input type="file" accept="image/*" @change="uploadReferences($event, 'last_frame')" /></label>
      <p class="sub" v-if="uploadedCount">已上传 {{ uploadedCount }} 个输入文件</p>
      <button class="btn" :disabled="busy || !form.template" @click="generate">{{ busy ? '提交中…' : '批量生成' }}</button>
    </section>
    <section class="section"><div class="section-head"><h2>历史结果</h2><span class="sub">勾选两项可比较</span></div><div class="catalog-grid">
      <article v-for="run in runs" :key="run.id" class="card run-card">
        <label><input type="checkbox" :value="run.id" v-model="compareIds" :disabled="!compareIds.includes(run.id) && compareIds.length >= 2" /> 比较</label>
        <h3>{{ run.template }}</h3><p>{{ run.prompt }}</p><span class="badge badge-gray">{{ run.status }}</span>
        <div v-if="resultsOf(run).length" class="run-results">
          <figure v-for="(file, index) in resultsOf(run)" :key="index" class="run-result">
            <a v-if="runResultKind(file) === 'image'" :href="runResultUrl(run, file)" target="_blank" rel="noopener"><img :src="runResultUrl(run, file)" :alt="file.filename" loading="lazy" /></a>
            <video v-else-if="runResultKind(file) === 'video'" :src="runResultUrl(run, file)" controls preload="metadata"></video>
            <audio v-else-if="runResultKind(file) === 'audio'" :src="runResultUrl(run, file)" controls preload="metadata"></audio>
            <a v-else :href="runResultUrl(run, file)" target="_blank" rel="noopener" class="badge badge-gray">{{ file.filename }}</a>
            <figcaption><a :href="runResultUrl(run, file)" target="_blank" rel="noopener">{{ file.filename }}</a></figcaption>
          </figure>
        </div>
        <details v-else-if="run.result && run.result !== '[]'" class="raw-result"><summary>原始结果</summary><pre class="mono result">{{ run.result }}</pre></details>
        <div class="section-actions"><router-link :to="`/tasks/${run.task_id}`" class="btn btn-sm btn-ghost">任务详情</router-link><button class="btn btn-sm btn-ghost" @click="star(run)">{{ run.starred ? '★' : '☆' }}</button><button class="btn btn-sm btn-secondary" :disabled="run.status !== 'success'" @click="promote(run)">加入素材库</button></div>
      </article>
    </div></section>
    <section v-if="compared.length === 2" class="card compare"><div v-for="run in compared" :key="run.id"><h3>{{ run.template }}</h3><pre>{{ run.prompt }}</pre><div v-if="resultsOf(run).length" class="run-results"><figure v-for="(file, index) in resultsOf(run)" :key="index" class="run-result"><a v-if="runResultKind(file) === 'image'" :href="runResultUrl(run, file)" target="_blank" rel="noopener"><img :src="runResultUrl(run, file)" :alt="file.filename" loading="lazy" /></a><video v-else-if="runResultKind(file) === 'video'" :src="runResultUrl(run, file)" controls preload="metadata"></video><audio v-else-if="runResultKind(file) === 'audio'" :src="runResultUrl(run, file)" controls preload="metadata"></audio><a v-else :href="runResultUrl(run, file)" target="_blank" rel="noopener" class="badge badge-gray">{{ file.filename }}</a></figure></div><pre v-else class="mono">{{ run.result }}</pre></div></section>
  </div>
</template>
<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api } from '../api'
import { useToastStore } from '../stores/toast'
import { MAX_PLAYGROUND_REFERENCES, appendReferences, moveReference, referenceSlots, removeReference } from '../utils/playgroundReferences'
import { parseRunResults, runResultKind, runResultUrl } from '../utils/playgroundResults'
const toast = useToastStore(); const catalog = ref([]); const runs = ref([]); const busy = ref(false); const compareIds = ref([]); const paramsText = ref('{}'); const files = ref({})
const form = reactive({ mode: 'text-to-image', template: '', prompt: '', batch_count: 1 })
const modes = computed(() => [...new Set(catalog.value.map(v => v.mode))])
const templates = computed(() => catalog.value.filter(v => v.mode === form.mode))
const compared = computed(() => runs.value.filter(v => compareIds.value.includes(v.id)))
const uploadedCount = computed(() => Object.values(files.value).reduce((n, rows) => n + rows.length, 0))
const maxReferences = computed(() => { const found = catalog.value.find(v => v.template_code === form.template); const limit = Number(found?.max_references); return Number.isFinite(limit) && limit > 0 ? limit : MAX_PLAYGROUND_REFERENCES })
const refSlots = computed(() => referenceSlots(files.value.ref_images))
// 结果文件必须拼成图片/视频地址才能看见；只把 result JSON 打出来是看不见图的。
const resultsOf = run => parseRunResults(run?.result)
watch(() => form.mode, () => { form.template = templates.value[0]?.template_code || '' })
async function load(){ try { const [cat,res]=await Promise.all([api.modelCatalog(),api.playgroundRuns()]); catalog.value=cat.data?.items||[]; runs.value=res.data?.items||[]; if(!form.template){form.mode=modes.value[0]||'';form.template=templates.value[0]?.template_code||''} } catch(e){toast.error(e.response?.data?.error||'加载失败')} }
async function uploadReferences(event, slot){ const selected=[...(event.target.files||[])]; event.target.value=''; if(!selected.length)return
  // 参考图是追加的：已上传的图片保留原顺序，只有首帧/尾帧是单张覆盖。
  const isRefs=slot==='ref_images'; const limit=isRefs?maxReferences.value:1
  const room=isRefs?Math.max(0,limit-(files.value.ref_images||[]).length):1
  if(!room){ toast.error(`参考图最多 ${limit} 张`); return }
  busy.value=true
  try{const taskId=`playground-${Date.now()}`; const rows=[]; for(const file of selected.slice(0,room)){const {data}=await api.upload(file,'image',taskId);rows.push({task_id:data.task_id,name:data.name})}
    if(isRefs){ const {rows:next,skipped}=appendReferences(files.value.ref_images,rows,limit); files.value={...files.value,ref_images:next}; if(skipped)toast.error(`参考图最多 ${limit} 张，已忽略 ${skipped} 张`); else toast.success('参考图已追加') }
    else { files.value={...files.value,[slot]:rows.slice(0,1)}; toast.success('输入图片已上传') }
  }catch(e){toast.error(e.response?.data?.error||'上传失败')}finally{busy.value=false} }
function moveRef(index,delta){ files.value={...files.value,ref_images:moveReference(files.value.ref_images,index,delta)} }
function removeRef(index){ files.value={...files.value,ref_images:removeReference(files.value.ref_images,index)} }
async function generate(){ busy.value=true; try{const params=JSON.parse(paramsText.value||'{}');await api.createPlaygroundRun({...form,params,files:files.value});toast.success('试验任务已提交');files.value={};await load()}catch(e){toast.error(e.response?.data?.error||e.message||'提交失败')}finally{busy.value=false} }
async function star(run){await api.starPlaygroundRun(run.id,!run.starred);run.starred=!run.starred}
async function promote(run){try{await api.promotePlaygroundRun(run.id,0);toast.success('结果已加入素材库')}catch(e){toast.error(e.response?.data?.error||'提升失败')}}
onMounted(load)
</script>
<style scoped>.ref-block{display:grid;gap:8px}.ref-list{display:grid;gap:6px;margin:0;padding:0;list-style:none}.ref-row{display:flex;align-items:center;gap:10px}.ref-index{font-family:var(--mono,monospace);font-size:12px;color:var(--text-2,#6e6e73);min-width:66px}.ref-thumb{width:44px;height:44px;object-fit:cover;border-radius:8px;border:1px solid rgba(0,0,0,.1)}.ref-name{flex:1;font-size:12px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.ref-actions{display:flex;gap:6px}.run-results{display:flex;flex-wrap:wrap;gap:10px;margin:10px 0}.run-result{margin:0;max-width:220px;display:grid;gap:4px;font-size:12px}.run-result img{width:100%;max-height:220px;object-fit:contain;border-radius:8px;background:rgba(0,0,0,.04)}.run-result video,.run-result audio{width:100%;border-radius:8px}.run-result figcaption{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.raw-result summary{cursor:pointer;font-size:12px;color:var(--text-2,#6e6e73)}.form-card{display:grid;gap:14px;padding:18px}.field-row{display:grid;grid-template-columns:1fr 1fr 120px;gap:12px}.field-row label,.form-card>label{display:grid;gap:6px}.catalog-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(280px,1fr));gap:14px}.run-card{padding:16px}.result{max-height:130px;overflow:auto}.compare{display:grid;grid-template-columns:1fr 1fr;gap:16px;padding:18px}@media(max-width:700px){.field-row,.compare{grid-template-columns:1fr}}</style>
