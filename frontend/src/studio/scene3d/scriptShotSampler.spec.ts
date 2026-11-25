import { describe, expect, it } from 'vitest';
import * as THREE from 'three';
import { createScriptShotSampler } from './scriptShotSampler';
import { parseScriptScenes, buildScriptSceneDraft, buildScriptShotPlan, setScriptShotCamera, setScriptShotMotion, updateScriptShotDuration } from './toonflowSets';
import { createEnvironmentTerrain, defaultSceneEnvironment } from './ground-path';
import { createToonflowPrimitive } from './toonflowPrimitives';
import { findCapsuleRigs } from './toonflow/capsuleRig';

async function fixture() {
  const draft = await buildScriptSceneDraft(parseScriptScenes('1. 内景 现代卧室 日\n甲：第一句。\n乙：第二句。\n甲：第三句。'), 'scene-1', new AbortController().signal);
  let plan = buildScriptShotPlan(draft);
  for (const shot of plan.shots) {
    plan = updateScriptShotDuration(plan, shot.id, 2);
    plan = setScriptShotCamera(plan, shot.id, { position: [shot.order, 3, 6], target: [0, 1, 0], fov: 45 });
  }
  const camera = new THREE.PerspectiveCamera(); camera.position.set(8, 7, 9);
  const controls = { target: new THREE.Vector3(), enabled: true, enableDamping: true, update() {} };
  const actor = new THREE.Group(), bone = new THREE.Bone(); bone.name = 'hip'; actor.add(bone);
  actor.animations = [new THREE.AnimationClip('wave', 2, [new THREE.VectorKeyframeTrack(`${bone.uuid}.position`, [0, 2], [0, 0, 0, 0, 2, 0])])];
  const entries = [{ id: 'a', name: '甲', object: actor, locked: false }];
  plan = setScriptShotMotion(plan, plan.shots[0].id, { targetId: 'a', targetName: '甲', start: { x: 0, z: 0 }, end: { x: 2, z: 0 }, duration: 2, animation: { index: 0, name: 'wave', duration: 2 } });
  const baselines = new Map();
  return { plan, camera, controls, actor, bone, entries, baselines, options: { camera, controls, entries: () => entries, environment: null, baselines } };
}

