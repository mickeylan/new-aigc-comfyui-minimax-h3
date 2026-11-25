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
