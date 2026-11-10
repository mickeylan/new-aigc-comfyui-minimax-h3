<template>
 <section class="global-tracks" aria-label="跨片段自由叠加轨">
  <h3>跨片段图片 / 视频轨</h3>
  <p>独立于主视频切点，使用成片秒数，可跨越多个Scene及转场。拖动条移动、拖动两端裁剪，Alt关闭吸附；列表越靠后越在上层。字幕/功能文字仍在最上层。</p>
  <div class="tools"><select v-model.number="material"><option :value="0">选择项目图片或视频</option><option v-for="m in visuals" :key="m.id" :value="m.id">{{m.name||m.path}}</option></select><button type="button" @click="add">播放头插入叠加素材</button><label>缩放<input type="range" min="10" max="160" step="5" v-model.number="scale"></label></div>
  <div class="scroll"><div class="lanes" :style="{width:Math.max(600,duration*scale+60)+'px',height:(50+tracks.length*40)+'px'}">
   <div class="ruler" @pointerdown="seek"><span v-for="t in ticks" :key="t" :style="{left:t*scale+'px'}">{{t}}s</span></div>
   <div v-for="(row,i) in tracks" :key="i" class="lane" :style="{top:(35+i*40)+'px'}">
    <div class="bar" :class="{muted:row.muted}" :style="{left:row.start*scale+'px',width:(row.end-row.start)*scale+'px'}" @pointerdown.prevent="drag($event,i,'move')">
     <button aria-label="拖动全局轨入点" class="handle" @pointerdown.prevent.stop="drag($event,i,'start')">↔</button><span>{{i+1}} · {{name(row.material_id)}}</span><button aria-label="拖动全局轨出点" class="handle" @pointerdown.prevent.stop="drag($event,i,'end')">↔</button>
    </div>
   </div><div class="cursor" :style="{left:currentTime*scale+'px'}"></div>
  </div></div>
  <div v-for="(tr,i) in modelValue" :key="i" class="track-fields">
   <strong>{{i+1}} · {{name(tr.material_id)}}</strong>
   <label v-for="f in fields" :key="f.key">{{f.label}}<input type="number" :value="tr[f.key]" :min="f.min" :max="f.max??duration" :step="f.step" @change="edit(i,f.key,Number($event.target.value))"></label>
   <label><input type="checkbox" :checked="tr.muted" @change="edit(i,'muted',$event.target.checked)">隐藏</label>
   <button type="button" :disabled="i===0" @click="move(i,-1)">下一层</button><button type="button" :disabled="i===modelValue.length-1" @click="move(i,1)">上一层</button><button type="button" @click="emit('update:modelValue',modelValue.filter((_,n)=>n!==i))">删除</button>
  </div>
  <p v-if="error" role="alert">{{error}}</p><small>不修改原素材。短视频循环；只取画面，声音另用独立AudioLayer。所有轨道进入工程草稿和导出快照；即时播放器不合成这些轨道，请用“渲染完整预览”。最多8层。</small>
 </section>
