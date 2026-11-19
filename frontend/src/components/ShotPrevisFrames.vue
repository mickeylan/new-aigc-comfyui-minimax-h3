<template>
 <section class="shot-frames"><h4>选帧 → 绑定 Shot → 分镜图草稿</h4>
 <p>播放头 {{(timeMs/1000).toFixed(3)}} 秒。先保存预演，再暂停选帧绑定已有 Shot。每次绑定保留独立历史，不改剧情、对白或原基础站位。</p>
 <p v-if="dirty" class="warning">预演有未保存编辑，请先保存；不能将未保存画面绑定到旧版本。</p>
 <p v-if="error" role="alert">{{error}}</p>
 <div class="controls"><select v-model.number="shotID" aria-label="选帧目标Shot" :disabled="busy"><option :value="0">选择已有 Shot</option><option v-for="s in shots" :key="s.id" :value="s.id">Shot {{s.order}} · {{s.description||s.id}}</option></select><button type="button" :disabled="busy||disabled||dirty||!revision||!shotID" @click="bind">确认绑定当前帧</button><button type="button" :disabled="busy" @click="load">刷新绑定状态</button></div>
 <p v-if="!shots.length">当前 Scene 尚无 Shot，请先在原有导演镜头流程建立镜头；此处不自动拆分或新建。</p>
 <details v-for="item in items" :key="item.id" :open="active?.id===item.id"><summary>Shot {{shots.find(s=>s.id===item.shot_id)?.order??('#'+item.shot_id)}} · {{item.time_ms/1000}}秒 · 预演 v{{item.previs_revision}} · 绑定 #{{item.id}} {{item.stale?'（已过期）':''}}</summary>
 <p v-if="item.stale" class="warning">{{item.stale_reason}}。历史保留；请核对后重新绑定。</p><p v-if="item.applied_prompt_hash">此绑定曾采用提示词；不代表之后编辑的当前提示词仍一致。</p>
 <button type="button" :disabled="busy||disabled||dirty||item.stale" @click="preview(item)">基于此帧准备分镜图草稿</button><button type="button" :disabled="busy||disabled" @click="exportItem(item,'white')">此帧白模 ZIP</button><button type="button" :disabled="busy||disabled" @click="exportItem(item,'depth')">此帧深度 ZIP</button>
 <p class="hint">导出冻结历史帧而非当前播放头；此处深度映射范围固定0.1～50米。文件仅下载，缺GLB时拒绝占位导出，不接入生成。</p>
 </details>
 <div v-if="active" class="review"><h4>Shot #{{active.shot_id}} · {{active.time_ms/1000}}秒 · 分镜图提示词审阅</h4><p>以该 Shot 为画面目标；只提取帧的空间构图，不猜测GLB姿态含义或服装外观。确认采用会替换当前 Scene 的分镜图提示词，不修改其他 Shot，也不启动生成。</p><textarea aria-label="选帧分镜图草稿" v-model="prompt" rows="10" :disabled="busy||!sourceVersion"/><button type="button" :disabled="busy||disabled||dirty||!sourceVersion||!prompt.trim()" @click="apply">确认替换当前 Scene 分镜图提示词</button><button type="button" :disabled="busy" @click="clearPreview">放弃草稿</button></div>
 <p role="status">{{status}}</p>
 </section>
</template>
<script setup>
import {ref,onMounted,onBeforeUnmount,watch} from 'vue'
import {api} from '../api'
import {requestErrorMessage} from '../utils/requestError.js'
const props=defineProps({projectId:[String,Number],sceneId:Number,revision:Number,sourceHash:String,dirty:Boolean,disabled:Boolean,timeMs:{type:Number,default:0}})
const emit=defineEmits(['freeze','export-frame','adopted'])
const shots=ref([]),items=ref([]),shotID=ref(0),busy=ref(false),error=ref(''),status=ref(''),active=ref(null),prompt=ref(''),sourceVersion=ref('')
let alive=true,epoch=0
function clearPreview(){epoch++;active.value=null;sourceVersion.value='';prompt.value=''}
async function load(){const token=++epoch;busy.value=true;error.value='';sourceVersion.value='';active.value=null;try{const{data}=await api.listShotPrevisFrames(props.projectId,props.sceneId);if(!alive||token!==epoch)return;shots.value=data.shots||[];items.value=data.items||[]}catch(e){if(alive&&token===epoch)error.value=requestErrorMessage(e,'读取帧绑定失败')}finally{if(alive&&token===epoch)busy.value=false}}
async function bind(){emit('freeze');if(!window.confirm(`将已保存预演第${(props.timeMs/1000).toFixed(3)}秒冻结绑定到Shot #${shotID.value}？不会修改提示词或生成。`))return;const token=++epoch;busy.value=true;error.value='';sourceVersion.value='';active.value=null;try{await api.bindShotPrevisFrame(props.projectId,props.sceneId,{shot_id:shotID.value,previs_revision:props.revision,time_ms:Math.round(props.timeMs),source_content_hash:props.sourceHash});if(!alive||token!==epoch)return;status.value='已冻结绑定帧；可准备草稿或下载白模/深度';await load()}catch(e){if(alive&&token===epoch)error.value=requestErrorMessage(e,'绑定失败，请保留编辑后刷新状态')}finally{if(alive&&token===epoch)busy.value=false}}
async function preview(item){emit('freeze');const token=++epoch;busy.value=true;error.value='';sourceVersion.value='';active.value=null;try{const{data}=await api.previewShotPrevisFramePrompt(props.projectId,props.sceneId,item.id);if(!alive||token!==epoch)return;active.value=data.item||item;prompt.value=data.prompt;sourceVersion.value=data.source_version}catch(e){if(alive&&token===epoch)error.value=requestErrorMessage(e,'帧草稿失败，原提示词未修改')}finally{if(alive&&token===epoch)busy.value=false}}
async function apply(){if(!window.confirm('确认以审阅后的选帧草稿替换当前 Scene 分镜图提示词？现有提示词将被替换，但不启动生成。'))return;const token=++epoch;busy.value=true;error.value='';try{const{data}=await api.applyShotPrevisFramePrompt(props.projectId,props.sceneId,active.value.id,{prompt:prompt.value,source_version:sourceVersion.value,confirm_replace:true});if(!alive||token!==epoch)return;emit('adopted',data);status.value='已保存分镜图提示词；请回到分镜显式生成';await load()}catch(e){if(alive&&token===epoch){error.value=requestErrorMessage(e,'采用失败，草稿保留');if(e.response?.status===409)sourceVersion.value=''}}finally{if(alive&&token===epoch)busy.value=false}}
function exportItem(item,mode){emit('freeze');emit('export-frame',{item,mode})}
watch(()=>[props.dirty,props.revision,props.sourceHash],()=>{clearPreview();busy.value=false})
watch(()=>props.revision,()=>load())
onMounted(load)
onBeforeUnmount(()=>{alive=false;epoch++})
</script>
<style scoped>.shot-frames{border:1px solid var(--border);padding:.8rem;margin:.8rem 0}.controls{display:flex;flex-wrap:wrap;gap:.5rem}.controls select{max-width:70%}textarea{width:100%;box-sizing:border-box}.warning,[role=alert]{color:var(--danger,#c34545)}details,.review{padding:.5rem;margin:.5rem 0;border:1px solid var(--border)}button{margin:.3rem}.hint{font-size:.85rem}</style>
