<template>
 <section class="timeline"><h4>路线 / 机位时间轴（整体移动；GLB骨骼片段在下方绑定）</h4>
 <p>动画不写入提示词；原有采用按钮仍使用静态基础站位。先选对象，再在地面点选 A、B，或用当前对象位置记录端点。</p>
 <div class="controls"><label>时长（秒）<input type="number" min="1" max="120" step="0.1" :value="document.duration_ms/1000" @change="setDuration($event.target.value)"></label><label>播放头 {{(time/1000).toFixed(2)}}秒<input type="range" min="0" :max="document.duration_ms" step="10" v-model.number="time" @input="pause(false);showSample()"></label><button type="button" @click="toggle">{{playing?'暂停':'播放'}}</button><button type="button" @click="stop">停止 / 返回静态编辑</button></div>
 <div class="controls"><button type="button" :disabled="!selected" @click="pick('a')">地面标记 A</button><button type="button" :disabled="!selected" @click="pick('b')">地面标记 B</button><button type="button" :disabled="!selected" @click="mark('a')">当前位置设 A</button><button type="button" :disabled="!selected" @click="mark('b')">当前位置设 B</button><button type="button" :disabled="!selected" @click="addObjectFrame">当前时间记录对象帧</button><button type="button" @click="addCameraFrame">当前时间记录机位帧</button><label><input v-model="cut" type="checkbox">此机位硬切</label></div>
 <div v-for="route in document.routes" :key="route.id" class="controls"><span>{{name(route.object_id)}}：A {{route.a}} → B {{route.b}}</span><label>起秒<input type="number" v-model.number="start" min="0" step="0.1"></label><label>止秒<input type="number" v-model.number="end" min="0" step="0.1"></label><button type="button" @click="animateRoute(route)">生成两端移动帧</button><button type="button" @click="remove('routes',route)">删除路线</button></div>
 <details><summary>关键帧（点击时间定位；同对象同时间再次记录即替换，可单独删除）</summary><div v-for="k in document.keyframes" :key="`${k.object_id}/${k.time_ms}`"><button type="button" @click="seek(k.time_ms)">{{name(k.object_id)}} · {{k.time_ms/1000}}秒</button> {{k.position}} / {{k.yaw}}° <button type="button" @click="remove('keyframes',k)">删除</button></div><div v-for="k in document.camera_keyframes" :key="k.time_ms"><button type="button" @click="seek(k.time_ms)">机位 · {{k.time_ms/1000}}秒 {{k.cut?'硬切':'移动'}}</button><button type="button" @click="remove('camera_keyframes',k)">删除</button></div></details>
 <h4>Agent 编排</h4><textarea v-model="command" rows="3" placeholder="例如：让甲从 A 走到 B；我会补问起止时间和方向。只产生计划，不自动执行。" :disabled="busy"></textarea><button type="button" :disabled="busy||!command.trim()" @click="prepare">{{busy?'正在准备…':'补问 / 准备计划'}}</button>
 <div v-if="questions.length"><p v-for="q in questions" :key="q">{{q}}</p><textarea v-model="answer" placeholder="补充回答后继续准备"></textarea><button type="button" :disabled="busy||!answer.trim()" @click="respond">提交补充回答</button></div>
 <div v-if="plan"><p>{{plan.summary}}</p><label v-for="(op,i) in plan.operations" :key="i" class="operation"><input type="checkbox" v-model="chosen" :value="i">{{op.kind}}<pre>{{JSON.stringify(op,null,2)}}</pre></label><button type="button" :disabled="!chosen.length" @click="execute">确认执行所选操作到本地预演草稿</button></div>
 <button type="button" :disabled="!history.length" @click="undo">撤销上一步时间轴操作（仅本次会话）</button><p v-if="error" role="alert">{{error}}</p><p role="status">{{status}}</p>
 </section>
