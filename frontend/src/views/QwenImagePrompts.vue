<template>
  <div class="page prompt-page">
    <header class="page-head"><div><span class="overline">QWEN-IMAGE-2.1</span><h1>提示词生成程序</h1><p class="sub">独立生成文生图或多图编辑提示词；当前只输出提示词，不会提交任务，也不会改变 Krea2 或 MiniMax H3 流程。</p></div></header>
    <div v-if="error" class="card error">{{error}}</div>
    <section class="card program-card">
      <label>程序<select v-model="code" class="input"><option v-for="p in programs" :key="p.code" :value="p.code">{{p.name}}</option></select></label>
      <p class="sub">{{current?.description}}</p>
      <label>原始需求<textarea v-model="form.brief" class="textarea" rows="5" placeholder="输入你希望生成或编辑的画面要求"></textarea></label>
      <label>固定上下文（可选）<textarea v-model="form.context" class="textarea" rows="4" placeholder="角色、场景、风格、必须保留的文本或其他硬约束"></textarea></label>
      <label>明确画幅（可选）<input v-model="form.aspect_ratio" class="input" placeholder="例如 16:9；编辑任务留空时由画布图决定"></label>
      <section v-if="isEdit" class="refs">
        <div class="section-head"><div><h2>有序参考图职责</h2><p class="sub">顺序将固定映射到 &lt;image1&gt;、&lt;image2&gt;……，以后提交多图模板时必须保持相同顺序。</p></div><button class="btn btn-sm btn-secondary" :disabled="form.references.length>=16" @click="addRef">＋参考图</button></div>
        <article v-for="(ref,i) in form.references" :key="i" class="ref-row"><strong>&lt;image{{i+1}}&gt;</strong><input v-model="ref.role" class="input" placeholder="职责：人物身份/服装/目标场景/画布"><input v-model="ref.description" class="input" placeholder="可见事实或必须锁定的属性"><button class="btn btn-xs btn-danger" @click="form.references.splice(i,1)">删除</button></article>
      </section>
      <button class="btn" :disabled="busy||!form.brief.trim()||(isEdit&&!form.references.length)" @click="generate">{{busy?'生成中…':'生成 Qwen-Image-2.1 提示词'}}</button>
    </section>
    <section v-if="result" class="card result"><div class="section-head"><div><h2>生成结果</h2><p class="sub">{{result.target_mode}} · {{result.provider}}</p></div><button class="btn btn-sm btn-secondary" @click="copy">复制提示词</button></div><div class="meta"><span>wh_ratio: {{result.wh_ratio||'—'}}</span><span v-if="result.ratio_follow">ratio_follow: {{result.ratio_follow}}</span><span>模板：尚未接入，仅生成提示词</span></div><textarea class="textarea" rows="16" :value="result.prompt" readonly></textarea></section>
  </div>
</template>
<script setup>
import{computed,onMounted,reactive,ref,watch}from'vue'
import{api}from'../api'
import{useToastStore}from'../stores/toast'
const toast=useToastStore(),programs=ref([]),code=ref('qwen-image-2.1-t2i'),busy=ref(false),error=ref(''),result=ref(null)
const form=reactive({brief:'',context:'',aspect_ratio:'',references:[]})
const current=computed(()=>programs.value.find(p=>p.code===code.value)),isEdit=computed(()=>current.value?.target_mode==='multi-image-edit')
function addRef(){if(form.references.length<16)form.references.push({role:'',description:''})}
watch(code,()=>{result.value=null;if(isEdit.value&&!form.references.length)addRef()})
async function load(){try{programs.value=(await api.qwenImagePromptPrograms()).data.items||[]}catch(e){error.value=e.response?.data?.error||e.message}}
async function generate(){busy.value=true;error.value='';try{result.value=(await api.generateQwenImagePrompt(code.value,{...form,references:form.references.map(v=>({...v}))})).data}catch(e){error.value=e.response?.data?.error||e.message}finally{busy.value=false}}
async function copy(){await navigator.clipboard.writeText(result.value?.prompt||'');toast.success('提示词已复制')}
onMounted(load)
</script>
<style scoped>.prompt-page{max-width:980px;margin:auto}.program-card,.result{padding:22px;display:grid;gap:16px}.program-card label{display:grid;gap:7px}.refs{display:grid;gap:10px}.ref-row{display:grid;grid-template-columns:100px 1fr 2fr auto;gap:8px;align-items:center}.meta{display:flex;gap:12px;flex-wrap:wrap;color:var(--text-secondary)}.error{padding:14px;color:var(--red)}@media(max-width:720px){.ref-row{grid-template-columns:1fr}.page-head{display:block}}</style>
