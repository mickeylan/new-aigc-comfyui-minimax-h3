import { expect, it } from 'vitest';
import * as THREE from 'three';
import { assertScriptShotApplicationSafe, createScriptShotSampler } from './scriptShotSampler';
import { validateScriptShotPlan, type ScriptShotPlan } from './scriptShotPlan';
import { createEnvironmentTerrain, defaultSceneEnvironment } from './ground-path';
import { createRiggedPerson } from './toonflow/riggedPerson';
import { serializeBonePose } from './posing/bonePose';
import { correctExecutionBeatClip } from './performanceCorrection';

function fixture() {
  const entries = [-3, 3].map((x, i) => {
    const object = new THREE.Group(); object.position.x = x;
    object.add(new THREE.Mesh(new THREE.BoxGeometry(1, 2, 1), new THREE.MeshBasicMaterial()));
    const bone = new THREE.Bone(); bone.name = 'joint'; object.add(bone);
    object.animations = [new THREE.AnimationClip('idle', 2, [new THREE.VectorKeyframeTrack('joint.position', [0, 2], [0, 0, 0, 0, 1, 0])])];
    return { id: `a${i}`, name: `A${i}`, object };
  });
  const actor = (i: number, x = entries[i].object.position.x) => ({ targetId: entries[i].id, targetName: entries[i].name, beats: [{ start: 0, end: 2, from: { x, z: 0 }, to: { x, z: 0 }, yaw: 0, animation: { index: 0, name: 'idle', duration: 2 } }] });
  const plan: ScriptShotPlan = { version: 1, sceneId: 's', heading: '', location: '', sourceFingerprint: 's', sceneFingerprint: 's', action: [], diagnostics: [], shots: [{ id: 'one', order: 1, start: 0, end: 2, camera: { position: [0, 3, 8], target: [0, 0, 0], fov: 45 }, dialogue: [], execution: { version: 1, actors: [actor(0), actor(1)] } }] };
  const updates = (id: string, x: number, scale = 1) => [{ id, position: [x, 0, 0], rotation: [0, .7, 0], scale: [scale, scale, scale] }];
  const snapshot = () => entries.map(e => ({ position: e.object.position.toArray(), quaternion: e.object.quaternion.toArray(), scale: e.object.scale.toArray(), parent: e.object.parent, bone: e.object.getObjectByName('joint')!.position.toArray() }));
  return { entries, plan, actor, updates, snapshot };
}

it('keeps an actor facing pi upright after baseline quaternion restore and repeated absolute seeks', () => {
  const f = fixture(), root = f.entries[0].object;
  root.rotation.y = Math.PI;
  f.plan.shots[0].execution!.actors![0].beats[0].yaw = Math.PI;
  const baseline = root.quaternion.clone();
  const sampler = createScriptShotSampler(f.plan, { camera: new THREE.PerspectiveCamera(), controls: { target: new THREE.Vector3(), enabled: true, update() {} }, entries: () => f.entries, environment: null });
  try { for (const t of [0, 1, .2, 2, 0]) {
    sampler.sampleAt(t);
    expect(new THREE.Vector3(0, 1, 0).applyQuaternion(root.quaternion).y).toBeCloseTo(1, 8);
    expect(new THREE.Vector3(0, 0, 1).applyQuaternion(root.quaternion).z).toBeCloseTo(-1, 8);
  } } finally { sampler.stop(); }
  expect(root.quaternion.angleTo(baseline)).toBeLessThan(1e-7);
});

it('rejects real-box clearance before application although the 0.7m structural floor passes', () => {
  const f = fixture(), baseline = f.snapshot();
  f.plan.shots[0].execution!.actors = [f.actor(0, 0), f.actor(1, 1)];
  expect(() => validateScriptShotPlan(f.plan)).not.toThrow();
  // Box half diagonal = sqrt(.5²+.5²); runtime radius adds .35, pair threshold ≈2.114m.
  expect(() => assertScriptShotApplicationSafe(f.plan, f.entries, null, f.updates('a0', 0))).toThrow('间距');
  expect(f.snapshot()).toEqual(baseline);
  expect(() => createScriptShotSampler(f.plan, { camera: new THREE.PerspectiveCamera(), controls: { target: new THREE.Vector3(), enabled: true, update() {} }, entries: () => f.entries, environment: null })).toThrow('间距');
});

