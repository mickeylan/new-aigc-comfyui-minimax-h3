import { sampleTimeline } from './previsTimeline.js'
import { validatePrevis, clonePrevis } from './scenePrevisualization.js'
export function freezeShotFrame(document,timeMS){
 const error=validatePrevis(document);if(error)throw Error(error)
 if(!Number.isInteger(timeMS)||timeMS<0||timeMS>(document.duration_ms??0))throw Error('选帧时间超出已保存时间轴')
 return sampleTimeline(document,timeMS)
}
export function frameExportSource(item){
 const document=JSON.parse(item.document_json),frame=JSON.parse(item.frame_json)
 if(validatePrevis(document)||validatePrevis(frame))throw Error('绑定帧格式无效')
 const sampled=freezeShotFrame(document,item.time_ms)
 if(JSON.stringify(sampled.objects)!==JSON.stringify(frame.objects)||JSON.stringify(sampled.camera)!==JSON.stringify(frame.camera))throw Error('绑定帧与原始时间轴采样不一致，禁止导出')
 return {document:clonePrevis(document),frame:clonePrevis(frame),time_ms:item.time_ms}
}
export function frameManifest(item,mode,depth,files){return {schema_version:1,kind:'shot-previs-frame',binding_id:item.id,shot_id:item.shot_id,scene_id:item.scene_id,previs_revision:item.previs_revision,time_ms:item.time_ms,frame_hash:item.frame_hash,source_content_hash:item.source_content_hash,shot_hash:item.shot_hash,mode,width:1280,height:720,files,depth,frame:JSON.parse(item.frame_json),asset_hashes:(JSON.parse(item.document_json).models||[]).map(b=>({object_id:b.object_id,asset_hash:b.asset_hash,clip_index:b.clip_index}))}}
