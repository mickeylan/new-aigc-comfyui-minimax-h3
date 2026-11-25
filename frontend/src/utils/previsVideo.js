// Local previs video export: deterministic WebCodecs encoding + minimal WebM (Matroska) muxing. See ADR-0010.
import {exportFrameTimes} from './previsFrameArchive.js'
export const VIDEO_WIDTH=1280,VIDEO_HEIGHT=720,VIDEO_MAX_FRAMES=2880,VIDEO_MAX_BYTES=256*1024*1024
const CODECS=[{name:'VP9',codec:'vp09.00.31.08',webm:'V_VP9'},{name:'VP8',codec:'vp8',webm:'V_VP8'}]
export function videoFrameTimes(from,to,fps,duration){return exportFrameTimes(from,to,fps,duration,VIDEO_MAX_FRAMES)}
export async function pickVideoCodec(Encoder,Frame,fps,{width=VIDEO_WIDTH,height=VIDEO_HEIGHT}={}){
 if(!Number.isSafeInteger(width)||!Number.isSafeInteger(height)||width<2||height<2||width%2||height%2||Math.max(width,height)>1280||width*height>VIDEO_WIDTH*VIDEO_HEIGHT)throw Error('视频尺寸无效或超过1280×720像素预算')
 if(typeof Encoder?.isConfigSupported!=='function'||typeof Frame!=='function')throw Error('当前浏览器不支持WebCodecs视频编码，请改用PNG序列导出')
 for(const c of CODECS){const config={codec:c.codec,width,height,bitrate:8_000_000,framerate:fps,latencyMode:'quality'};try{if((await Encoder.isConfigSupported(config))?.supported)return{...c,config}}catch{/* try next codec */}}
 throw Error('当前浏览器不支持VP9/VP8编码，请改用PNG序列导出')
}
// Timestamps come from the frame index, never the wall clock, so slow rendering cannot drop frames or change speed.
// renderFrame(i,time,{timestamp,duration}) must resolve to a VideoFrame captured while the export pass is on the canvas.
export async function encodePrevisVideo({times,fps,codec,Encoder,renderFrame,isCancelled=()=>false,onProgress=()=>{},maxBytes=VIDEO_MAX_BYTES,tick=()=>new Promise(r=>setTimeout(r,0))}){
 const chunks=[];let bytes=0,failure=null
 const encoder=new Encoder({output:chunk=>{if(failure)return;bytes+=chunk.byteLength;if(bytes>maxBytes){failure=Error('视频超过256MiB，请缩小时间范围或降低FPS');return}const data=new Uint8Array(chunk.byteLength);chunk.copyTo(data);chunks.push({timestamp_ms:Math.round(chunk.timestamp/1000),key:chunk.type==='key',data})},error:e=>{failure??=Error(`视频编码失败：${e?.message||e}`)}})
 try{
  encoder.configure(codec.config)
  const keyEvery=Math.max(1,2*fps),frameUS=1e6/fps
  for(let i=0;i<times.length;i++){
   if(isCancelled())throw Error('导出已取消');if(failure)throw failure
   const frame=await renderFrame(i,times[i],{timestamp:Math.round(i*frameUS),duration:Math.round(frameUS)})
   try{if(isCancelled())throw Error('导出已取消');encoder.encode(frame,{keyFrame:i%keyEvery===0})}finally{frame.close()}
   onProgress(i+1,times.length)
   while(encoder.encodeQueueSize>4&&!failure&&!isCancelled())await tick()
   await tick()
  }
  try{await encoder.flush()}catch(e){throw failure||Error(`视频编码失败：${e?.message||e}`)}
  if(failure)throw failure;if(isCancelled())throw Error('导出已取消')
  if(chunks.length!==times.length)throw Error(`视频编码帧数不完整（${chunks.length}/${times.length}），未下载`)
  return{chunks,duration_ms:times.length*1000/fps}
 }finally{if(encoder.state!=='closed')encoder.close()}
}
const text=new TextEncoder()
function idBytes(id){const out=[];for(let v=id;v>0;v=Math.floor(v/256))out.unshift(v%256);return Uint8Array.from(out)}
function sizeBytes(n){const b=new Uint8Array(8);b[0]=1;for(let i=7,v=n;i>0;i--,v=Math.floor(v/256))b[i]=v%256;return b}
function uintBytes(n,width){const out=[];for(let v=n;v>0;v=Math.floor(v/256))out.unshift(v%256);while(out.length<(width||1))out.unshift(0);return Uint8Array.from(out)}
function el(id,children){const parts=[idBytes(id),null];let size=0;for(const c of children){if(c instanceof Uint8Array){parts.push(c);size+=c.length}else{for(const p of c.parts)parts.push(p);size+=c.size}}parts[1]=sizeBytes(size);return{parts,size:parts[0].length+8+size}}
const uint=(id,n,width)=>el(id,[uintBytes(n,width)]),str=(id,s)=>el(id,[text.encode(s)])
function flt(id,x){const b=new Uint8Array(8);new DataView(b.buffer).setFloat64(0,x);return el(id,[b])}
// Fixed 8-byte positions keep the SeekHead size independent of the offsets it records.
function seekHead(positions){return el(0x114D9B74,positions.map(([target,pos])=>el(0x4DBB,[el(0x53AB,[idBytes(target)]),uint(0x53AC,pos,8)])))}
export function webmBlob({codecID,width=VIDEO_WIDTH,height=VIDEO_HEIGHT,fps,chunks,durationMs,app='ComfyStudio previs'}){
 if(!chunks.length)throw Error('没有可封装的视频帧');if(!chunks[0].key)throw Error('首帧必须是关键帧')
 const header=el(0x1A45DFA3,[uint(0x4286,1),uint(0x42F7,1),uint(0x42F2,4),uint(0x42F3,8),str(0x4282,'webm'),uint(0x4287,4),uint(0x4285,2)])
 const info=el(0x1549A966,[uint(0x2AD7B1,1000000),str(0x4D80,app),str(0x5741,app),flt(0x4489,durationMs)])
 const tracks=el(0x1654AE6B,[el(0xAE,[uint(0xD7,1),uint(0x73C5,1),uint(0x83,1),str(0x86,codecID),uint(0x9C,0),uint(0x23E383,Math.round(1e9/fps)),el(0xE0,[uint(0xB0,width),uint(0xBA,height)])])])
 const clusters=[];let current=null,last=-1
 for(const c of chunks){if(!Number.isInteger(c.timestamp_ms)||c.timestamp_ms<last)throw Error('视频帧时间戳必须递增');last=c.timestamp_ms
  // Relative block time is int16; key frames every 2s keep clusters far below that.
  if(!current||c.key||c.timestamp_ms-current.time>30000){current={time:c.timestamp_ms,key:c.key,blocks:[]};clusters.push(current)}
  const rel=c.timestamp_ms-current.time;current.blocks.push(el(0xA3,[Uint8Array.of(0x81,(rel>>8)&255,rel&255,c.key?0x80:0),c.data]))}
 const clusterEls=clusters.map(c=>el(0x1F43B675,[uint(0xE7,c.time),...c.blocks]))
 const infoPos=seekHead([[0x1549A966,0],[0x1654AE6B,0],[0x1C53BB6B,0]]).size,tracksPos=infoPos+info.size
 let pos=tracksPos+tracks.size;const cuePoints=[]
 clusters.forEach((c,i)=>{if(c.key)cuePoints.push(el(0xBB,[uint(0xB3,c.time),el(0xB7,[uint(0xF7,1),uint(0xF1,pos)])]));pos+=clusterEls[i].size})
 const cues=el(0x1C53BB6B,cuePoints),segment=el(0x18538067,[seekHead([[0x1549A966,infoPos],[0x1654AE6B,tracksPos],[0x1C53BB6B,pos]]),info,tracks,...clusterEls,cues])
 // The budget applies to the complete artifact, including EBML/track/cue overhead.
 // Check the computed size before Blob copies any payload buffers.
 if(header.size+segment.size>VIDEO_MAX_BYTES)throw Error('视频超过256MiB，请缩小时间范围或降低FPS')
 return new Blob([...header.parts,...segment.parts],{type:'video/webm'})
}
export function videoManifest({sceneID,revision,dirty,mode,codec,fps,from_ms,to_ms,times,depth,document}){return{schema_version:1,kind:'previs-video',scene_id:sceneID,saved_revision:revision,unsaved_changes:!!dirty,mode,container:'webm',codec:codec.name,codec_string:codec.codec,width:VIDEO_WIDTH,height:VIDEO_HEIGHT,fps,from_ms,to_ms,frame_count:times.length,times_ms:times,depth,note:'浏览器本地导出，仅供审阅；深度视频为有损编码，精确深度请用PNG序列',document}}
