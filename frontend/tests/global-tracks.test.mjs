import test from 'node:test'
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import {normalizeGlobalTracks,dragGlobalTrack} from '../src/utils/globalTracks.js'
import {createEditHistory} from '../src/utils/editingTimeline.js'
const track={material_id:2,start:1,end:5,source_start:3,x:0,y:0,width:.5,opacity:1,muted:false}
test('global tracks move across main cuts and snap without changing source or length',()=>{
 const moved=dragGlobalTrack(track,'move',2.03,10,[3],35,true)
 assert.equal(moved.start,3);assert.equal(moved.end,7);assert.equal(moved.source_start,3)
 assert.equal(dragGlobalTrack(track,'move',2.03,10,[3],35,false).start,3.03)
 const bounded=dragGlobalTrack(track,'move',20,10);assert.equal(bounded.start,6);assert.equal(bounded.end,10)
})
test('edge trimming advances source start while preserving the other edge and bounds',()=>{
 const trimmed=dragGlobalTrack(track,'start',1,10,[],35,false)
 assert.equal(trimmed.source_start,4);assert.equal(trimmed.start,2);assert.equal(trimmed.end,5)
 assert.equal(dragGlobalTrack(track,'start',-20,10,[],35,false).start,0)
 assert.equal(dragGlobalTrack(track,'end',20,10,[],35,false).end,10)
})
test('unknown material and shorter arrangement reject atomically; full engineer history includes tracks',()=>{
 assert.throws(()=>normalizeGlobalTracks([track],4))
 assert.throws(()=>normalizeGlobalTracks([track],10,[{id:2,type:'audio'}]))
 assert.equal(normalizeGlobalTracks([track],10,[{id:2,type:'image'}]).length,1)
 const history=createEditHistory();const initial={clips:[{scene_id:1}],tracks:[]};history.record(initial);history.record({...initial,tracks:[track]})
 assert.deepEqual(history.undo(),initial);assert.deepEqual(history.redo().tracks,[track])
})
test('draft save/restore/export and independent drag editor all carry global tracks',()=>{
 const read=p=>readFileSync(new URL(p,import.meta.url),'utf8')
 const parent=read('../src/views/ProjectEditor.vue'),editor=read('../src/components/GlobalVisualTimeline.vue'),timeline=read('../src/components/FinalCutTimeline.vue')
 assert.match(parent,/v-model="globalVisualTracks"/);assert.match(parent,/visual_tracks:normalizeGlobalTracks/);assert.match(parent,/JSON.parse\(data.draft.visual_tracks/)
 assert.match(parent,/api.saveEditingDraft\(project,episode,revision,clips,global\)/)
 assert.match(editor,/setPointerCapture/);assert.match(editor,/!ev.altKey/);assert.match(editor,/pointercancel/)
 assert.match(timeline,/tracks:props.globalTracks/);assert.match(timeline,/engineering-change/)
})
