import test from 'node:test'
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import {cachedMedia,previewSourceKey,previewFPS,rememberMedia,timecodeAt} from '../src/utils/previewCache.js'
function memoryStorage(){const data=new Map();return {getItem:k=>data.has(k)?data.get(k):null,setItem:(k,v)=>data.set(k,String(v)),removeItem:k=>data.delete(k)}}
test('source-keyed media cache persists measured fps and bounds itself',()=>{
 const storage=memoryStorage(),scene={scene_id:7},material={material_id:3,scene_id:null}
 assert.equal(previewSourceKey(scene),'scene:7');assert.equal(previewSourceKey(material),'material:3')
 assert.equal(cachedMedia(scene,storage),null)
 rememberMedia(scene,{duration:4.5,fps:29.97,width:1920,height:1080,proxy_ready:true},storage)
 assert.equal(cachedMedia(scene,storage).fps,29.97);assert.equal(previewFPS(scene,storage),29.97)
 assert.equal(previewFPS({scene_id:99},storage),24,'unknown source must fall back explicitly')
 rememberMedia(scene,{duration:0},storage);assert.deepEqual(cachedMedia(scene,storage),{duration:4.5,fps:29.97,width:1920,height:1080,proxy_ready:true,updated_at:cachedMedia(scene,storage).updated_at})
 for(let i=0;i<200;i++)rememberMedia({scene_id:1000+i},{duration:1,fps:24},storage)
 const keys=Object.keys(JSON.parse(storage.getItem('project-editor-preview-cache-v1')))
 assert.equal(keys.length,150,'cache must stay bounded')
})
test('timecode uses frame rate and never renders negative frames',()=>{
 assert.equal(timecodeAt(0,24),'00:00:00:00');assert.equal(timecodeAt(1.5,24),'00:00:01:12')
 assert.equal(timecodeAt(61.25,25),'00:01:01:06');assert.equal(timecodeAt(-5,24),'00:00:00:00')
 assert.equal(timecodeAt(0.5,29.97).startsWith('00:00:00:'),true)
})
test('editor steps frames from measured fps and exposes preflight/proxy controls',()=>{
 const source=readFileSync(new URL('../src/views/ProjectEditor.vue',import.meta.url),'utf8')
 assert.match(source,/function stepPreview\(direction\)\{const video=assembledPreview\.value;const step=1\/previewFrameRate\.value/)
 assert.match(source,/previewFrameRate/);assert.match(source,/timecodeAt\(finalCutTime\.value/)
 assert.match(source,/api\.sceneMediaInfo/);assert.match(source,/api\.createSceneProxy/)
 assert.match(source,/api\.preflightAudioMerge/);assert.match(source,/exportPayload\(\)/)
 assert.match(source,/导出前一致性检查/);assert.match(source,/代理仅用于预览/)
})