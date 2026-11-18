<template>
  <details class="preparation-stages" @toggle="onToggle">
    <summary>准备阶段记录 · 成功步骤复用 / 失败步骤重试</summary>
    <p class="field-hint">同一输入版本的成功步骤会跨重启复用。原入口再次执行时，仅请求失败或未完成步骤；输入、模型、技能或参考内容变化时建立新版本。历史产物不自动覆盖当前表单。</p>
    <button type="button" class="btn btn-xs btn-secondary" :disabled="busy" @click="load">刷新记录</button>
    <p v-if="error" role="alert">{{error}}</p><p v-else-if="!busy&&!rows.length">尚无阶段记录；下次生成草稿或翻译时记录。</p>
    <article v-for="r in rows" :key="r.id" class="stage-row"><strong>{{label(r.stage)}}</strong> · {{state(r.status)}} · 请求{{r.attempts}}次 / 复用{{r.reuse_count}}次 <code>{{r.input_hash.slice(0,12)}}</code><p v-if="r.error" role="alert">{{r.error}}</p><details v-if="r.result"><summary>查看该版本草稿</summary><pre>{{readable(r.result)}}</pre></details></article>
  </details>
</template>
<script setup>
import { onBeforeUnmount, ref } from 'vue'
import { api } from '../api'
import { requestErrorMessage } from '../utils/requestError.js'
const props=defineProps({projectId:{type:[String,Number],required:true},sceneId:{type:Number,required:true}})
const rows=ref([]),error=ref(''),busy=ref(false)
let alive=true,epoch=0
function label(stage){return stage==='video_prompt'?'视频动作草稿':stage.startsWith('translation_')?stage.replace('translation_en_chunk_','英文翻译 · 段 ').replace('translation_zh_chunk_','中文预览 · 段 '):stage.replace('plot_anchor_or_structure_repair_','剧情锚点/结构修复 · 步 ')}
const state=status=>({success:'成功，可复用',failed:'失败，原入口可重试',warning:'有警告，需审阅；下次重新执行',running:'执行中或上次中断，可从原入口恢复'}[status]||status)
function readable(raw){try{const value=JSON.parse(raw);return value.text||value.Output||JSON.stringify(value,null,2)}catch{return raw}}
function onToggle(e){if(e.target.open)load()}
async function load(){if(busy.value)return;const token=++epoch;busy.value=true;error.value='';try{const {data}=await api.scenePreparationStages(props.projectId,props.sceneId);if(alive&&token===epoch)rows.value=data.items||[]}catch(e){if(alive&&token===epoch)error.value=requestErrorMessage(e,'读取阶段记录失败')}finally{if(alive&&token===epoch)busy.value=false}}
onBeforeUnmount(()=>{alive=false;epoch++})
</script>
<style scoped>
.preparation-stages{margin:1rem 0;padding:.7rem;border:1px solid var(--border);border-radius:6px}.stage-row{border-top:1px solid var(--border);padding:.5rem 0}.stage-row pre{white-space:pre-wrap;max-height:260px;overflow:auto}
</style>
