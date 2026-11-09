<template>
 <details class="transform-editor" @toggle="opened=$event.target.open">
  <summary>画面变换 / 裁切 / 关键帧 {{clip.transform?'（已启用）':''}}</summary>
  <template v-if="opened">
   <p>非破坏式：边缘裁切 → 适配画幅 → 旋转 → 缩放/位移。位置为画幅比例，关键帧时间为裁剪变速后的片段秒数。输出画幅沿用成片设置。</p>
   <div ref="stage" class="transform-stage" :style="{aspectRatio:aspect}">
    <div class="pose" :style="poseStyle"><div class="rotation-clip"><div class="rotation-plane" :style="{transform:`rotate(${pose.rotation}deg)`}"><div class="fit-clip"><div class="fitted" :style="fittedStyle"><video ref="video" :src="clip.video_url" muted preload="metadata" :style="videoStyle" @loadedmetadata="metadata" @timeupdate="onTime" @ended="playing=false"></video></div></div></div></div></div>
   </div>
   <div class="controls"><button type="button" @click="togglePlayback">{{playing?'暂停':'播放变换预览'}}</button><input aria-label="变换预览时间" type="range" min="0" :max="duration" step="0.01" :value="time" @input="seek(Number($event.target.value))"><span>{{time.toFixed(2)}} / {{duration.toFixed(2)}}秒</span></div>
   <div class="controls">
    <label>适配<select :value="value.fit" @change="setField('fit',$event.target.value)"><option value="contain">完整显示（留黑边）</option><option value="cover">填满画面（居中裁切）</option></select></label>
    <label v-for="field in fields" :key="field.key">{{field.label}}<input type="number" :min="field.min" :max="field.max" :step="field.step" :value="value[field.key]" @change="setField(field.key,Number($event.target.value))"></label>
   </div>
   <div class="controls"><button type="button" @click="addKeyframe">在预览时间添加/更新关键帧</button><button type="button" @click="clearKeys">清空动画（保留静态变换）</button><button type="button" @click="emit('change',null)">恢复原始画面</button></div>
   <p v-if="value.keyframes.length">已启用线性插值；首帧之前、末帧之后保持端点值。修改静态参数不会覆盖已有关键帧，请点击更新关键帧。</p>
   <div v-for="(key,index) in value.keyframes" :key="index" class="controls"><button type="button" @click="seek(key.time)">{{key.time.toFixed(2)}}秒</button><span>缩放{{key.zoom}} · X{{key.x}} Y{{key.y}} · 旋转{{key.rotation}}°</span><button type="button" @click="removeKey(index)">删除</button></div>
   <p v-if="error" role="alert">{{error}}</p>
   <small>这是几何预览，不包含字幕、叠加、转场或最终色彩；正式效果请渲染完整预览。</small>
  </template>
 </details>
