import * as THREE from 'three';
import { expect, it, vi } from 'vitest';
import { diagnosePosedSubjects } from './previsPosedOcclusion';
import type { ScriptShot } from './scriptShotPlan';
import type { CoverageCandidate } from './previsCoverage';
const shot = { start: 2, end: 4, execution: { actors: [{ targetId: 'back' }, { targetId: 'front' }] } } as ScriptShot;
const candidate = { subjects: ['back', 'front'], camera: { position: [0,0,10], target: [0,0,0], fov: 50 } } as CoverageCandidate;
function fixture() {
  const back = new THREE.Mesh(new THREE.BoxGeometry(1,1,1)), front = new THREE.Mesh(new THREE.BoxGeometry(2,2,1)); front.position.z = 4;
  const entries = [{ id:'back', name:'back', object:back }, { id:'front', name:'front', object:front }];
  const sampleAt = vi.fn(), stop = vi.fn();
  return { back, front, entries, sampleAt, stop, make: () => ({ sampleAt, stop }) };
}
it('measures same-time subject obstruction and absolute half-open sampling with guaranteed stop', () => {
  const f = fixture(); const result = diagnosePosedSubjects(shot, [candidate], f.entries, f.make)[0];
  expect(f.sampleAt.mock.calls.map(c => c[0])).toEqual([2,3,4-1/24]); expect(f.stop).toHaveBeenCalledTimes(1);
  expect(result.visuallyApproved).toBe(false); expect(result.frames).toHaveLength(3);
  expect(result.frames.every(f => f.rays === 18 && f.blockedRays === 9 && f.blockers.join() === 'front')).toBe(true);
});
it('samples moving subjects simultaneously instead of unioning independent endpoints', () => {
  const f = fixture(); f.sampleAt.mockImplementation(t => { f.front.position.x = t === 3 ? 8 : 0; });
  const frames = diagnosePosedSubjects(shot, [candidate], f.entries, f.make)[0].frames;
  expect(frames.map(f => f.blockedRays)).toEqual([9,0,9]);
});
it('uses actual skinned vertices rather than editing bounding boxes', () => {
  const f = fixture(), geometry = new THREE.BoxGeometry(2,2,1), count = geometry.attributes.position.count;
  geometry.setAttribute('skinIndex', new THREE.Uint16BufferAttribute(new Uint16Array(count*4),4));
  const weights = new Float32Array(count*4); for(let i=0;i<count;i++) weights[i*4]=1;
  geometry.setAttribute('skinWeight',new THREE.Float32BufferAttribute(weights,4));
  const mesh = new THREE.SkinnedMesh(geometry), bone = new THREE.Bone(); mesh.add(bone); mesh.bind(new THREE.Skeleton([bone])); mesh.position.z=4;
  f.entries[1].object = mesh;
  f.sampleAt.mockImplementation(t => { bone.position.x = t===3 ? 8 : 0; }); f.stop.mockImplementation(() => { bone.position.x=0; });
  const result = diagnosePosedSubjects(shot,[candidate],f.entries,f.make);
  expect(result[0].frames.map(f=>f.blockedRays)).toEqual([9,0,9]); expect(bone.position.x).toBe(0);
  expect(mesh.boundingBox).toBeNull(); expect(geometry.boundingBox).toBeNull();
});
it('restores after sample failure without returning partial results', () => {
  const f = fixture(); f.sampleAt.mockImplementation(t => { f.front.position.x=8; if(t===3) throw Error('sample failed'); }); f.stop.mockImplementation(()=>{f.front.position.x=0;});
  expect(()=>diagnosePosedSubjects(shot,[candidate],f.entries,f.make)).toThrow('sample failed'); expect(f.stop).toHaveBeenCalledTimes(1); expect(f.front.position.x).toBe(0);
});
it('rejects non-finite sampled vertices and still stops', () => {
  const f=fixture(); f.sampleAt.mockImplementation(()=>{f.front.position.x=NaN;});
  expect(()=>diagnosePosedSubjects(shot,[candidate],f.entries,f.make)).toThrow('非有限'); expect(f.stop).toHaveBeenCalledTimes(1);
});
it.each(['duplicate','hidden','instance','budget','custom','missing'])('rejects unsupported %s before starting a session', kind => {
  const f=fixture(), make=vi.fn(f.make);
  if(kind==='duplicate') f.entries.push(f.entries[0]);
  if(kind==='hidden') f.front.visible=false;
  if(kind==='instance') f.entries[1].object=new THREE.InstancedMesh(new THREE.BoxGeometry(),new THREE.MeshBasicMaterial(),2);
  if(kind==='budget') f.front.geometry.setAttribute('position',new THREE.BufferAttribute(new Float32Array(100001*3),3));
  if(kind==='custom') f.front.getVertexPosition=()=>new THREE.Vector3();
  if(kind==='missing') f.entries.pop();
  expect(()=>diagnosePosedSubjects(shot,[candidate],f.entries,make)).toThrow(); expect(make).not.toHaveBeenCalled();
});
it('uses distinct views over the same sampled poses', () => {
  const f=fixture(), side={...candidate,camera:{...candidate.camera,position:[10,0,0] as [number,number,number]}};
  const results=diagnosePosedSubjects(shot,[candidate,side],f.entries,f.make);
  expect(results[0].frames[0].blockedRays).toBe(9); expect(results[1].frames[0].blockedRays).toBe(0); expect(f.sampleAt).toHaveBeenCalledTimes(3);
});
