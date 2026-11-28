import { readFileSync, existsSync } from 'node:fs';
import { resolve } from 'node:path';
import { execFileSync } from 'node:child_process';
import * as THREE from 'three';
import { expect, it, vi } from 'vitest';
import { validateScriptShotPlan } from './scriptShotPlan';
import { diagnosePosedSubjects } from './previsPosedOcclusion';
import { createScriptShotSampler } from './scriptShotSampler';
import { sessionEvents, setSessionEvents } from './previsEvents';
import type { CoverageCandidate } from './previsCoverage';
const frontend=resolve(existsSync('src/studio/scene3d')?'.':'frontend');
const stage=readFileSync(resolve(frontend,'src/studio/scene3d/Scene3DStage.tsx'),'utf8');
const marker='plan={scriptShots} diagnosePosed={', start=stage.indexOf(marker)+marker.length, end=stage.indexOf('} capture={() => {',start);
if(start<marker.length || end<start) throw Error('Missing production D3 callback');
const expression=stage.slice(start,end).replace("import('./previsPosedOcclusion')",'Promise.resolve(diagnosisModule)').replace("import('./scriptShotSampler')",'Promise.resolve(samplerModule)');
const code=execFileSync(process.execPath,['-e',"process.stdout.write(require('esbuild').transformSync(require('fs').readFileSync(0,'utf8'),{loader:'ts'}).code)"],{cwd:frontend,input:`const callback=${expression};`,encoding:'utf8'});
function fixture() {
  const object=new THREE.Mesh(new THREE.BoxGeometry(),new THREE.MeshBasicMaterial()), scene=new THREE.Scene(); scene.add(object);
  const entries=[{id:'p',name:'p',object}], camera=new THREE.PerspectiveCamera(45,1,.1,100); camera.position.set(9,3,12);
  const controls={target:new THREE.Vector3(),enabled:true,enableDamping:true,update:vi.fn()};
  const plan=validateScriptShotPlan({version:1,sceneId:'s',heading:'',location:'',sourceFingerprint:'src',sceneFingerprint:'rev',diagnostics:[],action:[],shots:[{id:'s',order:1,start:0,end:2,dialogue:[],camera:{position:[0,3,8],target:[0,0,0],fov:50},motion:{targetId:'p',targetName:'p',start:{x:0,z:0},end:{x:1,z:0},duration:2}}]});
  const make=vi.fn(createScriptShotSampler), scope:any={sessionEvents,sceneRef:{current:scene},sceneEpochRef:{current:0},executionUnavailableRef:{current:false},designShotPlanRef:{current:plan},fingerprintRef:{current:()=>({full:()=> 'rev'})},cameraRef:{current:camera},controlsRef:{current:controls},objectsRef:{current:entries},environmentRef:{current:null},motionBaselineRef:{current:new Map()},diagnosisModule:{diagnosePosedSubjects},samplerModule:{createScriptShotSampler:make}};
  const call=new Function('scope',`with(scope){${code};return callback;}`)(scope);
  const candidates=[{subjects:['p'],camera:{position:[0,3,8],target:[0,0,0],fov:50}}] as CoverageCandidate[];
  return {scope,make,object,camera,controls,plan,run:()=>call('s',candidates,'rev',plan)};
}
it('runs production inline callback with the real shared sampler and restores the viewport', async()=>{
  const f=fixture(), before=f.camera.position.toArray(); const result=await f.run();
  expect(result[0].frames).toHaveLength(3); expect(result[0].frames.map((f:any)=>f.rays)).toEqual([9,9,9]);
  expect(f.make).toHaveBeenCalledTimes(1); expect(f.object.position.toArray()).toEqual([0,0,0]); expect(f.camera.position.toArray()).toEqual(before); expect(f.controls.enabled).toBe(true); expect(f.controls.enableDamping).toBe(true); expect(f.scope.motionBaselineRef.current.size).toBe(0);
});
it.each(['plan','content','epoch','events','busy'])('rejects %s changed across the lazy import before sampling',async mode=>{
  const f=fixture(), promise=f.run();
  if(mode==='plan') f.scope.designShotPlanRef.current=structuredClone(f.plan);
  if(mode==='content') f.plan.heading='changed';
  if(mode==='epoch') f.scope.sceneEpochRef.current++;
  if(mode==='events') setSessionEvents(f.scope.sceneRef.current,[{kind:'fire',objectId:'p',start:0,end:1,offset:[0,0,0],rotation:[0,0,0],velocity:[0,0,0],scale:1}]);
  if(mode==='busy') f.scope.executionUnavailableRef.current=true;
  await expect(promise).rejects.toThrow('来源已失效'); expect(f.make).not.toHaveBeenCalled(); expect(f.object.position.x).toBe(0);
});
it('restores the production sampler on a mid-session diagnosis error',async()=>{
  const f=fixture(), before=f.camera.position.toArray();
  f.scope.samplerModule.createScriptShotSampler=(...args: Parameters<typeof createScriptShotSampler>)=>{const sampler=createScriptShotSampler(...args);return {...sampler,sampleAt:(seconds:number)=>{const sample=sampler.sampleAt(seconds);if(seconds>0)throw Error('probe failure');return sample;}};};
  await expect(f.run()).rejects.toThrow('probe failure'); expect(f.object.position.x).toBe(0);expect(f.camera.position.toArray()).toEqual(before);expect(f.controls.enabled).toBe(true);
});
