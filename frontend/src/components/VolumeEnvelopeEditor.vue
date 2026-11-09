<template>
 <fieldset class="volume-envelope">
  <legend>独立音频层音量包络</legend>
  <p>本层开始后的秒数，点间线性插值，首尾保持；增益乘以本层基础音量。保存音频层后生效。</p>
  <svg viewBox="0 0 800 80" preserveAspectRatio="none" aria-label="独立音频层包络"><polyline :points="curve" stroke="#80dfab" fill="none" stroke-width="2"/></svg>
  <div class="row"><label>秒<input v-model.number="time" type="number" min="0" :max="duration" step=".01"></label><label>增益<input v-model.number="gain" type="number" min="0" max="4" step=".05"></label><button type="button" @click="add">添加/更新点</button><button type="button" @click="emit('update:modelValue',[])">清空</button></div>
  <div v-for="(point,i) in modelValue" :key="i" class="row"><span>{{point.time.toFixed(3)}}秒 · 增益 {{point.gain}}</span><button type="button" @click="time=point.time;gain=point.gain">编辑</button><button type="button" @click="emit('update:modelValue',modelValue.filter((_,j)=>i!==j))">删除</button></div>
  <p v-if="error" role="alert">{{error}}</p>
 </fieldset>
</template>
<script setup>
import {ref,computed} from 'vue'
import {normalizeClipTracks,envelopeAt} from '../utils/clipTracks.js'
const props=defineProps({modelValue:{type:Array,default:()=>[]},duration:{type:Number,required:true}}),emit=defineEmits(['update:modelValue'])
const time=ref(0),gain=ref(1),error=ref('')
function add(){try{const points=props.modelValue.filter(p=>Math.abs(p.time-time.value)>.001);points.push({time:Number(time.value.toFixed(3)),gain:gain.value});points.sort((a,b)=>a.time-b.time);normalizeClipTracks({volume_envelope:points},props.duration);emit('update:modelValue',points);error.value=''}catch(e){error.value=e.message}}
const curve=computed(()=>Array.from({length:81},(_,i)=>`${i*10},${80-envelopeAt(props.modelValue,i/80*props.duration)*20}`).join(' '))
</script>
<style scoped>
.volume-envelope{grid-column:1/-1;padding:8px;border:1px solid #56647d}.volume-envelope p{font-size:12px}.volume-envelope svg{width:100%;height:80px;background:#192233}.row{display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin:6px 0}.row label{display:flex;gap:4px}.row input{width:80px}
</style>
