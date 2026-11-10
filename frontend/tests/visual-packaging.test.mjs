import test from 'node:test'
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import {normalizePackaging,normalizeMask,packagingPresets,transitionOptions} from '../src/utils/visualPackaging.js'
import {normalizeClip,buildEditSequence,splitClip} from '../src/utils/finalCutEditing.js'
import {normalizeGlobalTracks,dragGlobalTrack} from '../src/utils/globalTracks.js'
test('original packaging recipes preserve source, exact title and timing through split/export',()=>{
 for(const preset of packagingPresets){
  const clip=normalizeClip({scene_id:1,duration:4,title:'原文不改',packaging:preset.packaging})
  assert.equal(clip.title,'原文不改');assert.equal(clip.duration,4)
  assert.deepEqual(buildEditSequence([clip])[0].packaging,preset.packaging)
  const [a,b]=splitClip(clip,2);assert.deepEqual(a.packaging,preset.packaging);assert.deepEqual(b.packaging,preset.packaging)
 }
 assert.equal(normalizePackaging(null),null)
})
test('mask survives global move/split and rejects arbitrary filter expressions',()=>{
 const track={material_id:2,start:0,end:4,source_start:0,width:.5,opacity:.75,mask:'ellipse'}
 const normalized=normalizeGlobalTracks([track],8)[0]
 assert.equal(dragGlobalTrack(normalized,'move',2,8).mask,'ellipse')
 const [a,b]=splitClip({scene_id:1,duration:4,visual_tracks:[track]},2)
 assert.equal(a.visual_tracks[0].mask,'ellipse');assert.equal(b.visual_tracks[0].mask,'ellipse')
 for(const mask of ['shell','movie=/tmp/file',{}])assert.throws(()=>normalizeMask(mask))
 assert.throws(()=>normalizePackaging({look:'unknown'}));assert.throws(()=>normalizePackaging({title_animation:'unknown'}))
})
test('extended transitions still enforce final-cut and overlap bounds',()=>{
 for(const [transition] of transitionOptions){
  if(transition==='cut')continue
  const clips=[{scene_id:1,duration:4,transition,transition_duration:.5},{scene_id:2,duration:4}]
  assert.equal(buildEditSequence(clips)[0].transition,transition)
  assert.throws(()=>buildEditSequence([clips[0]]))
 }
})
test('visible packaging and mask selectors are wired to editable/persisted values',()=>{
 const read=p=>readFileSync(new URL(p,import.meta.url),'utf8')
 const source=read('../src/views/ProjectEditor.vue')
 assert.match(source,/<ClipPackagingEditor :model-value="clip.packaging"/)
 assert.match(source,/transitionOptions/)
 for(const file of ['ClipTracksEditor','GlobalVisualTimeline'])assert.match(read(`../src/components/${file}.vue`),/maskOptions/)
 assert.match(read('../src/components/ClipPackagingEditor.vue'),/window.confirm/)
})
