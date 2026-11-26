import * as THREE from 'three';
import { assertScriptShotMotionTargets, assertScriptShotPlaybackReady, validateScriptShotPlan, sampleScriptShotPlayback, sampleScriptShotMotionAt, type ScriptShotPlan, type ScriptShotMotionSample } from './scriptShotPlan';
import { AnimationPreview } from './animationPreview';
import { sampleExecutionActors, validateShotExecution } from './shotExecution';
import { createCharacterMotionRuntime } from './characterMotion';
import { compileShotNavigation } from './shotNavigation';
import { serializeBonePose } from './posing/bonePose';
import { canMoveEntry } from './sceneSafety';
import { GROUND_OBSTACLE_MARGIN, groundHeightAt, proveWalkableLine, type GroundObstacle, type SceneEnvironment } from './ground-path';
import { findCapsuleRigs, gaitCapsuleRig, resetCapsuleRig } from './toonflow/capsuleRig';

type Entry = { id: string; name: string; locked?: boolean; splat?: unknown; object: THREE.Object3D };
type Baseline = { position: THREE.Vector3; quaternion: THREE.Quaternion; pose: ReturnType<typeof serializeBonePose> };
type Controls = { target: THREE.Vector3; enabled: boolean; enableDamping?: boolean; update(): unknown };

/** One immutable editing-baseline obstacle snapshot for viewport, video and reference sessions. */
export function assertScriptShotWalkable(plan: ScriptShotPlan, entries: readonly Entry[], environment: SceneEnvironment | null) {
  const excludedIds = [...new Set(plan.shots.flatMap(shot => [...(shot.motion ? [shot.motion.targetId] : []), ...(shot.execution?.actors ?? []).map(a => a.targetId)]))];
  const excluded = new Set(excludedIds);
  const obstacles: (GroundObstacle & { id: string })[] = [];
  if (excluded.size) for (const entry of entries) {
    if (excluded.has(entry.id) || entry.splat || !entry.object.visible) continue;
    const box = new THREE.Box3().setFromObject(entry.object);
    if (box.isEmpty()) continue;
    obstacles.push({ id: entry.id, name: entry.name, min: [box.min.x, box.min.z], max: [box.max.x, box.max.z] });
  }
  // Track held actors across cuts, including actors not bound in the current shot.
  const executionIds = new Set(plan.shots.flatMap(s => (s.execution?.actors ?? []).map(a => a.targetId)));
  if (executionIds.size > 32) throw Error('整份执行计划超过32个角色，请拆分场次');
  const radii = new Map([...executionIds].map(id => {
    const object = entries.find(e => e.id === id)?.object;
    if (!object) throw Error(`执行角色不存在：${id}`);
    object.updateMatrixWorld(true);
    const box = new THREE.Box3().setFromObject(object), p = object.getWorldPosition(new THREE.Vector3());
    const radius = box.isEmpty() ? .35 : .35 + Math.hypot(Math.max(Math.abs(box.min.x - p.x), Math.abs(box.max.x - p.x)), Math.max(Math.abs(box.min.z - p.z), Math.abs(box.max.z - p.z)));
    if (!Number.isFinite(radius) || radius > 10) throw Error('执行角色包围体超出可证明安全范围');
    return [id, radius];
  }));
  const held = new Map([...executionIds].map(id => {
    const entry = entries.find(e => e.id === id);
    if (!entry) throw Error(`Execution actor missing: ${id}`);
    return [id, { x: entry.object.position.x, z: entry.object.position.z }];
  }));
  for (const shot of plan.shots) if (executionIds.size) {
    const duration = shot.end - shot.start;
    const actors = [...executionIds].map(id => {
      const bound = shot.execution?.actors?.find(a => a.targetId === id);
      if (bound) return bound;
      const p = held.get(id)!;
      return { targetId: id, targetName: id, beats: [{ start: 0, end: duration, from: p, to: p, yaw: 0, animation: { index: 0, name: 'proof-only', duration: 1 } }] };
    });
    validateShotExecution({ version: 1, actors }, duration, 2 * Math.max(...radii.values()));
    for (const actor of actors) for (const beat of actor.beats) {
      const proof = proveWalkableLine(environment?.terrain, beat.from, beat.to, { obstacles, obstacleMargin: radii.get(actor.targetId) });
      if (!proof.ok) throw Error(`Execution actor ${actor.targetId}: ${proof.reason}`);
    }
    for (const actor of actors) held.set(actor.targetId, actor.beats[actor.beats.length - 1].to);
  }
  const shots = plan.shots.flatMap(shot => {
    if (!shot.motion || shot.motion.navigation) return [];
    const proof = proveWalkableLine(environment?.terrain, shot.motion.start, shot.motion.end, { obstacles });
    if (!proof.ok) throw Error(`第 ${shot.order} 镜走位：${proof.reason}`);
    return [{ shotId: shot.id, ...proof }];
  });
  return { version: 1, policy: 'static-xz-reject-v1', margin: GROUND_OBSTACLE_MARGIN, excludedIds, obstacles, shots };
}

