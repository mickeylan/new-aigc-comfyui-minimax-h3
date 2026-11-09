<template>
 <details class="clip-tracks" @toggle="open=$event.target.open">
  <summary>多轨 / 波形 / 音量包络 / 打点</summary>
  <template v-if="open">
   <p>以下时间均为本片段裁剪、变速后的秒数。叠加轨从上到下依次覆盖；音量包络只控制本片段原声，对白/BGM/音效继续用独立音频层。</p>
   <video ref="player" :src="clip.video_url" controls preload="metadata" @loadedmetadata="seek(0)" @timeupdate="onTime" @play="applyPreview"></video>
   <AudioWaveform :src="clip.video_url" :start="Number(clip.source_start||0)" :end="Number(clip.source_end??clip.duration)" @seek="seekSource" />
   <div class="row"><label>片段播放头 <input type="number" min="0" :max="duration" step=".01" :value="time" @change="seek(Number($event.target.value))"></label><span>/ {{duration.toFixed(2)}}秒</span></div>
   <svg viewBox="0 0 800 100" class="lane-view" preserveAspectRatio="none" aria-label="片段多轨时间线" @click="seekLane">
    <rect x="0" y="0" width="800" height="18" fill="#436b9a"/>
    <g v-for="(track,i) in tracks" :key="i"><rect :x="track.start/duration*800" :y="22+i*8" :width="(track.end-track.start)/duration*800" height="6" :fill="track.muted?'#555':'#9b75d9'"/></g>
    <line v-for="(mark,i) in markers" :key="'m'+i" :x1="mark.time/duration*800" :x2="mark.time/duration*800" y1="0" y2="100" stroke="#f2c15f"/>
    <line :x1="time/duration*800" :x2="time/duration*800" y1="0" y2="100" stroke="white"/>
   </svg>
   <h4>图片/视频叠加轨（最多8条）</h4>
   <div class="row"><select v-model.number="newMaterial"><option :value="0">选择项目图片或视频</option><option v-for="m in visualMaterials" :key="m.id" :value="m.id">{{m.name||m.path}}</option></select><button type="button" @click="addTrack">新增叠加轨</button></div>
   <div v-for="(track,i) in tracks" :key="i" class="track-card">
    <strong>轨{{i+1}} · {{materialName(track.material_id)}}</strong>
    <div class="row"><label v-for="f in trackFields" :key="f.key">{{f.label}}<input type="number" :value="track[f.key]" :step="f.step" :min="f.min" :max="f.max??duration" @change="editTrack(i,f.key,Number($event.target.value))"></label></div>
    <div class="row"><label><input type="checkbox" :checked="track.muted" @change="editTrack(i,'muted',$event.target.checked)">隐藏该轨</label><button type="button" :disabled="i===0" @click="moveTrack(i,-1)">下移一层</button><button type="button" :disabled="i===tracks.length-1" @click="moveTrack(i,1)">上移一层</button><button type="button" @click="removeTrack(i)">删除轨</button></div>
   </div>
   <small>叠加视频仅取画面，短视频循环填充；源起点按素材时长循环取模。多轨最终画面请渲染完整预览。</small>
   <h4>原声音量包络（基于片段音量再乘增益，0–4）</h4>
   <svg viewBox="0 0 800 80" class="envelope" preserveAspectRatio="none" aria-label="音量包络曲线"><polyline :points="envelopePoints" fill="none" stroke="#80dfab" stroke-width="2"/></svg>
   <div class="row"><label>增益<input type="number" v-model.number="newGain" min="0" max="4" step=".05"></label><button type="button" @click="addPoint">播放头添加/更新音量点</button><button type="button" @click="commit({volume_envelope:[]})">清空包络</button></div>
   <div v-for="(point,i) in envelope" :key="i" class="row"><button type="button" @click="seek(point.time)">{{point.time.toFixed(3)}}秒</button><label>增益<input type="number" :value="point.gain" min="0" max="4" step=".05" @change="editPoint(i,Number($event.target.value))"></label><button type="button" @click="commit({volume_envelope:envelope.filter((_,n)=>n!==i)})">删除</button></div>
   <small>预览音量上限100%；超过1的增益以正式混音为准，最终混音不保证无削波。首尾点外保持端点值，点间线性插值。</small>
   <h4>时间线标记（人工打点，不自动分析节拍）</h4>
   <div class="row"><input v-model="markerLabel" maxlength="100" placeholder="如：落剑 / 音乐重拍"><button type="button" @click="addMarker">播放头添加标记</button></div>
   <div v-for="(mark,i) in markers" :key="i" class="row"><button type="button" @click="seek(mark.time)">{{mark.time.toFixed(3)}}秒 · {{mark.label}}</button><button type="button" @click="commit({markers:markers.filter((_,n)=>n!==i)})">删除</button></div>
   <p v-if="error" role="alert">{{error}}</p>
  </template>
 </details>
