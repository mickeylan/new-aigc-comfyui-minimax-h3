import * as THREE from 'three';
import { restoreMoveAnimation, type SavedMoveAnimation, saveAnimationClips } from './animationPreview';

export interface CharacterMotionSettings {
  version: 1;
  blend?: { animation: SavedMoveAnimation; weight: number; fadeSeconds: number };
  feet?: { left: LegNames; right: LegNames; soleOffset: number; maxCorrection: number };
}
export interface LegNames { hip: string; knee: string; ankle: string }
const fail = (message: string): never => { throw new Error(message); };
const finite = (n: unknown): n is number => typeof n === 'number' && Number.isFinite(n);
export function validateCharacterMotionSettings(value: unknown): CharacterMotionSettings {
  const v = value as CharacterMotionSettings;
  if (!v || v.version !== 1 || Object.keys(v).some(k => !['version', 'blend', 'feet'].includes(k))) fail('Invalid character motion settings');
  if (v.blend !== undefined) {
    const b = v.blend;
    if (!b || typeof b !== 'object' || Object.keys(b).some(k => !['animation','weight','fadeSeconds'].includes(k))) fail('Invalid blend settings');
    const a = b.animation;
    if (!a || !Number.isSafeInteger(a.index) || a.index < 0 || typeof a.name !== 'string' || !finite(a.duration) || a.duration <= 0 || !finite(b.weight) || b.weight < 0 || b.weight > 1 || !finite(b.fadeSeconds) || b.fadeSeconds < 0) fail('Invalid blend reference, weight or fade');
  }
  if (v.feet !== undefined) {
    const f = v.feet;
    if (!f || typeof f !== 'object' || Object.keys(f).some(k => !['left','right','soleOffset','maxCorrection'].includes(k))) fail('Invalid feet settings');
    if (!finite(f.soleOffset) || !finite(f.maxCorrection) || f.maxCorrection <= 0) fail('Invalid foot correction bounds');
    const names = [f.left, f.right].flatMap(l => l ? [l.hip, l.knee, l.ankle] : []);
    if (names.length !== 6 || names.some(n => typeof n !== 'string' || !n.trim()) || new Set(names).size !== 6) fail('Feet require six distinct bone names');
  }
  return JSON.parse(JSON.stringify(v));
}
function bones(root: THREE.Object3D) {
  const result = new Map<string, THREE.Bone>();
  root.traverse(n => { if (n instanceof THREE.Bone) { if (!n.name || result.has(n.name)) fail('Bones require unique nonempty stable names'); result.set(n.name, n); } });
  if (!result.size) fail('No bones');
  return result;
}
function snapshot(root: THREE.Object3D) {
  const entries: { n: THREE.Object3D; p: THREE.Vector3; q: THREE.Quaternion; s: THREE.Vector3 }[] = [];
  root.traverse(n => entries.push({ n, p: n.position.clone(), q: n.quaternion.clone(), s: n.scale.clone() }));
  return () => { for (const { n, p, q, s } of entries) { n.position.copy(p); n.quaternion.copy(q); n.scale.copy(s); n.updateMatrix(); } root.updateWorldMatrix(true, true); };
}
function unitScales(root: THREE.Object3D) {
  root.updateWorldMatrix(true, true);
  const check = (n: THREE.Object3D) => {
    if (!n.matrixAutoUpdate || ![...n.position, ...n.quaternion, ...n.scale].every(Number.isFinite) || Math.abs(n.quaternion.lengthSq()-1) > 1e-4 || n.scale.distanceTo(new THREE.Vector3(1, 1, 1)) > 1e-6) fail('Only finite normalized rotations and automatic unit-scale transforms are supported');
  };
  // Mesh-only local scales do not change a bone chain. Enforce unit transforms
  // on the root/ancestors and on bone ancestors, not decorative child meshes.
  const relevant = new Set<THREE.Object3D>([root]);
  root.traverse(n => { if ((n as THREE.Bone).isBone) for (let p: THREE.Object3D | null = n; p; p = p.parent) relevant.add(p); });
  for (let p = root.parent; p; p = p.parent) relevant.add(p);
  relevant.forEach(check);
}
function checkedClip(root: THREE.Object3D, clip: THREE.AnimationClip) {
  // Reuse the archive validator without changing resource ownership.
  const holder = root.animations;
  try { root.animations = [clip]; saveAnimationClips(root); } finally { root.animations = holder; }
  if (clip.tracks.some(t => THREE.PropertyBinding.parseTrackName(t.name).propertyName === 'scale')) fail('Animated scales are unsupported');
  // Never evaluate unsaved root/non-bone channels behind the archive validator.
  if (clip.tracks.some(t => !(THREE.PropertyBinding.findNode(root, THREE.PropertyBinding.parseTrackName(t.name).nodeName) instanceof THREE.Bone))) fail('Non-bone animation tracks are unsupported');
  return clip;
}
function sampler(root: THREE.Object3D, clip: THREE.AnimationClip) {
  const mixer = new THREE.AnimationMixer(root);
  let action = mixer.clipAction(clip); action.setLoop(THREE.LoopOnce, 1); action.clampWhenFinished = true;
  return { at(t: number) {
    action.reset().play(); mixer.setTime(t);
    unitScales(root);
    const evaluated = snapshot(root);
    mixer.stopAllAction(); mixer.uncacheRoot(root);
    evaluated();
    // Rebinding clears PropertyMixer's cached values before the next absolute seek.
    action = mixer.clipAction(clip); action.setLoop(THREE.LoopOnce, 1); action.clampWhenFinished = true;
  }, stop() { mixer.stopAllAction(); mixer.uncacheRoot(root); } };
}
const pos = (n: THREE.Object3D) => n.getWorldPosition(new THREE.Vector3());
const quat = (n: THREE.Object3D) => n.getWorldQuaternion(new THREE.Quaternion());
function worldRotation(n: THREE.Object3D, q: THREE.Quaternion) { n.quaternion.copy(n.parent ? quat(n.parent).invert().multiply(q) : q); n.updateWorldMatrix(false, true); }
function aim(n: THREE.Object3D, from: THREE.Vector3, to: THREE.Vector3) {
  worldRotation(n, new THREE.Quaternion().setFromUnitVectors(from.normalize(), to.normalize()).multiply(quat(n)));
}
/** Geometric projection on every sample, NOT a stance/contact lock. No pelvis movement.
 * Fade is absolute: secondary weight = weight * min(seconds/fadeSeconds,1).
 * Clips loop independently; exact duration boundaries wrap to zero. Missing channels use captured pose.
 */
