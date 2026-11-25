// @vitest-environment jsdom
import { webcrypto } from 'node:crypto';
import { PerspectiveCamera, Vector3 } from 'three';
import { afterEach, expect, it, vi } from 'vitest';
import { snapshotDirectorSource } from './directorSource';
import { applyStudioExecution } from './studioExecutionApply';
import { validateScriptShotPlan } from './scriptShotPlan';
import { createScriptShotSampler } from './scriptShotSampler';
// Load the real 11750-entry catalog during module setup, not inside a timed assertion.
// No catalog mocking, altered assertions or timeout increases.
import './toonflow/assetCatalog';
import './toonflowSets';
afterEach(()=>vi.unstubAllGlobals());

it('authoritative offscreen speech survives confirmed application, JSON roundtrip, exact playback boundaries and camera restoration',async()=>{
 vi.stubGlobal('crypto',webcrypto);
 const scene={id:2,project_id:1,title:'空院',location:'院落',content:'乙在画外说：“别动😀。”',director_source_stale:false,character_roles_set:true,visible_characters:'',characters:'乙'};
 const shots=[{id:9,scene_id:2,order:1,duration:3,camera_movement:'固定',dialogue:'过期缓存',dialogue_ranges:[{dialogue_id:3,group_key:'g',start_rune:0,end_rune:4}],action_timeline:[]}];
 const source=await snapshotDirectorSource(1,scene,shots);
 const authoritative={source_hash:'hash-with-dialogue',source:{scene,shots,dialogues:[{id:3,project_id:1,scene_id:2,text:'别动😀。',character:'乙',speech_type:'offscreen_dialogue'}],dialogue_timeline:{version:1,lines:[{shot_id:9,dialogue_id:3,speaker:'乙',speech_type:'offscreen_dialogue',text:'别动😀。',start:0.5,end:2}],blockers:[]}}};
 const plan={source_hash:authoritative.source_hash,context_revision:'resources',objects:[],bindings:[],shots:[{shot_id:9,duration:3,camera:{position:[1,2,5],target:[0,1,0],fov:45},motion:null,evidence:[scene.content],assumptions:[],blockers:[]}],assumptions:[],blockers:[]};
 const commit=vi.fn();const applied=await applyStudioExecution(plan,source,{readSource:async()=>structuredClone(authoritative),captureContext:async()=>({revision:'resources',assets:[],objects:[]}),fingerprint:()=> 'editor',isCurrent:()=>true,commit});
 expect(commit).toHaveBeenCalledTimes(1);expect(commit.mock.calls[0][0].additions).toEqual([]); // No offscreen actor inserted.
 const restored=validateScriptShotPlan(JSON.parse(JSON.stringify(applied)));
 expect(JSON.parse(restored.diagnostics[1]).speechSource).toEqual({dialogues:authoritative.source.dialogues,dialogue_timeline:authoritative.source.dialogue_timeline});
 const camera=new PerspectiveCamera();camera.position.set(8,9,10);const controls={target:new Vector3(0,0,0),enabled:true,update:vi.fn()};
 const sampler=createScriptShotSampler(restored,{camera,controls,entries:()=>[],environment:null});
 try {
 expect(sampler.sampleAt(0.49).line).toBeNull();expect(sampler.sampleAt(0.5).line?.line).toBe('别动😀。');
 expect(sampler.sampleAt(1.99).line?.speaker).toBe('乙');expect(sampler.sampleAt(2).line).toBeNull();
 expect(camera.position.toArray()).toEqual([1,2,5]);expect(sampler.sampleAt(0.7).line?.line).toBe('别动😀。');
 } finally {sampler.stop();}
 expect(camera.position.toArray()).toEqual([8,9,10]);expect(controls.target.toArray()).toEqual([0,0,0]);
});

it('dialogue source hash change during application prevents publication',async()=>{
 vi.stubGlobal('crypto',webcrypto);
 const scene={id:2,project_id:1,title:'院',location:'院',content:'空院。',director_source_stale:false};const shots=[{id:9,scene_id:2,order:1,duration:3,action_timeline:[]}];
 const source=await snapshotDirectorSource(1,scene,shots);const plan={source_hash:'old',context_revision:'resources',objects:[],bindings:[],shots:[{shot_id:9,duration:3,camera:{position:[1,2,3],target:[0,1,0],fov:45},motion:null,evidence:['空院。'],assumptions:[],blockers:[]}],assumptions:[],blockers:[]};
 let calls=0;const commit=vi.fn();
 await expect(applyStudioExecution(plan,source,{readSource:async()=>({source_hash:++calls===1?'old':'dialogue-edited',source:{scene,shots,dialogues:[],dialogue_timeline:{version:1,lines:[],blockers:[]}}}),captureContext:async()=>({revision:'resources',assets:[],objects:[]}),fingerprint:()=> 'editor',isCurrent:()=>true,commit})).rejects.toThrow('导演来源已变化');
 expect(commit).not.toHaveBeenCalled();
});