</template>
<script setup>
import {ref,computed,onBeforeUnmount} from 'vue'
import AudioWaveform from './AudioWaveform.vue'
import {normalizeClipTracks,envelopeAt} from '../utils/clipTracks.js'
const props=defineProps({clip:{type:Object,required:true},materials:{type:Array,default:()=>[]}}),emit=defineEmits(['change'])
const open=ref(false),player=ref(null),time=ref(0),error=ref(''),newMaterial=ref(0),newGain=ref(1),markerLabel=ref('')
const tracks=computed(()=>props.clip.visual_tracks||[]),envelope=computed(()=>props.clip.volume_envelope||[]),markers=computed(()=>props.clip.markers||[])
const duration=computed(()=>Math.max(.001,(Number(props.clip.source_end??props.clip.duration)-Number(props.clip.source_start||0))/Number(props.clip.speed||1)))
const visualMaterials=computed(()=>props.materials.filter(m=>['image','video'].includes(m.type)))
const trackFields=[{key:'start',label:'开始',min:0,step:.01},{key:'end',label:'结束',min:0,step:.01},{key:'source_start',label:'源起点',min:0,max:1e8,step:.01},{key:'x',label:'X',min:0,max:1,step:.01},{key:'y',label:'Y',min:0,max:1,step:.01},{key:'width',label:'宽度',min:.05,max:1,step:.05},{key:'opacity',label:'透明度',min:0,max:1,step:.05}]
function materialName(id){return visualMaterials.value.find(m=>m.id===id)?.name||`素材${id}`}
function commit(changes){try{const value=normalizeClipTracks({...props.clip,...changes},duration.value);emit('change',value);error.value=''}catch(e){error.value=e.message}}
function addTrack(){if(!newMaterial.value)return;commit({visual_tracks:[...tracks.value,{material_id:newMaterial.value,start:0,end:duration.value,source_start:0,x:0,y:0,width:.3,opacity:1,muted:false}]})}
function editTrack(index,key,value){commit({visual_tracks:tracks.value.map((t,i)=>i===index?{...t,[key]:value}:t)})}
function moveTrack(i,delta){const next=[...tracks.value];[next[i],next[i+delta]]=[next[i+delta],next[i]];commit({visual_tracks:next})}
function removeTrack(i){commit({visual_tracks:tracks.value.filter((_,n)=>n!==i)})}
function addPoint(){const points=envelope.value.filter(p=>Math.abs(p.time-time.value)>.001);points.push({time:Number(time.value.toFixed(3)),gain:newGain.value});points.sort((a,b)=>a.time-b.time);commit({volume_envelope:points})}
function editPoint(i,gain){commit({volume_envelope:envelope.value.map((p,n)=>i===n?{...p,gain}:p)})}
function addMarker(){commit({markers:[...markers.value,{time:Number(time.value.toFixed(3)),label:markerLabel.value}]})}
function seek(t){time.value=Math.max(0,Math.min(duration.value,t));if(player.value)player.value.currentTime=Number(props.clip.source_start||0)+time.value*Number(props.clip.speed||1);applyPreview()}
function seekSource(t){seek((t-Number(props.clip.source_start||0))/Number(props.clip.speed||1))}
function seekLane(e){const rect=e.currentTarget.getBoundingClientRect();seek((e.clientX-rect.left)/rect.width*duration.value)}
function applyPreview(){if(player.value){player.value.playbackRate=Number(props.clip.speed||1);player.value.volume=Math.max(0,Math.min(1,Number(props.clip.volume??1)*envelopeAt(envelope.value,time.value)))}}
function onTime(){if(!player.value)return;time.value=Math.max(0,(player.value.currentTime-Number(props.clip.source_start||0))/Number(props.clip.speed||1));if(time.value>=duration.value){player.value.pause();time.value=duration.value}applyPreview()}
const envelopePoints=computed(()=>Array.from({length:81},(_,i)=>`${i*10},${80-envelopeAt(envelope.value,i/80*duration.value)*20}`).join(' '))
onBeforeUnmount(()=>player.value?.pause())
</script>
<style scoped>
.clip-tracks{margin:12px 0;padding:8px;border:1px solid #596480;border-radius:6px}.clip-tracks summary{cursor:pointer}.clip-tracks p,.clip-tracks small{font-size:12px}.clip-tracks small{display:block}.clip-tracks video{max-width:100%;max-height:180px}.row{display:flex;gap:6px;flex-wrap:wrap;align-items:center;margin:6px 0}.row label{font-size:12px;display:flex;gap:4px;align-items:center}.row input[type=number]{width:65px}.track-card{border:1px solid #586078;margin:8px 0;padding:8px}.lane-view{height:100px;width:100%;background:#1a2334;cursor:crosshair}.envelope{height:80px;width:100%;background:#1a2334}
</style>