export function createCharacterMotionRuntime(root: THREE.Object3D, primary?: SavedMoveAnimation, settings?: CharacterMotionSettings) {
  const config = validateCharacterMotionSettings(settings ?? { version: 1 });
  unitScales(root); const named = bones(root); const restore = snapshot(root);
  const resolve = (ref: SavedMoveAnimation) => checkedClip(root, root.animations[restoreMoveAnimation(root.animations, ref)]);
  const pClip = primary ? resolve(primary) : undefined, bClip = config.blend ? resolve(config.blend.animation) : undefined;
  const legs = config.feet ? [config.feet.left, config.feet.right].map(l => {
    const h = named.get(l.hip), k = named.get(l.knee), a = named.get(l.ankle);
    if (!h || !k || !a || k.parent !== h || a.parent !== k || k.position.length() < 1e-6 || a.position.length() < 1e-6) return fail('Unsupported leg: require direct nonzero hip/knee/ankle chain');
    return { h, k, a };
  }) : [];
  const p = pClip ? sampler(root, pClip) : undefined, b = bClip ? sampler(root, bClip) : undefined;
  let stopped = false;
  return {
    sampleAt(seconds: number, groundHeight: (x: number, z: number) => number) {
      if (stopped) fail('Motion runtime stopped');
      if (!finite(seconds) || seconds < 0) fail('Invalid sample time');
      restore();
      try {
        if (p && pClip) p.at(seconds % pClip.duration);
        const pose: { n: THREE.Object3D; p: THREE.Vector3; q: THREE.Quaternion }[] = [];
        root.traverse(n => pose.push({ n, p: n.position.clone(), q: n.quaternion.clone() }));
        if (b && bClip && config.blend) {
          restore(); b.at(seconds % bClip.duration);
          const w = config.blend.weight * (config.blend.fadeSeconds === 0 ? 1 : Math.min(1, seconds / config.blend.fadeSeconds));
          for (const v of pose) { v.n.position.lerpVectors(v.p, v.n.position, w); v.n.quaternion.copy(v.q.clone().slerp(v.n.quaternion, w)); }
        }
        root.updateWorldMatrix(true, true);
        for (const { h, k, a } of legs) {
          const hp = pos(h), kp = pos(k), ap = pos(a), target = ap.clone();
          const y = groundHeight(ap.x, ap.z);
          if (!finite(y)) fail('Terrain height is nonfinite');
          target.y = y + config.feet!.soleOffset;
          if (Math.abs(target.y - ap.y) > config.feet!.maxCorrection) fail('Foot correction exceeds maxCorrection');
          const upper = hp.distanceTo(kp), lower = kp.distanceTo(ap), d = hp.distanceTo(target);
          if (d <= Math.abs(upper - lower) + 1e-7 || d > upper + lower + 1e-6) fail('Foot target unreachable or singular');
          const axis = target.clone().sub(hp).normalize();
          const pole = kp.clone().sub(hp).addScaledVector(axis, -kp.clone().sub(hp).dot(axis));
          if (pole.length() < 1e-7) {
            // Fully extended standing rigs have no bend plane yet. Use their
            // calibrated local forward (+Z), or +X if parallel, deterministically.
            pole.set(0,0,1).applyQuaternion(quat(h)); pole.addScaledVector(axis,-pole.dot(axis));
            if (pole.length() < 1e-7) { pole.set(1,0,0).applyQuaternion(quat(h)); pole.addScaledVector(axis,-pole.dot(axis)); }
          }
          const reachable = Math.min(d, upper + lower);
          const along = (upper * upper - lower * lower + reachable * reachable) / (2 * reachable);
          const kneeTarget = hp.clone().addScaledVector(axis, along).addScaledVector(pole.normalize(), Math.sqrt(Math.max(0, upper * upper - along * along)));
          const ankleQ = quat(a);
          aim(h, kp.clone().sub(hp), kneeTarget.clone().sub(hp));
          aim(k, pos(a).sub(pos(k)), target.clone().sub(pos(k)));
          // Surface normal estimated from deterministic central differences; local +Y is sole-up.
          const e = 0.001, heights = [groundHeight(target.x-e,target.z), groundHeight(target.x+e,target.z), groundHeight(target.x,target.z-e), groundHeight(target.x,target.z+e)];
          if (!heights.every(finite)) fail('Terrain normal is nonfinite');
          const normal = new THREE.Vector3(heights[0]-heights[1], 2*e, heights[2]-heights[3]).normalize();
          worldRotation(a, new THREE.Quaternion().setFromUnitVectors(new THREE.Vector3(0,1,0).applyQuaternion(ankleQ), normal).multiply(ankleQ));
        }
      } catch (error) { restore(); throw error; }
    },
    stop() { if (stopped) return; stopped = true; p?.stop(); b?.stop(); restore(); },
  };
}

