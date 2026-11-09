import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { normalizeClipTracks,envelopeAt,splitClipTracks,waveformPeaks } from '../src/utils/clipTracks.js'
import { splitClip,normalizeClip,buildEditSequence } from '../src/utils/finalCutEditing.js'
const tracks={visual_tracks:[{material_id:3,start:1,end:7,source_start:2,x:0,y:0,width:.5,opacity:1,muted:false}],volume_envelope:[{time:0,gain:0},{time:8,gain:2}],markers:[{time:3,label:'hit'},{time:6,label:'end'}]}
test('split clips rebase source offsets, gain endpoints and beat markers without jumps',()=>{
 const clip=normalizeClip({scene_id:1,duration:8,...tracks})
 const [left,right]=splitClip(clip,4)
 assert.equal(left.visual_tracks[0].end,4);assert.equal(right.visual_tracks[0].start,0);assert.equal(right.visual_tracks[0].end,3);assert.equal(right.visual_tracks[0].source_start,5)
 assert.equal(left.volume_envelope.at(-1).gain,1);assert.equal(right.volume_envelope[0].gain,1)
 assert.equal(right.markers[0].time,2);assert.equal(left.markers[0].time,3)
 assert.deepEqual(buildEditSequence([left,right])[1].visual_tracks,right.visual_tracks)
 assert.throws(()=>normalizeClip({...clip,source_end:2}),'trim may not silently discard track ranges')
})
test('gain interpolation and waveform peaks reflect actual samples in all channels',()=>{
 assert.equal(envelopeAt(tracks.volume_envelope,2),.5);assert.equal(envelopeAt([],2),1)
 assert.deepEqual(waveformPeaks([new Float32Array([0,-.5,1,0]),new Float32Array([0,0,0,.75])],2),[.5,1])
 assert.deepEqual(waveformPeaks([],20),[])
})
test('invalid, excessive, unordered and unavailable track fields reject',()=>{
 for(const clip of [{visual_tracks:[{...tracks.visual_tracks[0],material_id:0}]},{volume_envelope:[{time:1,gain:0},{time:0,gain:1}]},{markers:[{time:9,label:'late'}]},{visual_tracks:Array(9).fill(tracks.visual_tracks[0])}])assert.throws(()=>normalizeClipTracks(clip,8))
})
test('workbench uses real waveform decoding, existing audio layers and persisted clip fields',()=>{
 const read=p=>readFileSync(new URL(p,import.meta.url),'utf8')
 const source=read('../src/views/ProjectEditor.vue'),wave=read('../src/components/AudioWaveform.vue')
 assert.match(source,/<ClipTracksEditor :clip="clip" :materials="materials"/)
 assert.match(source,/<VolumeEnvelopeEditor v-model="audioLayerForm.volume_envelope"/)
 assert.match(wave,/decodeAudioData/);assert.match(wave,/getChannelData/);assert.match(wave,/AbortController/);assert.match(wave,/32\*1024\*1024/)
 const timeline=read('../src/components/FinalCutTimeline.vue');assert.match(timeline,/beat-marker/);assert.match(timeline,/markers.value.map/)
})