/** Synchronous pre-publication proof against the proposed editor transforms, not the
 * old editing positions. No scene publication, animation ownership, await or history
 * writes; restore every borrowed transform even if the shared runtime proof rejects.
 */
export function assertScriptShotApplicationSafe(plan: ScriptShotPlan, entries: readonly Entry[], environment: SceneEnvironment | null,
  updates: readonly { id: string; position: readonly number[]; rotation: readonly number[]; scale: readonly number[] }[] = []) {
  const ready = assertScriptShotMotionTargets(validateScriptShotPlan(plan), entries.map(entry => ({
    id: entry.id, name: entry.name, visible: entry.object.visible, locked: !!entry.locked,
    animations: entry.object.animations.map((clip, index) => ({ index, name: clip.name, duration: clip.duration })),
  })));
  const staged = updates.map(value => {
    const entry = entries.find(entry => entry.id === value.id);
    if (!entry || !canMoveEntry(entry)) throw Error(`无法预检不可编辑对象：${value.id}`);
    if ([value.position, value.rotation, value.scale].some(v => v.length !== 3 || v.some(n => !Number.isFinite(n)))) throw Error('预检对象变换无效');
    return { value, object: entry.object, position: entry.object.position.clone(), rotation: entry.object.rotation.clone(), scale: entry.object.scale.clone() };
  });
  try {
    for (const { object, value } of staged) {
      object.position.set(value.position[0], value.position[1], value.position[2]);
      object.rotation.set(value.rotation[0], value.rotation[1], value.rotation[2]);
      object.scale.set(value.scale[0], value.scale[1], value.scale[2]);
      object.updateMatrixWorld(true);
    }
    const proof = assertScriptShotWalkable(ready, entries, environment);
    if (ready.shots.some(shot => shot.motion?.navigation)) compileShotNavigation(ready, entries, environment);
    return proof;
  } finally {
    for (const { object, position, rotation, scale } of staged) {
      object.position.copy(position); object.rotation.copy(rotation); object.scale.copy(scale); object.updateMatrixWorld(true);
    }
  }
}

/** Random access cuts + movement + skeletal clips; shared by video and reference captures.
 * All validation precedes the first mutation. Every seek starts at the editing baseline,
 * replays completed movements, then samples the current shot, independent of seek order.
 */