/** Caller supplies BOTH rigs in their calibrated rest pose. No inferred mapping or provenance.
 * Bake world orientation deltas onto target rest; translations use mapped local rest frames
 * and target/source parent-segment length ratio. Unit-scale rigs only; bounded 30fps bake.
 */
export function retargetSkeletalClip(sourceRoot: THREE.Object3D, targetRoot: THREE.Object3D, clip: THREE.AnimationClip, mapping: Record<string,string>, name: string): THREE.AnimationClip {
  const overlaps = (a: THREE.Object3D, b: THREE.Object3D) => { let found = false; a.traverse(n => { if (n === b) found = true; }); return found; };
  if (overlaps(sourceRoot,targetRoot) || overlaps(targetRoot,sourceRoot) || !name.trim()) fail('Choose disjoint rigs and a clip name');
  unitScales(sourceRoot); unitScales(targetRoot);
  const source = bones(sourceRoot), target = bones(targetRoot);
  checkedClip(sourceRoot, clip);
  const pairs = Object.entries(mapping).map(([s,t]) => {
    const a = source.get(s), b = target.get(t); if (!a || !b) return fail('Mapping must use existing unique bone names');
    return { a, b, sp: a.position.clone(), tp: b.position.clone(), sq: quat(a), tq: quat(b), parentQ: a.parent ? quat(a.parent) : new THREE.Quaternion(), targetParentQ: b.parent ? quat(b.parent) : new THREE.Quaternion() };
  });
  if (!pairs.length || new Set(pairs.map(p => p.b)).size !== pairs.length) fail('Empty or duplicate target mapping');
  for (const track of clip.tracks) {
    const binding = THREE.PropertyBinding.parseTrackName(track.name), node = THREE.PropertyBinding.findNode(sourceRoot, binding.nodeName);
    if (!pairs.some(p => p.a === node)) fail('Unmapped animated source bone');
  }
  const frames = Math.ceil(clip.duration * 30) + 1;
  if (!Number.isSafeInteger(frames) || frames > 18001 || frames * pairs.length * 7 > 2_000_000) fail('Retarget bake exceeds resource budget');
  // Parent-first evaluation is essential when mapping JSON has arbitrary key order.
  const depth = (n: THREE.Object3D): number => n.parent ? 1 + depth(n.parent) : 0;
  pairs.sort((a,b) => depth(a.b)-depth(b.b));
  const restoreSource = snapshot(sourceRoot), restoreTarget = snapshot(targetRoot), play = sampler(sourceRoot, clip);
  const times: number[] = [], data = pairs.map(() => ({ p: [] as number[], q: [] as number[] }));
  try {
    for (let i=0;i<frames;i++) {
      const t = i * clip.duration / (frames-1); times.push(t); restoreSource(); restoreTarget(); play.at(t);
      pairs.forEach((p,j) => {
        const delta = quat(p.a).multiply(p.sq.clone().invert());
        worldRotation(p.b, delta.multiply(p.tq));
        const sl = p.sp.length(), tl = p.tp.length();
        if ((sl < 1e-7) !== (tl < 1e-7)) fail('Incompatible translation calibration');
        const offset = p.a.position.clone().sub(p.sp).applyQuaternion(p.parentQ).multiplyScalar(sl < 1e-7 ? 1 : tl/sl).applyQuaternion(p.targetParentQ.clone().invert());
        p.b.position.copy(p.tp).add(offset); p.b.updateWorldMatrix(false,true);
        data[j].p.push(...p.b.position); data[j].q.push(...p.b.quaternion);
      });
    }
    const tracks = pairs.flatMap((p,j) => [new THREE.VectorKeyframeTrack(`${p.b.uuid}.position`, times, data[j].p), new THREE.QuaternionKeyframeTrack(`${p.b.uuid}.quaternion`, times, data[j].q)]);
    const result = new THREE.AnimationClip(name,clip.duration,tracks); checkedClip(targetRoot,result); return result;
  } finally { play.stop(); restoreSource(); restoreTarget(); }
}