it('proves proposed obstacle transforms rather than old positions and restores success and failure', () => {
  const f = fixture();
  const wall = new THREE.Mesh(new THREE.BoxGeometry(1, 3, 1), new THREE.MeshBasicMaterial()); wall.position.x = 10;
  const entries = [...f.entries, { id: 'wall', name: '墙', object: wall }], baseline = f.snapshot();
  expect(() => assertScriptShotApplicationSafe(f.plan, entries, null, f.updates('wall', -3))).toThrow('墙');
  expect(wall.position.x).toBe(10); expect(wall.rotation.y).toBe(0); expect(f.snapshot()).toEqual(baseline);
  wall.position.x = -3;
  expect(() => assertScriptShotApplicationSafe(f.plan, entries, null, f.updates('wall', 10))).not.toThrow();
  expect(wall.position.x).toBe(-3); expect(wall.rotation.y).toBe(0); expect(f.snapshot()).toEqual(baseline);
});

it('rejects oversized proposed actor geometry and restores all transforms', () => {
  const f = fixture(), baseline = f.snapshot();
  expect(() => assertScriptShotApplicationSafe(f.plan, f.entries, null, f.updates('a0', -3, 20))).toThrow('包围体');
  expect(f.snapshot()).toEqual(baseline);
});

it('includes actors held before their first bound cut at the proposed editing position', () => {
  const f = fixture();
  f.plan.shots[0].execution!.actors = [f.actor(0)];
  f.plan.shots.push({ ...structuredClone(f.plan.shots[0]), id: 'two', order: 2, start: 2, end: 4, execution: { version: 1, actors: [f.actor(1)] } });
  const baseline = f.snapshot();
  expect(() => assertScriptShotApplicationSafe(f.plan, f.entries, null, f.updates('a1', -2))).toThrow('间距');
  expect(f.snapshot()).toEqual(baseline);
  expect(() => assertScriptShotApplicationSafe(f.plan, f.entries, null)).not.toThrow();
});

it('keeps static held/beat terrain-water checks, with no mutation on rejection', () => {
  const f = fixture(), baseline = f.snapshot();
  const terrain = createEnvironmentTerrain({ size: 40, segments: 64, amplitude: 0 });
  terrain.waterLevels = terrain.heights.map(() => 1);
  expect(() => assertScriptShotApplicationSafe(f.plan, f.entries, { ...defaultSceneEnvironment(), terrain })).toThrow('水域');
  expect(f.snapshot()).toEqual(baseline);
});

function contactFixture() {
  const source = createRiggedPerson('#eee'), target = createRiggedPerson('#ccc'); target.position.x = .85;
  const q = new THREE.Quaternion().setFromAxisAngle(new THREE.Vector3(0, 0, 1), -.2).toArray();
  source.animations = [new THREE.AnimationClip('hold', .2, [new THREE.QuaternionKeyframeTrack('chest.quaternion', [0, .2], [0, 0, 0, 1, 0, 0, 0, 1])])];
  target.animations = [new THREE.AnimationClip('offer', .2, [new THREE.QuaternionKeyframeTrack('rightUpperArm.quaternion', [0, .2], [...q, ...q])])];
  const entries = [{ id: 'a', name: '抓取者', object: source }, { id: 'b', name: '被抓者', object: target }];
  const plan: ScriptShotPlan = { version: 1, sceneId: 's', heading: '', location: '', sourceFingerprint: 's', sceneFingerprint: 's', action: [], diagnostics: [], shots: [{ id: 'one', order: 1, start: 0, end: .2, camera: { position: [0, 3, 8], target: [0, 0, 0], fov: 45 }, dialogue: [], execution: { version: 1,
    actors: entries.map((e, i) => ({ targetId: e.id, targetName: e.name, beats: [{ start: 0, end: .2, from: { x: i * .85, z: 0 }, to: { x: i * .86, z: 0 }, yaw: 0, animation: { index: 0, name: e.object.animations[0].name, duration: .2 } }] })),
    contacts: [{ version: 1, policy: 'whole-skin-separating-plane-v1', sourceId: 'a', targetId: 'b', start: 0, end: .2, normal: [-1, 0, 0],
      binding: { version: 1, arm: { shoulder: 'leftUpperArm', elbow: 'leftForeArm', wrist: 'leftHand' }, targetBone: 'rightHand', sourceAnchor: [.066, -.02, .0047], targetAnchor: [-.066, -.02, .0047], rotation: [0, 0, 0, 1], pole: [0, 0, 1] } }] } }] };
  const snapshot = () => entries.map(e => ({ p: e.object.position.toArray(), q: e.object.quaternion.toArray(), pose: serializeBonePose(e.object) }));
  const camera = new THREE.PerspectiveCamera(), controls = { target: new THREE.Vector3(), enabled: true, enableDamping: true, update() {} };
  const sampler = () => createScriptShotSampler(plan, { camera, controls, entries: () => entries, environment: null });
  const error = () => source.getObjectByName('leftHand')!.localToWorld(new THREE.Vector3(.066, -.02, .0047)).distanceTo(target.getObjectByName('rightHand')!.localToWorld(new THREE.Vector3(-.066, -.02, .0047)));
  return { plan, entries, source, target, snapshot, camera, controls, sampler, error };
}