</template>
<script setup>
import { ref, computed, onBeforeUnmount, watch, nextTick } from 'vue'
import { defaultTransform, normalizeTransform, transformAt } from '../utils/clipTransform.js'
const props=defineProps({clip:{type:Object,required:true},aspect:{type:Number,default:16/9}})
const emit=defineEmits(['change'])
const opened=ref(false),video=ref(null),time=ref(0),playing=ref(false),error=ref(''),sourceAspect=ref(16/9)
const duration=computed(()=>Math.max(0,(Number(props.clip.source_end??props.clip.duration)-Number(props.clip.source_start||0))/Number(props.clip.speed||1)))
const value=computed(()=>props.clip.transform||defaultTransform())
const fields=[{key:'zoom',label:'缩放',min:.25,max:4,step:.05},{key:'rotation',label:'旋转°',min:-360,max:360,step:1},{key:'x',label:'X',min:-1,max:1,step:.01},{key:'y',label:'Y',min:-1,max:1,step:.01},...['left','right','top','bottom'].map(k=>({key:'crop_'+k,label:({left:'左裁切',right:'右裁切',top:'上裁切',bottom:'下裁切'})[k],min:0,max:.95,step:.01}))]
const pose=computed(()=>transformAt(value.value,time.value))
const poseStyle=computed(()=>({transform:`translate(${pose.value.x*100}%,${pose.value.y*100}%) scale(${pose.value.zoom})`,overflow:'visible'}))
const fittedStyle=computed(()=>{
 const tr=value.value,croppedAspect=sourceAspect.value*(1-tr.crop_left-tr.crop_right)/(1-tr.crop_top-tr.crop_bottom)
 const contain=tr.fit==='contain',wide=croppedAspect>props.aspect
 const width=(wide===contain)?100:100*croppedAspect/props.aspect,height=(wide===contain)?100*props.aspect/croppedAspect:100
 return {width:width+'%',height:height+'%',left:(100-width)/2+'%',top:(100-height)/2+'%'}
})
const videoStyle=computed(()=>({width:100/(1-value.value.crop_left-value.value.crop_right)+'%',height:100/(1-value.value.crop_top-value.value.crop_bottom)+'%',left:-100*value.value.crop_left/(1-value.value.crop_left-value.value.crop_right)+'%',top:-100*value.value.crop_top/(1-value.value.crop_top-value.value.crop_bottom)+'%'}))
function commit(tr){try{normalizeTransform(tr,duration.value);emit('change',tr);error.value=''}catch(e){error.value=e.message}}
function setField(key,v){commit({...value.value,[key]:v})}
function addKeyframe(){const keys=value.value.keyframes.filter(k=>Math.abs(k.time-time.value)>.01);keys.push({time:Number(time.value.toFixed(3)),zoom:value.value.zoom,x:value.value.x,y:value.value.y,rotation:value.value.rotation});keys.sort((a,b)=>a.time-b.time);commit({...value.value,keyframes:keys})}
function removeKey(index){commit({...value.value,keyframes:value.value.keyframes.filter((_,i)=>i!==index)})}
function clearKeys(){commit({...value.value,keyframes:[]})}
function metadata(){sourceAspect.value=video.value.videoWidth/video.value.videoHeight||16/9;seek(time.value)}
function seek(t){time.value=Math.min(duration.value,Math.max(0,t));if(video.value)video.value.currentTime=Number(props.clip.source_start||0)+time.value*Number(props.clip.speed||1)}
function onTime(){if(!video.value)return;time.value=(video.value.currentTime-Number(props.clip.source_start||0))/Number(props.clip.speed||1);if(time.value>=duration.value){video.value.pause();playing.value=false;time.value=duration.value}}
async function togglePlayback(){if(!video.value)return;if(playing.value){video.value.pause();playing.value=false;return}if(time.value>=duration.value)seek(0);video.value.playbackRate=Number(props.clip.speed||1);try{await video.value.play();playing.value=true}catch(e){error.value=e.message}}
let animationFrame=0
function animate(){onTime();if(playing.value)animationFrame=requestAnimationFrame(animate)}
watch(playing,active=>{cancelAnimationFrame(animationFrame);if(active)animationFrame=requestAnimationFrame(animate)})
watch(()=>props.clip.transform,()=>{video.value?.pause();playing.value=false},{deep:true})
watch(opened,async open=>{if(!open){playing.value=false;return}await nextTick();seek(time.value)})
onBeforeUnmount(()=>{cancelAnimationFrame(animationFrame);video.value?.pause()})
</script>
<style scoped>
.transform-editor{margin:12px 0;border:1px solid #637088;border-radius:6px;padding:8px}.transform-editor p,.transform-editor small{font-size:12px}.controls{display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin:8px 0}.controls label{display:flex;gap:4px;align-items:center;font-size:12px}.controls input[type=number]{width:70px}.transform-stage{background:black;max-width:460px;position:relative;overflow:hidden}.pose{position:absolute;inset:0;transform-origin:center}.rotation-clip,.rotation-plane,.fit-clip{position:absolute;inset:0;transform-origin:center}.rotation-clip,.fit-clip{overflow:hidden}.fitted{position:absolute;overflow:hidden;transform-origin:center}.fitted video{position:absolute;max-width:none;object-fit:fill}summary{cursor:pointer}
</style>