/** Explicit local surface anchors, not inferred hand/wrist centres. This IK primitive
 * does NOT authorize collision exceptions, a closed finger grip or balance recovery. */
export interface WristContactBinding {
  version: 1;
  arm: { shoulder: string; elbow: string; wrist: string };
  targetBone: string;
  sourceAnchor: [number, number, number];
  targetAnchor: [number, number, number];
  /** Desired source wrist orientation relative to the target bone (xyzw). */
  rotation: [number, number, number, number];
  /** Authored world-space bend direction; must not be parallel to the reach. */
  pole: [number, number, number];
}
export function validateWristContactBinding(value: unknown): WristContactBinding {
  const record = (v: unknown, allowed: string[]) => {
    if (!v || typeof v !== 'object' || Array.isArray(v) || Object.keys(v).some(k => !allowed.includes(k))) fail('手腕接触配置无效');
    return v as Record<string, unknown>;
  };
  const v = record(value, ['version', 'arm', 'targetBone', 'sourceAnchor', 'targetAnchor', 'rotation', 'pole']);
  const a = record(v.arm, ['shoulder', 'elbow', 'wrist']);
  const names = [a.shoulder, a.elbow, a.wrist, v.targetBone];
  if (v.version !== 1 || names.some(n => typeof n !== 'string' || !n.trim() || n.length > 160) || new Set(names.slice(0, 3)).size !== 3) fail('手腕接触须指定实际骨骼名称');
  const vector = (p: unknown, length: number, limit: number) => {
    if (!Array.isArray(p) || p.length !== length || p.some(n => !finite(n) || Math.abs(n) > limit)) fail('手腕接触锚点或旋转无效');
    return [...p as number[]];
  };
  const sourceAnchor = vector(v.sourceAnchor, 3, .25), targetAnchor = vector(v.targetAnchor, 3, .25);
  const rotation = vector(v.rotation, 4, 1), pole = vector(v.pole, 3, 1);
  if (Math.abs(new THREE.Quaternion().fromArray(rotation).lengthSq() - 1) > 1e-6 || new THREE.Vector3().fromArray(pole).length() < 1e-6) fail('手腕接触需要单位旋转与非零肘向');
  return { version: 1, arm: { shoulder: a.shoulder as string, elbow: a.elbow as string, wrist: a.wrist as string },
    targetBone: v.targetBone as string, sourceAnchor: sourceAnchor as [number,number,number], targetAnchor: targetAnchor as [number,number,number],
    rotation: rotation as [number,number,number,number], pole: pole as [number,number,number] };
}
function contactBones(source: THREE.Object3D, target: THREE.Object3D, binding: WristContactBinding) {
  let overlap = false;
  source.traverse(n => { if (n === target) overlap = true; }); target.traverse(n => { if (n === source) overlap = true; });
  if (overlap) fail('手腕接触必须绑定两个独立角色');
  unitScales(source); unitScales(target);
  const s = bones(source), t = bones(target);
  const shoulder = s.get(binding.arm.shoulder), elbow = s.get(binding.arm.elbow), wrist = s.get(binding.arm.wrist), anchor = t.get(binding.targetBone);
  if (!shoulder || !elbow || !wrist || !anchor || elbow.parent !== shoulder || wrist.parent !== elbow || elbow.position.length() < 1e-6 || wrist.position.length() < 1e-6) fail('手腕接触骨链无效或目标骨骼缺失');
  return { shoulder: shoulder!, elbow: elbow!, wrist: wrist!, anchor: anchor! };
}
/** Solve the already evaluated pose of TWO characters in world space. No translations,
 * scale changes or target mutations. Unreachable/singular results atomically restore the
 * source. Caller must still prove whole-body/environment collision safety. */
