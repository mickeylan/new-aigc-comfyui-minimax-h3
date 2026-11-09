<template>
  <section class="edit-timeline" aria-label="可缩放剪辑时间轴" @keydown="keyboard" tabindex="0">
    <div class="tools">
      <button type="button" :disabled="!canUndo" @click="travel('undo')">撤销</button>
      <button type="button" :disabled="!canRedo" @click="travel('redo')">重做</button>
      <label>缩放 <input aria-label="时间轴缩放" type="range" min="10" max="160" step="5" v-model.number="scale"></label>
      <label><input type="checkbox" v-model="snapping">吸附（8px）</label>
      <span>播放头 {{ playhead.toFixed(2) }} 秒 · 拖动两端裁剪，点击刻度定位；Ctrl/⌘ Z 撤销</span>
    </div>
    <div class="scroller">
      <div class="track" :style="{width:width+'px'}">
        <div class="ruler" @pointerdown="seek"><span v-for="tick in ticks" :key="tick" :style="{left:tick*scale+'px'}">{{tick}}s</span></div>
        <div v-for="row in rows" :key="clips[row.index].id" class="clip" :style="{left:row.start*scale+'px',width:Math.max(3,row.duration*scale)+'px'}" :title="clips[row.index].label">
          <button class="trim left" aria-label="拖拽裁剪入点" @pointerdown.prevent.stop="beginTrim($event,row,'start')">↔</button>
          <button class="label" @click="locate(row.start)">{{row.index+1}} · {{clips[row.index].label}}</button>
          <button class="trim right" aria-label="拖拽裁剪出点" @pointerdown.prevent.stop="beginTrim($event,row,'end')">↔</button>
        </div>
        <button v-for="(marker,i) in markers" :key="`marker-${i}`" type="button" class="beat-marker" :style="{left:marker.time*scale+'px'}" :title="marker.label" @click="locate(marker.time)">◆</button>
        <div class="playhead" :style="{left:playhead*scale+'px'}"></div>
      </div>
    </div>
    <p v-if="error" role="alert">{{error}}</p>
  </section>
</template>
<script setup>
import { computed, ref, watch } from 'vue'
import { layoutClips, snapTime, trimTimelineClip, createEditHistory } from '../utils/editingTimeline.js'
import { buildEditSequence } from '../utils/finalCutEditing.js'
const props=defineProps({clips:{type:Array,required:true},currentTime:{type:Number,default:0}})
const emit=defineEmits(['change','seek'])
const scale=ref(35),snapping=ref(true),playhead=ref(0),error=ref(''),preview=ref(null)
const history=createEditHistory(),canUndo=ref(false),canRedo=ref(false)
let travelling=false
function flags(){canUndo.value=history.canUndo;canRedo.value=history.canRedo}
watch(()=>props.clips,value=>{if(!travelling)history.record(value);travelling=false;flags()},{deep:true,immediate:true})
watch(()=>props.currentTime,value=>{playhead.value=value})
const clips=computed(()=>preview.value||props.clips)
const rows=computed(()=>layoutClips(clips.value))
const markers=computed(()=>rows.value.flatMap(row=>(clips.value[row.index].markers||[]).map(m=>({time:row.start+m.time,label:m.label}))))
const end=computed(()=>rows.value.at(-1)?.end||0)
const width=computed(()=>Math.max(600,end.value*scale.value+60))
const ticks=computed(()=>{const step=scale.value<25?10:scale.value<60?5:1;return Array.from({length:Math.min(10000,Math.ceil(end.value/step)+1)},(_,i)=>i*step)})
function locate(value){playhead.value=Math.max(0,Math.min(end.value,value));emit('seek',playhead.value)}
function seek(event){const rect=event.currentTarget.getBoundingClientRect();locate(snapTime((event.clientX-rect.left)/scale.value,[...rows.value.flatMap(r=>[r.start,r.end]),...markers.value.map(m=>m.time)],scale.value,snapping.value))}
function travel(direction){const value=history[direction]();if(value){travelling=true;emit('change',value)}flags()}
function keyboard(event){if(/input|textarea|select/i.test(event.target.tagName))return;if((event.ctrlKey||event.metaKey)&&event.key.toLowerCase()==='z'){event.preventDefault();travel(event.shiftKey?'redo':'undo')}}
function beginTrim(event,row,edge){
 const target=event.currentTarget,origin=event.clientX,source=JSON.parse(JSON.stringify(props.clips)),clip=source[row.index]
 const initial=edge==='start'?row.start:row.end
 const targets=[0,playhead.value,...markers.value.map(m=>m.time),...rows.value.filter(r=>r.index!==row.index).flatMap(r=>[r.start,r.end])]
 target.setPointerCapture(event.pointerId);error.value=''
 function move(e){try{const desired=snapTime(initial+(e.clientX-origin)/scale.value,targets,scale.value,snapping.value&&!e.altKey);const updated=trimTimelineClip(clip,edge,desired-initial);preview.value=source.map((c,i)=>i===row.index?updated:c)}catch(err){error.value=err.message}}
 function finish(e){target.removeEventListener('pointermove',move);target.removeEventListener('pointerup',finish);target.removeEventListener('pointercancel',cancel);if(preview.value){try{buildEditSequence(preview.value);emit('change',preview.value)}catch(err){error.value=err.message}}preview.value=null;if(target.hasPointerCapture(e.pointerId))target.releasePointerCapture(e.pointerId)}
 function cancel(){preview.value=null;target.removeEventListener('pointermove',move);target.removeEventListener('pointerup',finish);target.removeEventListener('pointercancel',cancel)}
 target.addEventListener('pointermove',move);target.addEventListener('pointerup',finish);target.addEventListener('pointercancel',cancel)
}
</script>
<style scoped>
.edit-timeline{border:1px solid var(--border,#444);padding:10px;border-radius:8px}.tools{display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin-bottom:8px}.tools label{display:flex;align-items:center;gap:5px}.tools span{font-size:12px}.scroller{overflow-x:auto;padding-bottom:12px}.track{position:relative;height:100px;background:#171e2b}.ruler{height:28px;position:relative;cursor:crosshair;border-bottom:1px solid #556}.ruler span{position:absolute;font-size:11px;border-left:1px solid #889;padding-left:3px;pointer-events:none}.clip{position:absolute;top:38px;height:50px;display:flex;background:#345586;border:1px solid #94b5ef;border-radius:4px;overflow:hidden}.label{flex:1;overflow:hidden;white-space:nowrap;text-overflow:ellipsis;background:none;border:0;color:white;text-align:left;min-width:0}.trim{width:12px;padding:0;border:0;background:#92b3df;color:#142238;cursor:ew-resize;touch-action:none;flex-shrink:0}.beat-marker{position:absolute;top:24px;background:none;color:#ffd16a;border:0;transform:translateX(-50%);cursor:pointer;z-index:2}.playhead{position:absolute;top:0;height:100%;width:2px;background:#ffba53;pointer-events:none}
</style>