</template>
<script setup>
import {ref,computed,onBeforeUnmount} from 'vue'
import {normalizeGlobalTracks,dragGlobalTrack} from '../utils/globalTracks.js'
import {layoutClips} from '../utils/editingTimeline.js'
const props=defineProps({modelValue:{type:Array,default:()=>[]},clips:{type:Array,required:true},materials:{type:Array,default:()=>[]},duration:{type:Number,required:true},currentTime:{type:Number,default:0}})
const emit=defineEmits(['update:modelValue','seek'])
const material=ref(0),scale=ref(35),error=ref(''),preview=ref(null)
const tracks=computed(()=>preview.value||props.modelValue),visuals=computed(()=>props.materials.filter(m=>['image','video'].includes(m.type)))
const ticks=computed(()=>{const step=scale.value<25?10:scale.value<60?5:1;return Array.from({length:Math.min(10000,Math.ceil(props.duration/step)+1)},(_,i)=>i*step)})
const fields=[{key:'start',label:'入点',min:0,step:.01},{key:'end',label:'出点',min:0,step:.01},{key:'source_start',label:'素材起点',min:0,max:86400,step:.01},{key:'x',label:'X',min:0,max:1,step:.01},{key:'y',label:'Y',min:0,max:1,step:.01},{key:'width',label:'宽',min:.05,max:1,step:.05},{key:'opacity',label:'透明度',min:0,max:1,step:.05}]
function name(id){return visuals.value.find(m=>Number(m.id)===id)?.name||`素材${id}`}
function commit(value){try{emit('update:modelValue',normalizeGlobalTracks(value,props.duration,props.materials));error.value=''}catch(e){error.value=e.message}}
function add(){if(!material.value||props.duration<=.05)return;const start=Math.min(Math.max(0,props.currentTime),props.duration-.05);commit([...props.modelValue,{material_id:material.value,start,end:Math.min(props.duration,start+5),source_start:0,x:0,y:0,width:.35,opacity:1,muted:false}])}
function edit(i,key,value){commit(props.modelValue.map((t,n)=>n===i?{...t,[key]:value}:t))}
function move(i,delta){const next=[...props.modelValue];[next[i],next[i+delta]]=[next[i+delta],next[i]];commit(next)}
function seek(e){const rect=e.currentTarget.getBoundingClientRect();emit('seek',Math.max(0,Math.min(props.duration,(e.clientX-rect.left)/scale.value)))}
let cancelDrag=null
function drag(e,index,mode){
 cancelDrag?.();const target=e.currentTarget,x=e.clientX,source=JSON.parse(JSON.stringify(props.modelValue)),duration=props.duration
 const targets=[0,duration,props.currentTime,...layoutClips(props.clips).flatMap(r=>[r.start,r.end]),...source.filter((_,n)=>n!==index).flatMap(t=>[t.start,t.end])]
 target.setPointerCapture(e.pointerId)
 function move(ev){try{preview.value=source.map((t,n)=>n===index?dragGlobalTrack(t,mode,(ev.clientX-x)/scale.value,duration,targets,scale.value,!ev.altKey):t);error.value=''}catch(err){error.value=err.message}}
 function clean(){target.removeEventListener('pointermove',move);target.removeEventListener('pointerup',finish);target.removeEventListener('pointercancel',cancel);if(target.hasPointerCapture(e.pointerId))target.releasePointerCapture(e.pointerId);preview.value=null;cancelDrag=null}
 function finish(){const next=preview.value;clean();if(next&&duration===props.duration&&JSON.stringify(source)===JSON.stringify(props.modelValue))commit(next);else if(next)error.value='工程已变化，本次拖动未应用'}
 function cancel(){clean()}
 cancelDrag=cancel;target.addEventListener('pointermove',move);target.addEventListener('pointerup',finish);target.addEventListener('pointercancel',cancel)
}
onBeforeUnmount(()=>cancelDrag?.())
</script>
<style scoped>
.global-tracks{border:1px solid #677792;border-radius:6px;padding:10px;margin:12px 0}.global-tracks p,.global-tracks small{font-size:12px}.tools,.track-fields{display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin:8px 0}.track-fields input[type=number]{width:70px}.track-fields label{font-size:12px;display:flex;gap:4px}.scroll{overflow-x:auto;padding-bottom:10px}.lanes{position:relative;background:#192232}.ruler{height:28px;position:relative;cursor:crosshair}.ruler span{position:absolute;border-left:1px solid #788;font-size:11px;pointer-events:none}.lane{position:absolute;width:100%;height:32px;border-bottom:1px solid #394050}.bar{position:absolute;background:#7655aa;height:30px;border:1px solid #c7b3e4;display:flex;cursor:grab;touch-action:none;min-width:4px}.bar span{flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:12px}.handle{padding:0;width:12px;border:0;background:#b7a3d9;cursor:ew-resize;touch-action:none}.muted{opacity:.4}.cursor{position:absolute;top:0;bottom:0;width:2px;background:#ffd26a;pointer-events:none}
</style>