export function solveWristContact(source: THREE.Object3D, target: THREE.Object3D, value: WristContactBinding) {
  const binding = validateWristContactBinding(value), { shoulder, elbow, wrist, anchor } = contactBones(source, target, binding);
  const restore = snapshot(source);
  try {
    const targetPoint = anchor.localToWorld(new THREE.Vector3().fromArray(binding.targetAnchor));
    const wristQ = quat(anchor).multiply(new THREE.Quaternion().fromArray(binding.rotation));
    const wristTarget = targetPoint.clone().sub(new THREE.Vector3().fromArray(binding.sourceAnchor).applyQuaternion(wristQ));
    const hp = pos(shoulder), ep = pos(elbow), wp = pos(wrist);
    const upper = hp.distanceTo(ep), lower = ep.distanceTo(wp), distance = hp.distanceTo(wristTarget);
    if (!finite(distance) || distance <= Math.abs(upper - lower) + 1e-7 || distance > upper + lower + 1e-7) fail('手腕接触目标不可达，禁止拉伸骨链');
    const axis = wristTarget.clone().sub(hp).normalize(), pole = new THREE.Vector3().fromArray(binding.pole);
    pole.addScaledVector(axis, -pole.dot(axis));
    if (pole.length() < 1e-6) fail('手腕接触肘向与伸臂方向退化');
    const reach = Math.min(distance, upper + lower), along = (upper * upper - lower * lower + reach * reach) / (2 * reach);
    const elbowTarget = hp.clone().addScaledVector(axis, along).addScaledVector(pole.normalize(), Math.sqrt(Math.max(0, upper * upper - along * along)));
    aim(shoulder, ep.clone().sub(hp), elbowTarget.clone().sub(hp));
    aim(elbow, pos(wrist).sub(pos(elbow)), wristTarget.clone().sub(pos(elbow)));
    worldRotation(wrist, wristQ);
    const sourcePoint = wrist.localToWorld(new THREE.Vector3().fromArray(binding.sourceAnchor));
    const errorMetres = sourcePoint.distanceTo(targetPoint);
    if (!finite(errorMetres) || errorMetres > 1e-5 || quat(wrist).angleTo(wristQ) > 1e-5) fail('手腕接触残差超限，已还原姿态');
    return { errorMetres, sourcePoint: sourcePoint.toArray() as [number,number,number], targetPoint: targetPoint.toArray() as [number,number,number] };
  } catch (error) { restore(); throw error; }
}
/** Independent absolute-time laboratory/bake primitive. Both base poses are evaluated
 * BEFORE the cross-character solve; actor order cannot change the target. Restores both
 * editing poses after any failure and on stop. Not a playback collision exemption. */
