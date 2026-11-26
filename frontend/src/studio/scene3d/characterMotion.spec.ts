import { describe, it, expect } from 'vitest';
import * as THREE from 'three';
import { createCharacterMotionRuntime, retargetSkeletalClip, validateCharacterMotionSettings } from './characterMotion';
import * as motion from './characterMotion';
import { createRiggedPerson } from './toonflow/riggedPerson';
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

// This verifies a real hand/wrist anchor constraint, not proximity of root nodes.
// It does NOT authorize body collision exceptions or claim a closed-finger grip.
describe('cross-character wrist contact', () => {
  const binding = () => ({ version: 1 as const,
    arm: { shoulder: 'leftUpperArm', elbow: 'leftForeArm', wrist: 'leftHand' },
    targetBone: 'rightHand', sourceAnchor: [0, -.03, .02] as [number, number, number],
    targetAnchor: [.04, 0, .02] as [number, number, number],
    rotation: [0, 0, 0, 1] as [number, number, number, number], pole: [0, 0, 1] as [number, number, number] });
  function pair() {
    const source = createRiggedPerson('#eee'), target = createRiggedPerson('#ccc');
    // The other actor must actually offer the wrist: a hanging arm puts it .556m
    // from our shoulder, beyond this rig's measured .478m reach. No bone stretching.
    target.position.set(.85, 0, 0); target.getObjectByName('rightUpperArm')!.rotation.z = -.6;
    source.updateMatrixWorld(true); target.updateMatrixWorld(true);
    const world = (root: THREE.Object3D, name: string, p: number[]) => root.getObjectByName(name)!.localToWorld(new THREE.Vector3().fromArray(p));
    return { source, target, world };
  }
  const pose = (root: THREE.Object3D) => { const values: number[][] = []; root.traverse(n => values.push([...n.position, ...n.quaternion, ...n.scale])); return values; };
  it('locks the hand anchor onto the other actual wrist while preserving bone lengths and target pose', () => {
    const { source, target, world } = pair(), b = binding(), targetBefore = pose(target);
    const lengths = ['leftForeArm', 'leftHand'].map(n => source.getObjectByName(n)!.position.length());
    const result = motion.solveWristContact(source, target, b);
    expect(result.errorMetres).toBeLessThan(1e-5);
    expect(world(source, b.arm.wrist, b.sourceAnchor).distanceTo(world(target, b.targetBone, b.targetAnchor))).toBeLessThan(1e-5);
    expect(['leftForeArm', 'leftHand'].map(n => source.getObjectByName(n)!.position.length())).toEqual(lengths);
    expect(pose(target)).toEqual(targetBefore);
    expect(source.getObjectByName('leftUpperArm')!.quaternion.angleTo(new THREE.Quaternion())).toBeGreaterThan(.1);
  });
  it('follows wrist and torso motion at absolute times, seeks out of order and restores both actors', () => {
    const { source, target, world } = pair(), b = binding(), before = [pose(source), pose(target)];
    const runtime = motion.createWristContactRuntime(source, target, b);
    const evaluate = (t: number) => { target.position.x = .85 - t * .04; target.getObjectByName('chest')!.rotation.z = -.08 * t; target.getObjectByName('rightForeArm')!.rotation.x = t * .12; };
    const samples = new Map<number, number[][]>();
    for (const t of [0, 1, 2, .5, 2, 0, 1]) {
      const r = runtime.sampleAt(t, evaluate); expect(r.errorMetres).toBeLessThan(1e-5);
      expect(world(source, b.arm.wrist, b.sourceAnchor).distanceTo(world(target, b.targetBone, b.targetAnchor))).toBeLessThan(1e-5);
      if (samples.has(t)) expect(pose(source)).toEqual(samples.get(t)); else samples.set(t, pose(source));
    }
    runtime.stop(); expect([pose(source), pose(target)]).toEqual(before); runtime.stop();
    expect(() => runtime.sampleAt(0, evaluate)).toThrow(/停止/);
  });
  it('solves rotated parent coordinates rather than treating local anchors as world points', () => {
    const { source, target, world } = pair(), scene = new THREE.Group(); scene.rotation.y = .73; scene.position.set(3, 2, -5); scene.add(source, target);
    const b = binding(), r = motion.solveWristContact(source, target, b); expect(r.errorMetres).toBeLessThan(1e-5);
    expect(world(source, b.arm.wrist, b.sourceAnchor).distanceTo(world(target, b.targetBone, b.targetAnchor))).toBeLessThan(1e-5);
  });
  it('rejects unreachable targets atomically instead of stretching the arm or accepting a residual', () => {
    const { source, target } = pair(); target.position.x = 10; const before = pose(source);
    expect(() => motion.solveWristContact(source, target, binding())).toThrow(/不可达/); expect(pose(source)).toEqual(before);
  });
  it('restores both actors when pose evaluation or contact solving fails', () => {
    const { source, target } = pair(), before = [pose(source), pose(target)], runtime = motion.createWristContactRuntime(source, target, binding());
    runtime.sampleAt(0, () => {});
    expect(() => runtime.sampleAt(1, () => { target.position.x = 20; })).toThrow(/不可达/);
    expect([pose(source), pose(target)]).toEqual(before);
    expect(() => runtime.sampleAt(1, () => { source.position.z = 4; throw Error('pose failure'); })).toThrow('pose failure');
    expect([pose(source), pose(target)]).toEqual(before); runtime.stop();
  });
  it('rejects unknown fields, missing/duplicate bones, invalid poles and shared rigs before changing poses', () => {
    const { source, target } = pair(), before = pose(source);
    for (const bad of [{ ...binding(), typo: true }, { ...binding(), pole: [0, 0, 0] }, { ...binding(), rotation: [0, 0, 0, 2] }, { ...binding(), sourceAnchor: [NaN, 0, 0] }, { ...binding(), targetBone: 'missing' }]) {
      expect(() => motion.solveWristContact(source, target, bad as any)).toThrow(); expect(pose(source)).toEqual(before);
    }
    expect(() => motion.solveWristContact(source, source, binding())).toThrow(/独立/);
    const duplicate = new THREE.Bone(); duplicate.name = 'rightHand'; target.add(duplicate);
    expect(() => motion.solveWristContact(source, target, binding())).toThrow(/unique/);
  });
  it('rejects scaled chains and removed target bindings without silently reattaching by name', () => {
    const { source, target } = pair(), runtime = motion.createWristContactRuntime(source, target, binding());
    expect(() => runtime.sampleAt(0, () => { source.scale.x = 2; })).toThrow(/unit-scale/);
    const wrist = target.getObjectByName('rightHand')!; wrist.removeFromParent();
    expect(() => runtime.sampleAt(0, () => {})).toThrow(/绑定已变化/); runtime.stop();
  });
  function surfacePair() {
    const p = pair(), b = binding(); p.target.getObjectByName('rightUpperArm')!.rotation.z = -.2;
    // Continuous proxy arms have a .065m skin radius. 1mm above each surface;
    // this is a near-surface anchor, never permission for hand/body penetration.
    b.sourceAnchor = [.066, -.02, .0047]; b.targetAnchor = [-.066, -.02, .0047];
    motion.solveWristContact(p.source, p.target, b); return { ...p, b };
  }
  it('proves a conservative whole-skin separating plane using every real deformed vertex and surface triangle', () => {
    const { source, target, b } = surfacePair(), before = [pose(source), pose(target)];
    const proof = motion.assertWristContactSurface(source, target, b, [-1, 0, 0]);
    expect(proof.policy).toBe('whole-skin-separating-plane-v1');
    expect(proof.vertices).toBeGreaterThan(1000); expect(proof.triangles).toBeGreaterThan(1000);
    expect(proof.sourceMinimum).toBeGreaterThanOrEqual(-1e-7); expect(proof.targetMaximum).toBeLessThanOrEqual(1e-7);
    expect(proof.sourceSurfaceError).toBeLessThanOrEqual(.002); expect(proof.targetSurfaceError).toBeLessThanOrEqual(.002);
    expect([pose(source), pose(target)]).toEqual(before);
  });
  it('places the plane inside the measured positive skin gap, never through either convex support', () => {
    const { source, target, b } = surfacePair();
    const point = target.getObjectByName(b.targetBone)!.localToWorld(new THREE.Vector3(...b.targetAnchor));
    const normal = new THREE.Vector3(-1, 0, 0).applyQuaternion(target.getObjectByName(b.targetBone)!.getWorldQuaternion(new THREE.Quaternion()));
    const patch = new THREE.Mesh(new THREE.BoxGeometry(.0002, .0002, .0002), new THREE.MeshStandardMaterial());
    // An entire source patch crosses the anchor plane by .4mm, but is still
    // separated from the target's nearest convex support (~1mm past the anchor).
    patch.position.copy(point).addScaledVector(normal, -.0003).add(new THREE.Vector3(0, 0, .1)); source.add(patch);
    const proof = motion.assertWristContactSurface(source, target, b, [-1, 0, 0]);
    expect(proof.planeOffsetMetres).toBeLessThan(0);
    expect(proof.separationMetres).toBeGreaterThan(.0004);
    expect(proof.sourceMinimum).toBeGreaterThan(0); expect(proof.targetMaximum).toBeLessThan(0);
    patch.position.addScaledVector(normal, -.002);
    expect(() => motion.assertWristContactSurface(source, target, b, [-1, 0, 0])).toThrow('分离平面');
    source.remove(patch); patch.geometry.dispose(); (patch.material as THREE.Material).dispose();
  });
  it('refuses intersecting torso/clothes rather than exempting the two contact actors', () => {
    const { source, target, b } = surfacePair();
    const cloth = new THREE.Mesh(new THREE.BoxGeometry(2, 1, 1), new THREE.MeshStandardMaterial()); source.add(cloth);
    expect(() => motion.assertWristContactSurface(source, target, b, [-1, 0, 0])).toThrow(/分离平面/);
  });
  it('refuses air anchors, non-unit normals, missing skin and nonfinite/deforming shader geometry', () => {
    const { source, target, b } = surfacePair();
    expect(() => motion.assertWristContactSurface(source, target, b, [2, 0, 0])).toThrow(/法线/);
    expect(() => motion.assertWristContactSurface(source, target, b, [1, 0, 0])).toThrow(/分离平面/);
    const air = { ...b, sourceAnchor: [.09, -.02, .0047] as [number,number,number], targetAnchor: [-.09, -.02, .0047] as [number,number,number] };
    motion.solveWristContact(source, target, air);
    expect(() => motion.assertWristContactSurface(source, target, air, [-1, 0, 0])).toThrow(/表面/);
    const empty = new THREE.Group();
    expect(() => motion.assertWristContactSurface(empty, target, b, [-1, 0, 0])).toThrow();
    const bad = new THREE.Mesh(new THREE.BoxGeometry(), new THREE.ShaderMaterial()); source.add(bad);
    expect(() => motion.assertWristContactSurface(source, target, air, [-1, 0, 0])).toThrow(/不支持/);
    source.remove(bad); const mesh = source.getObjectByName('riggedPersonBody') as THREE.SkinnedMesh;
    mesh.geometry.getAttribute('position').setX(0, NaN);
    expect(() => motion.assertWristContactSurface(source, target, air, [-1, 0, 0])).toThrow(/有限/);
  });
});