it('runs actual paired wrist IK after BOTH base clips in the shared viewport/reference/video sampler', () => {
  const f = contactFixture(), baseline = f.snapshot(), sampler = f.sampler();
  expect(f.snapshot()).toEqual(baseline); // preflight borrows no persistent poses
  expect(sampler.walkProof.contact?.policy).toBe('whole-skin-separating-plane-v1');
  expect(sampler.walkProof.contact!.samples).toBeGreaterThanOrEqual(7);
  expect(sampler.walkProof.contact!.maxSampleIntervalSeconds).toBeLessThanOrEqual(1 / 60 + 1e-9);
  sampler.sampleAt(0); const initialWrist = f.target.getObjectByName('rightHand')!.getWorldPosition(new THREE.Vector3());
  sampler.sampleAt(.2); expect(f.target.getObjectByName('rightHand')!.getWorldPosition(new THREE.Vector3()).distanceTo(initialWrist)).toBeGreaterThan(.008);
  for (const t of [.2, 0, .03, .08, .03]) { sampler.sampleAt(t); expect(f.error()).toBeLessThan(1e-5); }
  const pose = f.snapshot(); sampler.stop(); expect(f.snapshot()).toEqual(baseline); expect(f.controls.enabled).toBe(true); expect(f.controls.enableDamping).toBe(true);
  f.plan.shots[0].execution!.actors!.reverse(); const reversed = f.sampler(); reversed.sampleAt(.03);
  expect(f.snapshot()).toEqual(pose); reversed.stop();
});

it('preflights real skin contact before application and rejects an arm that merely points nearby', () => {
  const f = contactFixture(), baseline = f.snapshot();
  expect(assertScriptShotApplicationSafe(f.plan, f.entries, null).contact?.samples).toBeGreaterThan(0);
  expect(f.snapshot()).toEqual(baseline);
  f.plan.shots[0].execution!.contacts![0].binding.sourceAnchor = [.09, -.02, .0047];
  f.plan.shots[0].execution!.contacts![0].binding.targetAnchor = [-.09, -.02, .0047];
  expect(() => assertScriptShotApplicationSafe(f.plan, f.entries, null)).toThrow('表面');
  expect(f.snapshot()).toEqual(baseline); expect(f.controls.enabled).toBe(true);
});

it('rejects unreachable or clothing-intersecting contact plans atomically without granting pair immunity', () => {
  for (const mode of ['unreachable', 'clothes']) {
    const f = contactFixture(), baseline = f.snapshot();
    if (mode === 'unreachable') { const beat = f.plan.shots[0].execution!.actors![1].beats[0]; beat.from.x = beat.to.x = 5; }
    else f.source.add(new THREE.Mesh(new THREE.BoxGeometry(2, 1, 1), new THREE.MeshStandardMaterial()));
    expect(() => f.sampler()).toThrow(mode === 'unreachable' ? '不可达' : '分离平面');
    expect(f.snapshot()).toEqual(baseline); expect(f.controls.enabled).toBe(true); expect(f.controls.enableDamping).toBe(true);
  }
});

it('revalidates live skin on every output frame and restores actors/camera/controls after failure', () => {
  const f = contactFixture(), baseline = f.snapshot(), cameraBefore = f.camera.position.toArray(), sampler = f.sampler();
  sampler.sampleAt(.03); f.source.add(new THREE.Mesh(new THREE.BoxGeometry(2, 1, 1), new THREE.MeshStandardMaterial()));
  expect(() => sampler.sampleAt(.05)).toThrow('分离平面');
  expect(f.snapshot()).toEqual(baseline); expect(f.camera.position.toArray()).toEqual(cameraBefore); expect(f.controls.enabled).toBe(true);
  sampler.stop();
});

it('rejects a turning wrist when this conservative plane intersects a foot rather than widening its policy', () => {
  const f = contactFixture(), baseline = f.snapshot();
  const q = new THREE.Quaternion().setFromAxisAngle(new THREE.Vector3(0, 0, 1), -.22).toArray();
  f.target.animations[0].tracks[0].values.set(q, 4);
  const beat = f.plan.shots[0].execution!.actors![1].beats[0]; beat.to.x = beat.from.x;
  expect(() => f.sampler()).toThrow('分离平面'); expect(f.snapshot()).toEqual(baseline);
});