export function createWristContactRuntime(source: THREE.Object3D, target: THREE.Object3D, value: WristContactBinding) {
  const binding = validateWristContactBinding(value), initial = contactBones(source, target, binding);
  const identities = [initial.shoulder, initial.elbow, initial.wrist, initial.anchor];
  const parents = identities.map(n => n.parent), lengths = [initial.elbow.position.length(), initial.wrist.position.length()];
  const restoreSource = snapshot(source), restoreTarget = snapshot(target);
  const restore = () => { restoreSource(); restoreTarget(); };
  let stopped = false;
  return {
    sampleAt(seconds: number, evaluateBasePoses: (seconds: number) => void) {
      if (stopped) fail('手腕接触会话已停止');
      if (!finite(seconds) || seconds < 0) fail('手腕接触取样时间无效');
      restore();
      try {
        const currentSource = bones(source), currentTarget = bones(target);
        const current = [currentSource.get(binding.arm.shoulder), currentSource.get(binding.arm.elbow), currentSource.get(binding.arm.wrist), currentTarget.get(binding.targetBone)];
        if (current.some((n, i) => n !== identities[i] || n?.parent !== parents[i])) fail('手腕接触骨骼绑定已变化');
        evaluateBasePoses(seconds);
        const afterSource = bones(source), afterTarget = bones(target);
        const after = [afterSource.get(binding.arm.shoulder), afterSource.get(binding.arm.elbow), afterSource.get(binding.arm.wrist), afterTarget.get(binding.targetBone)];
        if (after.some((n, i) => n !== identities[i] || n?.parent !== parents[i])) fail('手腕接触骨骼绑定已变化');
        if (Math.abs(initial.elbow.position.length() - lengths[0]) > 1e-6 || Math.abs(initial.wrist.position.length() - lengths[1]) > 1e-6) fail('手腕接触动作改变骨长');
        return solveWristContact(source, target, binding);
      } catch (error) { restore(); throw error; }
    },
    stop() { if (stopped) return; stopped = true; restore(); },
  };
}

/** A deliberately restrictive certificate, NOT a hand-region collision exemption.
 * Every visible posed vertex (including clothes/decorations) must be on its actor's
 * side of a plane parallel to the authored wrist normal, located in the actual
 * gap between the two measured convex supports. Pinning that plane to a near-skin
 * (not on-skin) anchor falsely rejects separated elbows. The full convex hulls must
 * still be strictly disjoint; no vertex, body part or clothing gets an exemption.
 * Anchors must also be within 2mm of actual surface triangles; an air anchor cannot
 * make a floating "grip" pass. All tolerances/budgets are fixed, not authored. */
