import * as THREE from 'three';

/** Only bone-local TRS is previewable: never animate the editor wrapper or materials. */
export function skeletalClips(root: THREE.Object3D, clips: THREE.AnimationClip[]): THREE.AnimationClip[] {
  return clips.flatMap(clip => {
    if (!Number.isFinite(clip.duration) || clip.duration <= 0) return [];
    const tracks = clip.tracks.filter(track => {
      try {
        const binding = THREE.PropertyBinding.parseTrackName(track.name);
        if (binding.objectName || binding.propertyIndex !== undefined || !['position', 'quaternion', 'scale'].includes(binding.propertyName)) return false;
        const node = THREE.PropertyBinding.findNode(root, binding.nodeName);
        return node instanceof THREE.Bone;
      } catch { return false; }
    });
    return tracks.length ? [new THREE.AnimationClip(clip.name, clip.duration, tracks.map(t => t.clone()))] : [];
  });
}

export interface SavedMoveAnimation { index: number; name: string; duration: number }

export function saveMoveAnimation(clips: THREE.AnimationClip[], index: number): SavedMoveAnimation | undefined {
  if (index === -1) return undefined;
  const clip = clips[index];
  if (!Number.isSafeInteger(index) || index < 0 || !clip || !Number.isFinite(clip.duration) || clip.duration <= 0) throw new Error('移动动作已失效，请重新选择');
  return { index, name: clip.name, duration: clip.duration };
}

export function restoreMoveAnimation(clips: THREE.AnimationClip[], value: unknown): number {
  if (value === undefined) return -1;
  const ref = value as SavedMoveAnimation | null;
  if (!ref || !Number.isSafeInteger(ref.index) || ref.index < 0 || typeof ref.name !== 'string' || !Number.isFinite(ref.duration) || ref.duration <= 0 ||
    !clips[ref.index] || clips[ref.index].name !== ref.name || clips[ref.index].duration !== ref.duration) {
    throw new Error('路径动作引用无效或与目标资源不匹配，场景未替换');
  }
  return ref.index;
}

const isGLTFCubic = (track: THREE.KeyframeTrack) => Boolean((track.createInterpolant as unknown as { isInterpolantFactoryMethodGLTFCubicSpline?: boolean }).isInterpolantFactoryMethodGLTFCubicSpline);

// glTF Hermite layout/semantics follow Three.js GLTFLoader (MIT).
class ArchiveCubicInterpolant extends THREE.Interpolant {
  constructor(times: ArrayLike<number>, values: ArrayLike<number>, width: number, result: any, private quaternion: boolean) {
    super(times, values, width, result);
  }
  copySampleValue_(index: number) {
    const offset = index * this.valueSize * 3 + this.valueSize;
    for (let i = 0; i < this.valueSize; i++) this.resultBuffer[i] = this.sampleValues[offset + i];
    return this.resultBuffer;
  }
  interpolate_(i1: number, t0: number, t: number, t1: number) {
    const dt = t1 - t0, p = (t - t0) / dt, pp = p * p, ppp = pp * p;
    const s2 = -2 * ppp + 3 * pp, s3 = ppp - pp, s0 = 1 - s2, s1 = s3 - pp + p;
    const w = this.valueSize, a = (i1 - 1) * w * 3, b = i1 * w * 3, v = this.sampleValues;
    for (let i = 0; i < w; i++) this.resultBuffer[i] = s0 * v[a + w + i] + s1 * v[a + 2 * w + i] * dt + s2 * v[b + w + i] + s3 * v[b + i] * dt;
    if (this.quaternion) new THREE.Quaternion().fromArray(this.resultBuffer).normalize().toArray(this.resultBuffer);
    return this.resultBuffer;
  }
}

function installGLTFCubic(track: THREE.KeyframeTrack, width: number, quaternion: boolean) {
  // Use `this`: KeyframeTrack.clone copies the factory, never the original typed arrays.
  const factory = function(this: THREE.KeyframeTrack, result: any) { return new ArchiveCubicInterpolant(this.times, this.values, width, result, quaternion); };
  Object.assign(factory, { isInterpolantFactoryMethodGLTFCubicSpline: true });
  track.createInterpolant = factory;
}

export interface SavedAnimationClip {
  name: string;
  duration: number;
  tracks: { path: number[]; property: 'position' | 'quaternion' | 'scale'; interpolation: number | 'CUBICSPLINE'; times: number[]; values: number[] }[];
}

/** Archive resource data only. Child paths remain local even when glTF renames duplicate bones. */
export function saveAnimationClips(root: THREE.Object3D): SavedAnimationClip[] {
  const paths = new Map<THREE.Object3D, number[]>();
  const visit = (node: THREE.Object3D, path: number[]) => { paths.set(node, path); node.children.forEach((child, i) => visit(child, [...path, i])); };
  visit(root, []);
  const saved = root.animations.map(clip => ({
    name: clip.name, duration: clip.duration,
    tracks: clip.tracks.map(track => {
      const binding = THREE.PropertyBinding.parseTrackName(track.name);
      const bone = THREE.PropertyBinding.findNode(root, binding.nodeName);
      if (!(bone instanceof THREE.Bone) || !paths.has(bone) || binding.objectName || binding.propertyIndex !== undefined ||
        !['position', 'quaternion', 'scale'].includes(binding.propertyName)) {
        throw new Error('动作轨道无法完整保存：骨骼绑定或插值不受支持');
      }
      return { path: paths.get(bone)!, property: binding.propertyName as 'position' | 'quaternion' | 'scale', interpolation: isGLTFCubic(track) ? 'CUBICSPLINE' as const : track.getInterpolation(), times: Array.from(track.times), values: Array.from(track.values) };
    }),
  }));
  restoreAnimationClips(root, saved); // Fail saving rather than silently losing invalid resources.
  return saved;
}