it('rejects an unsafe manually selected replacement clip at the real skin gate, preserving the original plan and live pose', () => {
  const f = contactFixture();
  const beat = f.plan.shots[0].execution!.actors![1].beats[0]; beat.to.x = beat.from.x;
  const original = structuredClone(f.plan), baseline = f.snapshot();
  expect(() => assertScriptShotApplicationSafe(f.plan, f.entries, null)).not.toThrow();
  const unsafe = f.target.animations[0].clone(); unsafe.name = 'manual-unsafe-wrist-turn';
  const q = new THREE.Quaternion().setFromAxisAngle(new THREE.Vector3(0, 0, 1), -.22).toArray(); unsafe.tracks[0].values.set(q, 4);
  f.target.animations.push(unsafe);
  const targets = f.entries.map(e => ({ id: e.id, name: e.name, visible: true, locked: false, animations: e.object.animations.map((c, index) => ({ index, name: c.name, duration: c.duration })) }));
  const corrected = correctExecutionBeatClip(f.plan, { shotId: 'one', targetId: 'b', beatIndex: 0, animation: targets[1].animations[1], reason: '隔离测试：人工改片段仍不可绕过安全门禁' }, targets);
  expect(() => assertScriptShotApplicationSafe(corrected, f.entries, null)).toThrow('分离平面');
  expect(f.plan).toEqual(original); expect(f.snapshot()).toEqual(baseline);
  console.log('MANUAL_PERFORMANCE_SKIN_GATE=PASS unsafeReplacement=rejected originalPlan=preserved livePose=restored');
});

it('keeps whole-shot wrist constraints across a split and rejects an unsafe second phase with the unchanged skin gate', () => {
  const f = contactFixture();
  const beat = f.plan.shots[0].execution!.actors![1].beats[0]; beat.to.x = beat.from.x;
  const unsafe = f.target.animations[0].clone(); unsafe.name = 'unsafe-second-phase';
  // A split restarts local time: its .15s phase never reaches the original .2s
  // final key. Make the unsafe pose present from local zero, not after the cut.
  const q = new THREE.Quaternion().setFromAxisAngle(new THREE.Vector3(0, 0, 1), -.22).toArray(); unsafe.tracks[0].values.set([...q, ...q]);
  f.target.animations.push(unsafe);
  const targets = f.entries.map(e => ({ id: e.id, name: e.name, visible: true, locked: false, animations: e.object.animations.map((c, index) => ({ index, name: c.name, duration: c.duration })) }));
  const input = { shotId: 'one', targetId: 'b', beatIndex: 0, animation: targets[1].animations[0], reason: '人工两阶段仍保持整镜贴合；非释放', split: { at: .05, afterAnimation: targets[1].animations[0] } };
  const baseline = f.snapshot(), original = structuredClone(f.plan);
  const safe = correctExecutionBeatClip(f.plan, input, targets);
  expect(safe.shots[0].execution!.contacts).toEqual(original.shots[0].execution!.contacts);
  const sampler = createScriptShotSampler(safe, { camera: f.camera, controls: f.controls, entries: () => f.entries, environment: null });
  try { for (const t of [.01, .05, .1, .2, .05]) { sampler.sampleAt(t); expect(f.error()).toBeLessThan(1e-5); } } finally { sampler.stop(); }
  const bad = correctExecutionBeatClip(f.plan, { ...input, split: { at: .05, afterAnimation: targets[1].animations[1] } }, targets);
  expect(() => assertScriptShotApplicationSafe(bad, f.entries, null)).toThrow('分离平面');
  expect(f.plan).toEqual(original); expect(f.snapshot()).toEqual(baseline);
  console.log('SPLIT_PERFORMANCE_SKIN_GATE=PASS contact=whole-shot boundary=sampled unsafeSecondPhase=rejected original=preserved stop=restored');
});

it('rejects base clips that change arm length instead of using the stretched rig to satisfy contact', () => {
  const f = contactFixture(), bone = f.source.getObjectByName('leftForeArm')!, p = bone.position.clone().multiplyScalar(1.01).toArray();
  f.source.animations[0].tracks.push(new THREE.VectorKeyframeTrack('leftForeArm.position', [0, .2], [...p, ...p]));
  const baseline = f.snapshot(); expect(() => f.sampler()).toThrow('骨长'); expect(f.snapshot()).toEqual(baseline);
});

it('does not exempt a third actor/environment object moved into a live contact pose', () => {
  const f = contactFixture(), wall = new THREE.Mesh(new THREE.BoxGeometry(.2, .4, .4), new THREE.MeshStandardMaterial()); wall.position.set(10, 1, 0);
  f.entries.push({ id: 'wall', name: '环境阻挡', object: wall as any }); const baseline = f.snapshot(), sampler = f.sampler();
  sampler.sampleAt(.03); wall.position.x = .3;
  expect(() => sampler.sampleAt(.05)).toThrow('环境阻挡');
  expect(f.snapshot().slice(0, 2)).toEqual(baseline.slice(0, 2)); expect(wall.position.x).toBe(.3); expect(f.controls.enabled).toBe(true); sampler.stop();
});
