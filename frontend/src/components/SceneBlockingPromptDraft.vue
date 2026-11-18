<template>
  <section class="card previs-prompt-review">
    <h3>3D预演 · {{request.kind==='image'?'分镜图':'视频'}}提示词草稿</h3>
    <p class="field-hint">原文、Shot与对白仍为权威，3D仅补充空间建议。预览不会保存；确认采用会同时保存站位并替换对应提示词，不启动生成。</p>
    <p v-if="error" role="alert">{{error}}</p><p v-if="busy" role="status">正在准备草稿…</p>
    <textarea v-if="ready" v-model="prompt" class="textarea" rows="14" aria-label="3D提示词草稿（可编辑）" :disabled="applying" />
    <div class="modal-actions"><button class="btn btn-secondary" type="button" :disabled="busy||applying" @click="load">重新准备草稿</button><button class="btn" type="button" :disabled="!ready||busy||applying||!prompt.trim()" @click="apply">确认采用：同时保存站位与提示词</button><button class="btn btn-ghost" type="button" :disabled="applying" @click="$emit('close')">放弃草稿</button></div>
  </section>
</template>
<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { api } from '../api'
import { requestErrorMessage } from '../utils/requestError.js'
const props=defineProps({projectId:{type:[String,Number],required:true},sceneId:{type:Number,required:true},request:{type:Object,required:true}})
const emit=defineEmits(['adopted','close'])
const prompt=ref(''),error=ref(''),busy=ref(false),applying=ref(false),ready=ref(false)
let alive=true,epoch=0,preview=null
async function load(){const token=++epoch;busy.value=true;ready.value=false;error.value=''
 try{const {data}=await api.previewSceneBlockingPrompt(props.projectId,props.sceneId,props.request.kind,{blocking:props.request.blocking});if(!alive||token!==epoch)return;preview=data;prompt.value=data.prompt;ready.value=true}
 catch(e){if(alive&&token===epoch)error.value=requestErrorMessage(e,'生成3D提示词草稿失败；原提示词未修改')}
 finally{if(alive&&token===epoch)busy.value=false}}
async function apply(){if(!ready.value||!preview||applying.value)return
 if(!window.confirm('确认同时保存此预演站位，并以审阅后的草稿替换对应提示词（包括已确认稿）？不会启动图片或视频生成。'))return
 const token=epoch;applying.value=true;error.value=''
 const payload={prompt:prompt.value,blocking:preview.blocking,blocking_hash:preview.blocking_hash,source_version:preview.source_version,confirm_replace:true}
 try{const {data}=await api.applySceneBlockingPrompt(props.projectId,props.sceneId,props.request.kind,payload);if(alive&&token===epoch){ready.value=false;emit('adopted',{...data,kind:props.request.kind})}}
 catch(e){if(alive&&token===epoch){error.value=requestErrorMessage(e,'采用失败；草稿保留');if(e.response?.status===409)ready.value=false}}
 finally{if(alive&&token===epoch)applying.value=false}}
onMounted(load)
onBeforeUnmount(()=>{alive=false;epoch++})
</script>
<style scoped>.previs-prompt-review{padding:1rem;margin:1rem 0}.previs-prompt-review textarea{width:100%;box-sizing:border-box}.previs-prompt-review [role=alert]{white-space:pre-wrap;color:var(--danger)}</style>
