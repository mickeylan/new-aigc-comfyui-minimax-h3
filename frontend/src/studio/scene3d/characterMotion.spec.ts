import { describe, it, expect } from 'vitest';
import * as THREE from 'three';
import { createCharacterMotionRuntime, retargetSkeletalClip, validateCharacterMotionSettings } from './characterMotion';
import { attachAnimationClip } from './animationPreview';
function rig() { const root = new THREE.Group(), bone = new THREE.Bone(); bone.name='rootBone'; root.add(bone); return {root,bone}; }
function clip(bone: THREE.Bone, name: string, x: number) { return new THREE.AnimationClip(name, 2, [new THREE.VectorKeyframeTrack(`${bone.uuid}.position`,[0,2],[x,0,0,x,0,0])]); }
const ref = (index:number,name:string) => ({index,name,duration:2});
describe('character motion',()=>{
  it('validates settings without accepting invalid bounds',()=>{ expect(()=>validateCharacterMotionSettings({version:1,blend:{animation:ref(0,'a'),weight:2,fadeSeconds:0}})).toThrow(); expect(validateCharacterMotionSettings({version:1})).toEqual({version:1}); });
  it('absolute blend fade is order independent and restores editing pose',()=>{
    const {root,bone}=rig(); bone.position.x=3; root.animations=[clip(bone,'a',0),clip(bone,'b',10)];
    const runtime=createCharacterMotionRuntime(root,ref(0,'a'),{version:1,blend:{animation:ref(1,'b'),weight:0.8,fadeSeconds:2}});
    runtime.sampleAt(1,()=>0); expect(bone.position.x).toBeCloseTo(4);
    runtime.sampleAt(5,()=>0); expect(bone.position.x).toBeCloseTo(8);
    runtime.sampleAt(1,()=>0); expect(bone.position.x).toBeCloseTo(4);
    runtime.sampleAt(0,()=>0); expect(bone.position.x).toBeCloseTo(0);
    runtime.stop(); expect(bone.position.x).toBe(3); runtime.stop();
  });
  it('rejects stale references, duplicate bone names and scale animation',()=>{
    const {root,bone}=rig(); root.animations=[clip(bone,'a',0)];
    expect(()=>createCharacterMotionRuntime(root,ref(0,'stale'))).toThrow();
    const other=new THREE.Bone(); other.name=bone.name; root.add(other); expect(()=>createCharacterMotionRuntime(root)).toThrow(); root.remove(other);
    root.animations=[new THREE.AnimationClip('s',2,[new THREE.VectorKeyframeTrack(`${bone.uuid}.scale`,[0,2],[1,1,1,2,2,2])])]; expect(()=>createCharacterMotionRuntime(root,ref(0,'s'))).toThrow(/scale/);
  });
  function legs() {
    const root=new THREE.Group();
    for(const side of ['left','right']) { const h=new THREE.Bone(),k=new THREE.Bone(),a=new THREE.Bone(); h.name=side+'Hip'; k.name=side+'Knee'; a.name=side+'Ankle'; h.position.set(side==='left'?-1:1,2,0); k.position.set(0,-0.8,0.6); a.position.set(0,-0.8,-0.6); root.add(h); h.add(k); k.add(a); }
    return root;
  }
  const feet={left:{hip:'leftHip',knee:'leftKnee',ankle:'leftAnkle'},right:{hip:'rightHip',knee:'rightKnee',ankle:'rightAnkle'},soleOffset:0,maxCorrection:1};
  it('solves both legs in world space and aligns sole to terrain normal',()=>{
    const root=legs(); root.position.y=0.1; const runtime=createCharacterMotionRuntime(root,undefined,{version:1,feet});
    const terrain=(x:number)=>0.3+x*0.1;
    runtime.sampleAt(0,terrain);
    for(const side of ['left','right']) {const a=root.getObjectByName(side+'Ankle')!; const p=a.getWorldPosition(new THREE.Vector3()); expect(p.y).toBeCloseTo(terrain(p.x),5); const up=new THREE.Vector3(0,1,0).applyQuaternion(a.getWorldQuaternion(new THREE.Quaternion())); expect(up.dot(new THREE.Vector3(-0.1,1,0).normalize())).toBeCloseTo(1,5); }
    const first=root.getObjectByName('leftKnee')!.quaternion.clone(); runtime.sampleAt(3,terrain); expect(root.getObjectByName('leftKnee')!.quaternion.angleTo(first)).toBeLessThan(1e-6);
    runtime.stop(); expect(root.getObjectByName('leftHip')!.quaternion.w).toBe(1);
  });
  it('fails atomically on unreachable terrain',()=>{ const root=legs(); const runtime=createCharacterMotionRuntime(root,undefined,{version:1,feet}); expect(()=>runtime.sampleAt(0,()=>-10)).toThrow(/maxCorrection/); expect(root.getObjectByName('leftHip')!.quaternion.w).toBe(1); runtime.stop(); });
  it('retargets calibrated orientation and proportional translation without mutating rigs',()=>{
    const s=rig(),t=rig(); s.bone.position.y=1; t.bone.position.y=2; t.bone.rotation.z=Math.PI/2;
    const q=t.bone.quaternion.clone(); const sourceClip=new THREE.AnimationClip('source',1,[new THREE.VectorKeyframeTrack(`${s.bone.uuid}.position`,[0,1],[0,1,0,1,1,0]),new THREE.QuaternionKeyframeTrack(`${s.bone.uuid}.quaternion`,[0,1],[0,0,0,1,0,Math.sin(Math.PI/4),0,Math.cos(Math.PI/4)])]);
    const baked=retargetSkeletalClip(s.root,t.root,sourceClip,{rootBone:'rootBone'},'baked'); expect(baked.tracks[0].times.length).toBe(31); expect(baked.tracks[0].values.at(-3)).toBeCloseTo(2); expect(t.bone.quaternion.angleTo(q)).toBeLessThan(1e-6); expect(s.bone.position.x).toBe(0); expect(attachAnimationClip(t.root,baked)).toHaveLength(1);
    const finalQ=new THREE.Quaternion().fromArray(baked.tracks[1].values,baked.tracks[1].values.length-4); const expected=new THREE.Quaternion().setFromAxisAngle(new THREE.Vector3(0,1,0),Math.PI/2).multiply(q); expect(finalQ.angleTo(expected)).toBeLessThan(0.001);
  });
  it('samples clip boundaries exactly and rejects nonfinite terrain atomically',()=>{
    const {root,bone}=rig(); root.animations=[new THREE.AnimationClip('ramp',2,[new THREE.VectorKeyframeTrack(`${bone.uuid}.position`,[0,2],[0,0,0,2,0,0])])];
    const runtime=createCharacterMotionRuntime(root,ref(0,'ramp')); runtime.sampleAt(1,()=>0); expect(bone.position.x).toBeCloseTo(1); runtime.sampleAt(2,()=>0); expect(bone.position.x).toBeCloseTo(0); runtime.sampleAt(1,()=>0); expect(bone.position.x).toBeCloseTo(1); runtime.stop();
    const legRoot=legs(), ik=createCharacterMotionRuntime(legRoot,undefined,{version:1,feet}); expect(()=>ik.sampleAt(0,()=>NaN)).toThrow(/nonfinite/); expect(legRoot.getObjectByName('leftHip')!.quaternion.w).toBe(1); ik.stop();
  });
  it('never applies secondary root/non-bone channels even with zero blend weight',()=>{
    const {root,bone}=rig(); root.position.set(4,0,0);
    root.animations=[clip(bone,'a',2),new THREE.AnimationClip('invalid-root',2,[new THREE.VectorKeyframeTrack('.position',[0,2],[8,0,0,10,0,0]),new THREE.VectorKeyframeTrack(`${bone.uuid}.position`,[0,2],[9,0,0,9,0,0])])];
    expect(()=>createCharacterMotionRuntime(root,ref(0,'a'),{version:1,blend:{animation:ref(1,'invalid-root'),weight:0,fadeSeconds:0}})).toThrow();
    expect(root.position.toArray()).toEqual([4,0,0]); expect(bone.position.x).toBe(0);
    root.animations[1]=clip(bone,'b',10);
    const runtime=createCharacterMotionRuntime(root,ref(0,'a'),{version:1,blend:{animation:ref(1,'b'),weight:0,fadeSeconds:0}});
    runtime.sampleAt(1,()=>0); expect(bone.position.x).toBe(2); expect(root.position.x).toBe(4); runtime.stop();
  });
  it('fits fully extended standing legs while ignoring decorative mesh-only scales',()=>{
    const root=legs(); for(const side of ['left','right']) { root.getObjectByName(side+'Knee')!.position.set(0,-1,0); root.getObjectByName(side+'Ankle')!.position.set(0,-1,0); }
    const decoration=new THREE.Mesh(new THREE.BoxGeometry(),new THREE.MeshBasicMaterial()); decoration.scale.set(2,3,4); root.add(decoration);
    const runtime=createCharacterMotionRuntime(root,undefined,{version:1,feet}); runtime.sampleAt(0,()=>0);
    expect(root.getObjectByName('leftAnkle')!.getWorldPosition(new THREE.Vector3()).y).toBeCloseTo(0,6);
    runtime.sampleAt(1,()=>0.1); expect(root.getObjectByName('rightAnkle')!.getWorldPosition(new THREE.Vector3()).y).toBeCloseTo(0.1,6);
    runtime.stop(); expect(root.getObjectByName('leftKnee')!.position.toArray()).toEqual([0,-1,0]); decoration.geometry.dispose(); (decoration.material as THREE.Material).dispose();
  });
  it('rejects missing mappings and excessive bake allocations',()=>{const s=rig(),t=rig(); expect(()=>retargetSkeletalClip(s.root,t.root,clip(s.bone,'a',0),{},'b')).toThrow(); const huge=clip(s.bone,'a',0); huge.duration=10000; expect(()=>retargetSkeletalClip(s.root,t.root,huge,{rootBone:'rootBone'},'b')).toThrow(/budget/);});
});
