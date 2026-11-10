import {normalizeClipTracks} from './clipTracks.js'
import {snapTime} from './editingTimeline.js'
export function normalizeGlobalTracks(tracks,duration,materials=null){
 const value=normalizeClipTracks({visual_tracks:tracks},duration).visual_tracks
 if(materials&&value.some(t=>!materials.some(m=>Number(m.id)===t.material_id&&['image','video'].includes(m.type))))throw new Error('全局叠加轨素材不可用，当前工程保持不变')
 return value
}
export function dragGlobalTrack(track,mode,delta,duration,targets=[],scale=35,snap=true){
 let next={...track}
 if(mode==='move'){
  const length=track.end-track.start
  const start=snapTime(track.start+delta,targets,scale,snap)
  next.start=Math.max(0,Math.min(duration-length,start));next.end=next.start+length
 }else if(mode==='start'){
  const start=snapTime(track.start+delta,targets,scale,snap)
  next.start=Math.max(Math.max(0,track.start-track.source_start),Math.min(track.end-.05,start))
  next.source_start=track.source_start+next.start-track.start
 }else if(mode==='end')next.end=Math.max(track.start+.05,Math.min(duration,snapTime(track.end+delta,targets,scale,snap)))
 return normalizeGlobalTracks([next],duration)[0]
}