/** Strict and atomic: caller assigns returned clips only after every binding validates. */
export function restoreAnimationClips(root: THREE.Object3D, data: unknown): THREE.AnimationClip[] {
  if (data === undefined) return [];
  const fail = (): never => { throw new Error('场景动作资源无效或骨骼绑定不兼容，场景未替换'); };
  if (!Array.isArray(data) || data.length > 256) return fail();
  let samples = 0;
  return data.map(clip => {
    if (!clip || typeof clip.name !== 'string' || clip.name.length > 1000 || !Number.isFinite(clip.duration) || clip.duration <= 0 ||
      !Array.isArray(clip.tracks) || !clip.tracks.length || clip.tracks.length > 4096) return fail();
    const bindings = new Set<string>();
    const tracks = clip.tracks.map((track: SavedAnimationClip['tracks'][number]) => {
      if (!track || !Array.isArray(track.path) || track.path.length > 256 || !track.path.every(i => Number.isSafeInteger(i) && i >= 0) ||
        !['position', 'quaternion', 'scale'].includes(track.property) || !['CUBICSPLINE', THREE.InterpolateDiscrete, THREE.InterpolateLinear, THREE.InterpolateSmooth].includes(track.interpolation) ||
        (track.property === 'quaternion' && track.interpolation === THREE.InterpolateSmooth)) return fail();
      let bone = root;
      for (const i of track.path) { if (!bone.children[i]) return fail(); bone = bone.children[i]; }
      const key = `${bone.uuid}.${track.property}`;
      if (!(bone instanceof THREE.Bone) || bindings.has(key)) return fail();
      bindings.add(key);
      if (!Array.isArray(track.times) || !Array.isArray(track.values) || !track.times.length) return fail();
      samples += track.times.length + track.values.length;
      const width = track.property === 'quaternion' ? 4 : 3;
      if (samples > 2_000_000 || track.values.length !== track.times.length * width * (track.interpolation === 'CUBICSPLINE' ? 3 : 1) ||
        !track.values.every(n => typeof n === 'number' && Number.isFinite(n) && Number.isFinite(Math.fround(n))) ||
        !track.times.every((n, i) => typeof n === 'number' && Number.isFinite(n) && n >= 0 && n <= clip.duration &&
          Number.isFinite(Math.fround(n)) && (!i || Math.fround(n) > Math.fround(track.times[i - 1])))) return fail();
      const Type = track.property === 'quaternion' ? THREE.QuaternionKeyframeTrack : THREE.VectorKeyframeTrack;
      const restored = new Type(key, track.times, track.values, track.interpolation === 'CUBICSPLINE' ? THREE.InterpolateLinear : track.interpolation);
      if (track.interpolation === 'CUBICSPLINE') installGLTFCubic(restored, width, track.property === 'quaternion');
      return restored;
    });
    return new THREE.AnimationClip(clip.name, clip.duration, tracks);
  });
}

/** Single-owner runtime preview. stop() restores the exact editing pose. */
export class AnimationPreview {
  readonly mixer: THREE.AnimationMixer;
  private snapshots: { bone: THREE.Bone; p: THREE.Vector3; q: THREE.Quaternion; s: THREE.Vector3 }[] = [];
  private action: THREE.AnimationAction;
  private running = true;
  private disposed = false;

  constructor(readonly root: THREE.Object3D, clip: THREE.AnimationClip) {
    const safe = skeletalClips(root, [clip])[0];
    if (!safe) throw new Error('此片段没有可预览的骨骼局部变换轨道');
    root.traverse(node => {
      if (node instanceof THREE.Bone) this.snapshots.push({ bone: node, p: node.position.clone(), q: node.quaternion.clone(), s: node.scale.clone() });
    });
    this.mixer = new THREE.AnimationMixer(root);
    this.action = this.mixer.clipAction(safe);
    this.action.setLoop(THREE.LoopRepeat, Infinity).play();
    this.mixer.update(0);
  }

  pause() { this.running = false; }
  resume() { if (!this.disposed) this.running = true; }
  update(delta: number) {
    if (!this.disposed && this.running && Number.isFinite(delta) && delta >= 0) {
      this.mixer.update(delta);
      this.root.updateMatrixWorld(true);
    }
  }
  /** Absolute timeline sampling, independent of preceding frames or render fps. */
  sampleAt(seconds: number) {
    if (this.disposed || !Number.isFinite(seconds) || seconds < 0) return;
    this.mixer.setTime(seconds);
    this.root.updateMatrixWorld(true);
  }
  stop() {
    if (this.disposed) return;
    this.disposed = true;
    this.running = false;
    this.mixer.stopAllAction();
    this.mixer.uncacheRoot(this.root);
    for (const { bone, p, q, s } of this.snapshots) {
      bone.position.copy(p); bone.quaternion.copy(q); bone.scale.copy(s);
    }
    this.root.updateMatrixWorld(true);
    this.snapshots = [];
  }
}