export function createScriptShotSampler(plan: ScriptShotPlan, options: {
  camera: THREE.PerspectiveCamera; controls: Controls; entries: () => readonly Entry[];
  environment: SceneEnvironment | null; baselines?: Map<string, Baseline>; isCurrent?: () => boolean;
}) {
  const entries = options.entries();
  const ready = assertScriptShotMotionTargets(assertScriptShotPlaybackReady(plan), entries.map(entry => ({
    id: entry.id, name: entry.name, visible: entry.object.visible, locked: !!entry.locked,
    animations: entry.object.animations.map((clip, index) => ({ index, name: clip.name, duration: clip.duration })),
  })));
  const legacyProof = assertScriptShotWalkable(ready, entries, options.environment);
  const navigation = ready.shots.some(shot => shot.motion?.navigation) ? compileShotNavigation(ready, entries, options.environment) : null;
  const walkProof = { ...legacyProof, ...(navigation ? { navigation: navigation.proof } : {}) };
  // Validate new rig/clip contracts before taking camera or actor ownership.
  for (const shot of ready.shots) if (shot.motion?.characterMotion) {
    const entry = entries.find(item => item.id === shot.motion!.targetId)!;
    const check = createCharacterMotionRuntime(entry.object, shot.motion.animation ?? undefined, shot.motion.characterMotion);
    check.stop();
  }
  const { camera, controls } = options;
  const cameraState = { p: camera.position.clone(), q: camera.quaternion.clone(), fov: camera.fov, target: controls.target.clone(), enabled: controls.enabled, damping: controls.enableDamping };
  const actors = new Map<string, { entry: Entry; baseline: Baseline; groundOffset: number; activated: boolean; preview: { stop(): void } | null; rigs: ReturnType<typeof findCapsuleRigs> }>();
  for (const shot of ready.shots) if (shot.motion && !actors.has(shot.motion.targetId)) {
    const entry = entries.find(item => item.id === shot.motion!.targetId)!;
    const baseline = { position: entry.object.position.clone(), quaternion: entry.object.quaternion.clone(), pose: serializeBonePose(entry.object) };
    // Anchor once at this actor's FIRST motion start. Re-anchoring at every cut
    // discards accumulated terrain elevation and makes a continuous walk jump down.
    const groundOffset = baseline.position.y - groundHeightAt(options.environment?.terrain, shot.motion.start.x, shot.motion.start.z);
    actors.set(entry.id, { entry, baseline, groundOffset, activated: false, preview: null, rigs: findCapsuleRigs(entry.object) });
  }
  for (const shot of ready.shots) for (const binding of shot.execution?.actors ?? []) if (!actors.has(binding.targetId)) {
    const entry = entries.find(item => item.id === binding.targetId)!;
    const baseline = { position: entry.object.position.clone(), quaternion: entry.object.quaternion.clone(), pose: serializeBonePose(entry.object) };
    const start = binding.beats[0].from;
    actors.set(entry.id, { entry, baseline, groundOffset: baseline.position.y - groundHeightAt(options.environment?.terrain, start.x, start.z), activated: false, preview: null, rigs: findCapsuleRigs(entry.object) });
  }
  controls.enabled = false; controls.enableDamping = false;
  let stopped = false;
  const reset = () => {
    for (const actor of actors.values()) {
      if (!actor.activated) continue;
      actor.preview?.stop(); actor.preview = null;
      actor.rigs.forEach(resetCapsuleRig);
      actor.entry.object.position.copy(actor.baseline.position); actor.entry.object.quaternion.copy(actor.baseline.quaternion);
      actor.entry.object.updateMatrixWorld(true);
    }
  };
  const move = (sample: ScriptShotMotionSample, animate: boolean) => {
    const actor = actors.get(sample.targetId)!;
    if (!options.entries().includes(actor.entry) || !canMoveEntry(actor.entry)) throw Error(`走位目标「${sample.targetName}」已隐藏、锁定或删除`);
    const object = actor.entry.object;
    // Only objects actually sampled are temporary edits. Pre-registering a future
    // shot's actor would hide real edits to that untouched object in dirty checks.
    if (!actor.activated) { actor.activated = true; options.baselines?.set(actor.entry.id, actor.baseline); }
    actor.preview?.stop(); actor.preview = null;
    const shot = ready.shots.find(shot => shot.id === sample.shotId)!;
    const motion = shot.motion!;
    if (motion.navigation) {
      const routed = navigation!.sample(sample.shotId, shot.start + sample.progress * motion.duration);
      sample.position = routed.position; sample.yaw = routed.yaw;
    }
    object.position.x = sample.position.x; object.position.z = sample.position.z; object.rotation.y = sample.yaw;
    const terrain = options.environment?.terrain;
    if (terrain) object.position.y = actor.groundOffset + groundHeightAt(terrain, sample.position.x, sample.position.z);
    if (motion.characterMotion) {
      const runtime = createCharacterMotionRuntime(object, motion.animation ?? undefined, motion.characterMotion);
      actor.preview = runtime;
      runtime.sampleAt(sample.progress * motion.duration, (x, z) => groundHeightAt(terrain, x, z));
    } else if (animate && sample.animation) {
      const clip = object.animations[sample.animation.index];
      if (!clip || clip.name !== sample.animation.name || clip.duration !== sample.animation.duration) throw Error('骨骼动作资源已变化');
      const preview = new AnimationPreview(object, clip); actor.preview = preview;
      preview.sampleAt(Math.min(sample.animationSeconds, Math.max(0, clip.duration - 1e-4)));
    } else if (animate) {
      // Capsule placeholders have real articulated joints; gait follows metres, not render frames.
      const distance = Math.hypot(sample.position.x - motion.start.x, sample.position.z - motion.start.z);
      const speed = Math.hypot(motion.end.x - motion.start.x, motion.end.z - motion.start.z) / motion.duration;
      actor.rigs.forEach(rig => gaitCapsuleRig(rig, distance, speed));
    }
    object.updateMatrixWorld(true);
  };
  const execute = (shot: ScriptShotPlan['shots'][number], seconds: number) => {
    for (const sample of sampleExecutionActors(shot.execution, seconds - shot.start)) {
      const actor = actors.get(sample.targetId)!;
      if (!options.entries().includes(actor.entry) || !canMoveEntry(actor.entry)) throw Error(`Execution actor unavailable: ${sample.targetId}`);
      const object = actor.entry.object;
      const clip = object.animations[sample.animation.index];
      if (!clip || clip.name !== sample.animation.name || clip.duration !== sample.animation.duration) throw Error('Execution clip resource changed');
      if (!actor.activated) { actor.activated = true; options.baselines?.set(actor.entry.id, actor.baseline); }
      actor.preview?.stop();
      object.position.x = sample.position.x; object.position.z = sample.position.z; object.rotation.y = sample.yaw;
      if (options.environment?.terrain) object.position.y = actor.groundOffset + groundHeightAt(options.environment.terrain, sample.position.x, sample.position.z);
      const preview = new AnimationPreview(object, clip); actor.preview = preview;
      preview.sampleAt(Math.min(sample.animationSeconds, Math.max(0, clip.duration - 1e-4)));
      object.updateMatrixWorld(true);
    }
  };
  return {
    plan: ready,
    walkProof,
    duration: ready.shots[ready.shots.length - 1]!.end,
    sampleAt(seconds: number) {
      if (stopped || !Number.isFinite(seconds) || seconds < 0) throw Error('逐镜取样时间无效或会话已停止');
      if (options.isCurrent?.() === false) throw Error('场景已替换，已停止取样');
      reset();
      const sample = sampleScriptShotPlayback(ready, seconds);
      // Replay ALL completed shots, even when jumping across several cuts.
      for (let i = 0; i < sample.index; i++) {
        const previous = ready.shots[i];
        const motion = sampleScriptShotMotionAt(previous, previous.end);
        if (motion) move(motion, false);
        execute(previous, previous.end);
      }
      if (sample.motion) move(sample.motion, true);
      execute(sample.shot, sample.seconds);
      camera.position.set(...sample.camera.position); camera.fov = sample.camera.fov; camera.updateProjectionMatrix();
      controls.target.set(...sample.camera.target); controls.update(); camera.lookAt(controls.target); camera.updateMatrixWorld(true);
      return sample;
    },
    stop() {
      if (stopped) return; stopped = true;
      reset();
      for (const [id, actor] of actors) if (actor.activated) options.baselines?.delete(id);
      if (options.isCurrent?.() !== false) {
        camera.position.copy(cameraState.p); controls.target.copy(cameraState.target); camera.fov = cameraState.fov; camera.updateProjectionMatrix();
        controls.update(); camera.position.copy(cameraState.p); camera.quaternion.copy(cameraState.q); controls.target.copy(cameraState.target); camera.updateMatrixWorld(true);
      }
      controls.enabled = cameraState.enabled; controls.enableDamping = cameraState.damping;
    },
  };
}
