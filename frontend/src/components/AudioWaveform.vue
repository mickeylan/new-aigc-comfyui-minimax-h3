<template>
 <div class="waveform">
  <button type="button" :disabled="loading||!src" @click="load">{{loading?'解码中…':'读取真实波形'}}</button>
  <span v-if="duration"> 源媒体 {{duration.toFixed(2)}} 秒</span>
  <svg v-if="peaks.length" viewBox="0 0 800 70" preserveAspectRatio="none" role="img" aria-label="真实音频峰值波形，点击定位" @click="seek">
   <line v-for="(p,i) in peaks" :key="i" :x1="i*800/peaks.length" :x2="i*800/peaks.length" :y1="35-p*32" :y2="35+p*32" stroke="#8ad4df" stroke-width="1.5" />
   <rect :x="Math.max(0,start)/duration*800" y="0" :width="Math.max(0,Math.min(end??duration,duration)-start)/duration*800" height="70" fill="#ffcf6822"/>
  </svg>
  <small v-if="error" role="status">{{error}}</small>
  <small v-else>按需读取，最多32MB；浏览器不支持的媒体会明确提示。波形不是语音逐字对齐。</small>
 </div>
</template>
<script setup>
import {ref,watch,onBeforeUnmount} from 'vue'
import {waveformPeaks} from '../utils/clipTracks.js'
const props=defineProps({src:String,start:{type:Number,default:0},end:{type:Number,default:null}})
const emit=defineEmits(['seek'])
const peaks=ref([]),duration=ref(0),loading=ref(false),error=ref('')
let controller=null,context=null,epoch=0
async function load(){
 if(!props.src||loading.value)return
 let localContext=null
 const token=++epoch;controller=new AbortController();loading.value=true;error.value=''
 try{
  const response=await fetch(props.src,{signal:controller.signal});if(!response.ok)throw new Error(`读取媒体失败 HTTP ${response.status}`)
  const limit=32*1024*1024;if(Number(response.headers.get('content-length'))>limit)throw new Error('媒体超过32MB，请使用较短代理文件查看波形')
  const reader=response.body.getReader(),chunks=[];let size=0
  while(true){const {done,value}=await reader.read();if(done)break;size+=value.length;if(size>limit){await reader.cancel();throw new Error('媒体超过32MB，已停止读取')};chunks.push(value)}
  if(token!==epoch)return
  const bytes=new Uint8Array(size);let offset=0;for(const chunk of chunks){bytes.set(chunk,offset);offset+=chunk.length}
  const Ctx=window.AudioContext||window.webkitAudioContext;if(!Ctx)throw new Error('浏览器不支持音频解码')
  localContext=new Ctx();context=localContext;const decoded=await localContext.decodeAudioData(bytes.buffer)
  if(token!==epoch)return
  duration.value=decoded.duration
  peaks.value=waveformPeaks(Array.from({length:decoded.numberOfChannels},(_,i)=>decoded.getChannelData(i)))
  if(!peaks.value.some(p=>p>0))error.value='检测到静音波形'
 }catch(e){if(token===epoch&&e.name!=='AbortError')error.value=e.message||'媒体没有可解码音轨'}finally{await localContext?.close().catch(()=>{});if(context===localContext)context=null;if(token===epoch)loading.value=false}
}
function seek(event){const rect=event.currentTarget.getBoundingClientRect();emit('seek',Math.min(duration.value,Math.max(0,(event.clientX-rect.left)/rect.width*duration.value)))}
function reset(){epoch++;controller?.abort();context?.close().catch(()=>{});context=null;loading.value=false;peaks.value=[];duration.value=0;error.value=''}
watch(()=>props.src,reset);onBeforeUnmount(reset)
</script>
<style scoped>
.waveform{padding:8px;border:1px solid #425066;border-radius:5px;margin:6px 0;max-width:100%}.waveform svg{display:block;width:100%;height:70px;background:#131e2c;cursor:crosshair}.waveform small{display:block;font-size:11px;opacity:.8}
</style>