describe('production random-access shot sampler', () => {
  it('编辑基线快照排除走位角色自身/隐藏/splat；锁定静态障碍仍在任何位移前拒绝', async () => {
    const f = await fixture();
    const mesh = () => new THREE.Mesh(new THREE.BoxGeometry(0.1, 2, 2));
    const wall = mesh(); wall.position.x = 1;
    f.entries.push({ id: 'wall', name: '薄墙', object: wall as any, locked: true });
    expect(() => createScriptShotSampler(f.plan, f.options)).toThrow('薄墙');
    expect(f.controls.enabled).toBe(true); expect(f.actor.position.toArray()).toEqual([0, 0, 0]); expect(f.baselines.size).toBe(0);
    wall.visible = false;
    const session = createScriptShotSampler(f.plan, f.options);
    expect(session.walkProof.obstacles).toHaveLength(0); expect(session.walkProof.excludedIds).toEqual(['a']); session.stop();
    wall.visible = true; (f.entries[1] as any).splat = {};
    const splatSession = createScriptShotSampler(f.plan, f.options); expect(splatSession.walkProof.obstacles).toHaveLength(0); splatSession.stop();
    delete (f.entries[1] as any).splat;
    const plan = setScriptShotMotion(f.plan, f.plan.shots[1].id, { targetId: 'wall', targetName: '薄墙', start: { x: 0, z: 0 }, end: { x: 2, z: 0 }, duration: 2 });
    f.entries[1].locked = false;
    const actors = createScriptShotSampler(plan, f.options); expect(actors.walkProof.excludedIds).toEqual(['a', 'wall']); expect(actors.walkProof.obstacles).toHaveLength(0); actors.stop();
    wall.geometry.dispose(); (wall.material as THREE.Material).dispose();
  });
  it('真实骨骼按时间采样，跳过镜次仍保留所有已完成走位；任意seek顺序得到同一帧，stop精确还原', async () => {
    const f = await fixture(), base = f.camera.position.toArray();
    const session = createScriptShotSampler(f.plan, f.options);
    session.sampleAt(1); expect(f.actor.position.x).toBe(1); expect(f.bone.position.y).toBeCloseTo(1);
    const first = f.actor.position.toArray(), pose = f.bone.position.toArray();
    session.sampleAt(session.duration - 0.1); expect(f.actor.position.x).toBe(2); expect(f.bone.position.y).toBe(0);
    session.sampleAt(1); expect(f.actor.position.toArray()).toEqual(first); expect(f.bone.position.toArray()).toEqual(pose);
    expect(f.baselines.size).toBe(1); session.stop(); session.stop();
    expect(f.actor.position.toArray()).toEqual([0, 0, 0]); expect(f.bone.position.y).toBe(0);
    expect(f.camera.position.toArray()).toEqual(base); expect(f.controls.enabled).toBe(true); expect(f.controls.enableDamping).toBe(true); expect(f.baselines.size).toBe(0);
    expect(() => session.sampleAt(1)).toThrow('已停止');
  });
  it('隐藏、锁定、clip变化、地形越界全部在改动前拒绝；采样中删除/换场也拒绝', async () => {
    for (const change of ['hidden', 'locked', 'clip'] as const) {
      const f = await fixture();
      if (change === 'hidden') f.actor.visible = false;
      if (change === 'locked') f.entries[0].locked = true;
      if (change === 'clip') f.actor.animations = [];
      expect(() => createScriptShotSampler(f.plan, f.options)).toThrow(); expect(f.controls.enabled).toBe(true); expect(f.baselines.size).toBe(0);
    }
    const f = await fixture(); let current = true;
    const session = createScriptShotSampler(f.plan, { ...f.options, isCurrent: () => current });
    f.entries[0].locked = true; expect(() => session.sampleAt(1)).toThrow('锁定'); f.entries[0].locked = false;
    current = false; f.camera.position.set(10, 10, 10); session.stop(); expect(f.camera.position.toArray()).toEqual([10, 10, 10]);
    expect(f.controls.enabled).toBe(true);
    const outside = setScriptShotMotion(f.plan, f.plan.shots[0].id, { targetId: 'a', targetName: '甲', start: { x: 0, z: 0 }, end: { x: 50, z: 0 }, duration: 2 });
    expect(() => createScriptShotSampler(outside, { ...f.options, environment: { ...defaultSceneEnvironment(), terrain: createEnvironmentTerrain({ size: 40 }) } })).toThrow('范围外');
  });
  it('同角色跨镜沿坡地保持脚底偏移，随机跳转与顺播一致，停止恢复编辑基线', async () => {
    for (const navigation of [undefined, { version: 1, mode: 'avoid', radius: 0.35, speed: 2 }] as const) {
      const f = await fixture();
      const terrain = createEnvironmentTerrain({ size: 40, amplitude: 0 });
      for (let row = 0; row <= terrain.segments; row++) for (let col = 0; col <= terrain.segments; col++) {
        terrain.heights[row * (terrain.segments + 1) + col] = (col * terrain.size / terrain.segments - terrain.size / 2) / 4;
      }
      f.actor.position.set(0, 0.02, 0);
      let plan = f.plan;
      for (let i = 0; i < 2; i++) plan = setScriptShotMotion(plan, plan.shots[i].id, {
        targetId: 'a', targetName: '甲', start: { x: i * 4, z: 0 }, end: { x: (i + 1) * 4, z: 0 }, duration: 2,
        ...(navigation ? { navigation } : {}),
      });
      const session = createScriptShotSampler(plan, { ...f.options, environment: { ...defaultSceneEnvironment(), terrain } });
      session.sampleAt(1.999); const before = f.actor.position.y;
      session.sampleAt(2); expect(f.actor.position.y).toBeCloseTo(1.02); expect(Math.abs(f.actor.position.y - before)).toBeLessThan(0.001);
      session.sampleAt(3); const later = f.actor.position.toArray(); expect(later[1]).toBeCloseTo(1.52);
      session.sampleAt(0); session.sampleAt(3); expect(f.actor.position.toArray()).toEqual(later);
      session.sampleAt(4.1); expect(f.actor.position.y).toBeCloseTo(2.02);
      session.stop(); expect(f.actor.position.toArray()).toEqual([0, 0.02, 0]); expect(f.baselines.size).toBe(0);
    }
  });
  it('胶囊人真实关节按行进距离摆动；停止后不把临时姿势留进存档', async () => {
    const f = await fixture();
    const capsule = createToonflowPrimitive({ shape: 'person' } as any, '#aabbcc');
    f.entries[0].object = capsule as any;
    const plan = setScriptShotMotion(f.plan, f.plan.shots[0].id, { targetId: 'a', targetName: '甲', start: { x: 0, z: 0 }, end: { x: 2, z: 0 }, duration: 2 });
    const rig = findCapsuleRigs(capsule)[0], base = rig.leg.quaternion.toArray();
    const session = createScriptShotSampler(plan, f.options);
    session.sampleAt(0.25); expect(rig.leg.quaternion.toArray()).not.toEqual(base);
    const first = rig.leg.quaternion.toArray(); session.sampleAt(1); session.sampleAt(0.25); expect(rig.leg.quaternion.toArray()).toEqual(first);
    session.stop(); expect(rig.leg.quaternion.toArray()).toEqual(base);
  });
});