</template>
<script setup>
import {computed,onBeforeUnmount,ref,watch} from 'vue'
import {api} from '../api'
import {clonePrevis} from '../utils/scenePrevisualization.js'
import {applyPrevisPlan,sampleTimeline,validateTimeline} from '../utils/previsTimeline.js'
import {requestErrorMessage} from '../utils/requestError.js'
const props=defineProps({document:{type:Object,required:true},selectedId:{type:String,default:''},projectId:{type:[String,Number],required:true},sceneId:{type:Number,required:true}})
const emit=defineEmits(['update-document','sample','pick'])
const selected=computed(()=>props.document.objects.find(o=>o.id===props.selectedId))
const time=ref(0),playing=ref(false),cut=ref(false),start=ref(0),end=ref(3),command=ref(''),answer=ref(''),questions=ref([]),plan=ref(null),chosen=ref([]),busy=ref(false),error=ref(''),status=ref(''),history=ref([])
let frame=0,last=0,alive=true,epoch=0
const name=id=>props.document.objects.find(o=>o.id===id)?.name||id
function pause(clear=true){playing.value=false;cancelAnimationFrame(frame);frame=0;if(clear)emit('sample',null)}
function stop(){pause();time.value=0}
function showSample(){emit('sample',sampleTimeline(props.document,time.value),time.value)}
function seek(t){pause(false);time.value=t;showSample()}
function tick(now){if(!playing.value||!alive)return;time.value=Math.min(props.document.duration_ms,time.value+Math.max(0,now-last));last=now;showSample();if(time.value>=props.document.duration_ms){pause(false);return}frame=requestAnimationFrame(tick)}
function toggle(){if(playing.value){pause(false);return}if(time.value>=props.document.duration_ms)time.value=0;playing.value=true;last=performance.now();frame=requestAnimationFrame(tick)}
function update(next){const e=validateTimeline(next);if(e){error.value=e;return}pause();history.value.push({before:clonePrevis(props.document),after:JSON.stringify(next)});if(history.value.length>30)history.value.shift();emit('update-document',next);error.value=''}
function apply(operations){try{update(applyPrevisPlan(props.document,{base_hash:JSON.stringify(props.document),operations}))}catch(e){error.value=e.message}}
function setDuration(v){const next=clonePrevis(props.document);next.duration_ms=Math.round(Number(v)*1000);update(next)}
function pick(point){pause();emit('pick',point)}
function mark(point){if(!selected.value)return;const existing=props.document.routes.find(r=>r.object_id===selected.value.id),r=existing?clonePrevis(existing):{id:`route-${selected.value.id}`,object_id:selected.value.id,a:clonePrevis(selected.value.position),b:clonePrevis(selected.value.position)};r[point]=clonePrevis(selected.value.position);apply([{kind:'route',route:r}])}
function addObjectFrame(){if(selected.value)apply([{kind:'keyframe',keyframe:{object_id:selected.value.id,time_ms:Math.round(time.value),position:clonePrevis(selected.value.position),yaw:selected.value.yaw}}])}
function addCameraFrame(){apply([{kind:'camera_keyframe',camera_keyframe:{time_ms:Math.round(time.value),camera:clonePrevis(props.document.camera),cut:cut.value}}])}
function animateRoute(r){if(!Number.isFinite(start.value)||!Number.isFinite(end.value)||end.value<=start.value){error.value='结束时间必须大于开始时间';return}const dx=r.b.x-r.a.x,dz=r.b.z-r.a.z,yaw=Math.atan2(dx,dz)*180/Math.PI;apply([{kind:'keyframe',keyframe:{object_id:r.object_id,time_ms:Math.round(start.value*1000),position:r.a,yaw}},{kind:'keyframe',keyframe:{object_id:r.object_id,time_ms:Math.round(end.value*1000),position:r.b,yaw}}])}
function remove(field,item){const next=clonePrevis(props.document);const index=props.document[field].indexOf(item);if(index<0)return;next[field].splice(index,1);update(next)}
async function prepare(){const token=++epoch,base=JSON.stringify(props.document),text=command.value;busy.value=true;error.value='';plan.value=null;questions.value=[]
 try{const {data}=await api.planScenePrevisualization(props.projectId,props.sceneId,{document:JSON.parse(base),command:text});if(!alive||token!==epoch||base!==JSON.stringify(props.document)||text!==command.value)return;questions.value=data.questions||[];if(!questions.value.length){plan.value={...data,base_hash:base};chosen.value=(data.operations||[]).map((_,i)=>i)}}catch(e){if(alive&&token===epoch)error.value=requestErrorMessage(e,'准备计划失败，未修改预演')}finally{if(alive&&token===epoch)busy.value=false}}
function respond(){command.value+='\n补充回答：'+answer.value;answer.value='';prepare()}
function execute(){try{const next=applyPrevisPlan(props.document,plan.value,chosen.value);if(!window.confirm('将所选操作应用到本地预演草稿？保存前不会写入服务器，也不会修改提示词。'))return;update(next);plan.value=null;status.value='计划已应用到本地草稿，请播放检查后保存预演'}catch(e){error.value=e.message}}
function undo(){const h=history.value.at(-1);if(!h)return;if(JSON.stringify(props.document)!==h.after){error.value='后续已有其他编辑，为避免覆盖不能撤销，请先手工核对';return}history.value.pop();pause();emit('update-document',h.before)}
watch(()=>JSON.stringify(props.document),()=>{epoch++;busy.value=false;plan.value=null;questions.value=[];pause();time.value=Math.min(time.value,props.document.duration_ms)})
watch(command,()=>{plan.value=null;questions.value=[]})
defineExpose({pause:()=>pause(false)})
onBeforeUnmount(()=>{alive=false;epoch++;pause()})
</script>
<style scoped>.timeline{border:1px solid var(--border);padding:.8rem;margin:.8rem 0}.controls{display:flex;flex-wrap:wrap;gap:.5rem;align-items:center;margin:.5rem 0}input[type=number]{width:5rem}textarea{width:100%;box-sizing:border-box}.operation{display:block}pre{white-space:pre-wrap;font-size:.8rem;max-height:180px;overflow:auto}[role=alert]{color:var(--danger)}</style>
