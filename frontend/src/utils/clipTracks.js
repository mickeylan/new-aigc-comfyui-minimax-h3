function num(v,min,max){if(v==null||v===''||typeof v==='boolean'||!Number.isFinite(Number(v))||Number(v)<min||Number(v)>max)throw new RangeError('轨道参数或时间无效');return Number(v)}
export function normalizeClipTracks(clip,duration){
 const tracks=clip.visual_tracks??[],points=clip.volume_envelope??[],markers=clip.markers??[]
 if(!Array.isArray(tracks)||!Array.isArray(points)||!Array.isArray(markers)||tracks.length>8||points.length>64||markers.length>100)throw new RangeError('轨道/包络/标记超过上限')
 let last=-1
 return {
  visual_tracks:tracks.map(t=>{const material_id=num(t.material_id,1,Number.MAX_SAFE_INTEGER),start=num(t.start,0,duration),end=num(t.end,0,duration);if(!Number.isInteger(material_id)||end<=start)throw new RangeError('叠加轨范围无效');return {material_id,start,end,source_start:num(t.source_start??0,0,1e8),x:num(t.x??0,0,1),y:num(t.y??0,0,1),width:num(t.width??.25,.05,1),opacity:num(t.opacity??1,0,1),muted:Boolean(t.muted)}}),
  volume_envelope:points.map(p=>{const time=num(p.time,0,duration),gain=num(p.gain,0,4);if(time<=last||(last>=0&&time-last<.001-1e-9))throw new RangeError('音量点须递增且间隔至少1毫秒');last=time;return {time,gain}}),
  markers:markers.map(m=>{if(String(m.label||'').length>100)throw new RangeError('标记文字过长');return {time:num(m.time,0,duration),label:String(m.label||'')}}),
 }
}
export function envelopeAt(points,time){
 if(!points?.length)return 1
 if(time<=points[0].time)return points[0].gain
 const i=points.findIndex(p=>p.time>=time);if(i<0)return points.at(-1).gain
 const a=points[i-1],b=points[i];return a.gain+(b.gain-a.gain)*(time-a.time)/(b.time-a.time)
}
export function splitClipTracks(clip,at,total){
 const left={visual_tracks:[],volume_envelope:[],markers:[]},right={visual_tracks:[],volume_envelope:[],markers:[]}
 for(const t of clip.visual_tracks||[]){
  if(t.start<at)left.visual_tracks.push({...t,end:Math.min(t.end,at)})
  if(t.end>at)right.visual_tracks.push({...t,start:Math.max(t.start,at)-at,end:t.end-at,source_start:t.source_start+Math.max(0,at-t.start)})
 }
 const points=clip.volume_envelope||[]
 if(points.length){left.volume_envelope=[...points.filter(p=>p.time<at),{time:at,gain:envelopeAt(points,at)}];right.volume_envelope=[{time:0,gain:envelopeAt(points,at)},...points.filter(p=>p.time>at&&p.time<=total).map(p=>({...p,time:p.time-at}))]}
 for(const m of clip.markers||[]){if(m.time<at)left.markers.push({...m});else right.markers.push({...m,time:m.time-at})}
 return [left,right]
}
export function waveformPeaks(channels,bins=400){
 if(!channels.length||!channels[0].length)return []
 const length=channels[0].length,step=Math.max(1,Math.ceil(length/bins)),result=[]
 for(let i=0;i<length;i+=step){let peak=0;for(const ch of channels){for(let j=i;j<Math.min(i+step,ch.length);j++)peak=Math.max(peak,Math.abs(ch[j]))}result.push(peak)}
 return result
}