export function assertWristContactSurface(source: THREE.Object3D, target: THREE.Object3D, value: WristContactBinding, localNormal: readonly number[]) {
  const binding = validateWristContactBinding(value), { wrist, anchor } = contactBones(source, target, binding);
  if (!Array.isArray(localNormal) || localNormal.length !== 3 || !localNormal.every(finite) || Math.abs(new THREE.Vector3().fromArray(localNormal).lengthSq() - 1) > 1e-6) fail('手腕表面需要单位局部法线');
  const point = anchor.localToWorld(new THREE.Vector3().fromArray(binding.targetAnchor));
  const sourcePoint = wrist.localToWorld(new THREE.Vector3().fromArray(binding.sourceAnchor));
  if (sourcePoint.distanceTo(point) > 1e-5) fail('手腕接触残差超限');
  const normal = new THREE.Vector3().fromArray(localNormal).applyQuaternion(quat(anchor));
  let vertices = 0, triangles = 0;
  const measure = (root: THREE.Object3D) => {
    // SkinnedMesh.updateMatrixWorld also refreshes bindMatrixInverse. Calling only
    // updateWorldMatrix would incorrectly double a translated rig's skin offset.
    root.updateWorldMatrix(true, false); root.updateMatrixWorld(true);
    let minimum = Infinity, maximum = -Infinity, nearest = Infinity, count = 0;
    const visit = (n: THREE.Object3D) => {
      if (!n.visible) return;
      if (n instanceof THREE.Mesh) {
        const materials = Array.isArray(n.material) ? n.material : [n.material];
        if (materials.some(m => m.visible && m.opacity > 0)) {
          if (n instanceof THREE.InstancedMesh || materials.some(m => m instanceof THREE.ShaderMaterial || !!(m as THREE.MeshStandardMaterial).displacementMap)) fail('手腕接触不支持实例或着色器位移几何');
          const p = n.geometry.getAttribute('position'), index = n.geometry.index;
          if (!p || (index?.count ?? p.count) % 3 || !Number.isSafeInteger(p.count) || (vertices += p.count) > 100000) fail('手腕表面几何缺失或超出验证预算');
          const world: THREE.Vector3[] = [];
          for (let i = 0; i < p.count; i++) {
            const v = n.getVertexPosition(i, new THREE.Vector3()).applyMatrix4(n.matrixWorld);
            if (![...v].every(finite)) fail('手腕表面顶点必须有限');
            world.push(v); const d = v.clone().sub(point).dot(normal);
            minimum = Math.min(minimum, d); maximum = Math.max(maximum, d);
          }
          for (let i = 0; i < (index?.count ?? p.count); i += 3) {
            if (++triangles > 150000) fail('手腕表面三角形超出验证预算');
            const ids = [0, 1, 2].map(k => index?.getX(i + k) ?? i + k);
            if (ids.some(id => !Number.isSafeInteger(id) || id < 0 || id >= world.length)) fail('手腕表面索引无效');
            const triangle = new THREE.Triangle(world[ids[0]], world[ids[1]], world[ids[2]]);
            // Sphere/capsule caps include zero-area triangles; their vertices still
            // participate in the plane proof, but cannot define a contact surface.
            if (triangle.getArea() <= 1e-14) continue;
            nearest = Math.min(nearest, triangle.closestPointToPoint(point, new THREE.Vector3()).distanceTo(point)); count++;
          }
        }
      }
      n.children.forEach(visit);
    };
    visit(root);
    if (!count || !finite(nearest)) fail('手腕接触缺少可验证的真实表面');
    return { minimum, maximum, nearest };
  };
  const a = measure(source), b = measure(target);
  if (a.nearest > .002 || b.nearest > .002) fail('手腕锚点不在真实表面2毫米范围内');
  const separationMetres = a.minimum - b.maximum;
  // This is a geometric support-interval proof, not a relaxed penetration epsilon.
  // Require a strictly positive gap, even when both supports lie on the same side
  // of the authored (up to 2mm off-surface) contact anchor.
  if (separationMetres <= 1e-7) fail('手腕接触完整蒙皮无法通过分离平面验证');
  const planeOffsetMetres = (a.minimum + b.maximum) / 2;
  return { version: 1 as const, policy: 'whole-skin-separating-plane-v1' as const, vertices, triangles, planeOffsetMetres, separationMetres,
    sourceMinimum: a.minimum - planeOffsetMetres, targetMaximum: b.maximum - planeOffsetMetres, sourceSurfaceError: a.nearest, targetSurfaceError: b.nearest };
}
