import { describe, it, expect } from 'vitest';
import * as THREE from 'three';
import { AnimationPreview, skeletalClips, saveAnimationClips, restoreAnimationClips, saveMoveAnimation, restoreMoveAnimation } from './animationPreview';

describe('glTF cubic archive semantics', () => {
  it('matches GLTFLoader at boundaries/interior/non-monotonic times without losing tangents', async () => {
    const { GLTFLoader } = await import('three/examples/jsm/loaders/GLTFLoader.js');
    const times = [0, 2, 5];
    const position = [0,0,0, 0,1,0, 1,2,0, 0,1,0, 2,3,0, -1,0,0, 1,0,0, 3,1,0, 0,0,0];
    const rotation = [0,0,0,0, 0,0,0,1, 0,.2,0,0, 0,.1,0,0, 0,.70710677,0,.70710677, 0,0,0,0, 0,0,0,0, 0,1,0,0, 0,0,0,0];
    const data = new Float32Array([...times, ...position, ...rotation]);
    const json = { asset: { version: '2.0' }, scene: 0, scenes: [{ nodes: [0] }], nodes: [{ name: 'Joint' }], skins: [{ joints: [0] }],
      buffers: [{ byteLength: data.byteLength, uri: 'data:application/octet-stream;base64,' + Buffer.from(data.buffer).toString('base64') }],
      bufferViews: [{ buffer: 0, byteOffset: 0, byteLength: 12 }, { buffer: 0, byteOffset: 12, byteLength: position.length * 4 }, { buffer: 0, byteOffset: (3 + position.length) * 4, byteLength: rotation.length * 4 }],
      accessors: [{ bufferView: 0, componentType: 5126, count: 3, type: 'SCALAR', min: [0], max: [5] }, { bufferView: 1, componentType: 5126, count: 9, type: 'VEC3' }, { bufferView: 2, componentType: 5126, count: 9, type: 'VEC4' }],
      animations: [{ name: 'Cubic', samplers: [{ input: 0, output: 1, interpolation: 'CUBICSPLINE' }, { input: 0, output: 2, interpolation: 'CUBICSPLINE' }], channels: [{ sampler: 0, target: { node: 0, path: 'translation' } }, { sampler: 1, target: { node: 0, path: 'rotation' } }] }] };
    const gltf = await new GLTFLoader().parseAsync(JSON.stringify(json), '');
    gltf.scene.animations = skeletalClips(gltf.scene, gltf.animations);
    expect(gltf.scene.animations[0].tracks).toHaveLength(2);
    const saved = JSON.parse(JSON.stringify(saveAnimationClips(gltf.scene)));
    expect(saved[0].tracks.every(t => t.interpolation === 'CUBICSPLINE')).toBe(true);
    const restored = restoreAnimationClips(gltf.scene, saved);
    expect(saveAnimationClips(Object.assign(gltf.scene, { animations: restored }))).toEqual(saved);
    for (let i = 0; i < 2; i++) {
      const original = gltf.animations[0].tracks[i].createInterpolant();
      const roundtrip = restored[0].tracks[i].clone().createInterpolant();
      for (const time of [-1, 0, .25, 1, 2, 3.5, 5, 7, .5]) {
        const a = Array.from(original.evaluate(time)), b = Array.from(roundtrip.evaluate(time));
        b.forEach((v, j) => expect(v).toBeCloseTo(a[j], 6));
      }
    }
    const bad = structuredClone(saved); bad[0].tracks[0].values.pop();
    expect(() => restoreAnimationClips(gltf.scene, bad)).toThrow();
    const preview = new AnimationPreview(gltf.scene, restored[0]); preview.sampleAt(1); preview.stop();
  });
});

describe('path action selection reference', () => {
  it('round trips the selected index even for duplicate clip names; legacy remains none', () => {
    const clips = [fixture().clip, fixture().clip];
    expect(restoreMoveAnimation(clips, saveMoveAnimation(clips, 1))).toBe(1);
    expect(saveMoveAnimation(clips, -1)).toBeUndefined();
    expect(restoreMoveAnimation(clips, undefined)).toBe(-1);
  });
  it('rejects invalid indices and mismatched resources without a first-clip fallback', () => {
    const clips = [fixture().clip];
    for (const i of [-2, .5, NaN, 1]) expect(() => saveMoveAnimation(clips, i)).toThrow();
    const ref = saveMoveAnimation(clips, 0)!;
    for (const value of [null, {}, { ...ref, index: -1 }, { ...ref, index: 1 }, { ...ref, name: 'other' }, { ...ref, duration: 3 }]) {
      expect(() => restoreMoveAnimation(clips, value)).toThrow();
    }
    expect(() => restoreMoveAnimation([], ref)).toThrow();
  });
});

describe('archive animation resources', () => {
  it('round trips resources through JSON and binds UUIDs on a renamed skeleton', () => {
    const a = fixture(); a.root.animations = [a.clip];
    const data = JSON.parse(JSON.stringify(saveAnimationClips(a.root)));
    const b = fixture(); b.bone.name = 'Hips_renamed_by_exporter';
    b.root.animations = restoreAnimationClips(b.root, data);
    const preview = new AnimationPreview(b.root, b.root.animations[0]); preview.sampleAt(.5);
    expect(b.bone.position.y).toBeCloseTo(1); expect(a.bone.position.y).toBe(2); preview.stop();
    expect(saveAnimationClips(b.root)).toEqual(data);
  });
  it('keeps duplicate names on separate actors independent and legacy resources empty', () => {
    const a = fixture(), b = fixture(); a.root.animations = [a.clip];
    const scene = new THREE.Scene(); scene.add(a.root, b.root);
    const clips = restoreAnimationClips(b.root, saveAnimationClips(a.root));
    const preview = new AnimationPreview(b.root, clips[0]); preview.sampleAt(1);
    expect(a.bone.position.y).toBe(2); expect(b.bone.position.y).toBe(2); preview.sampleAt(.5);
    expect(a.bone.position.y).toBe(2); expect(b.bone.position.y).toBe(1); preview.stop();
    expect(restoreAnimationClips(b.root, undefined)).toEqual([]);
  });
  it('rejects bad paths, non-bones, non-finite samples, duplicate tracks and invalid timing atomically', () => {
    const a = fixture(); a.root.animations = [a.clip]; const data = saveAnimationClips(a.root);
    const mutations = [
      d => d[0].tracks[0].path = [99], d => d[0].tracks[0].path = [1],
      d => d[0].tracks[0].values[0] = Infinity, d => d[0].tracks[0].times = [1, 0],
      d => d[0].tracks[0].times = [0, 0], d => d[0].duration = 0,
      d => d[0].tracks[0].property = 'material', d => d[0].tracks[0].values.pop(),
      d => d[0].tracks.push(d[0].tracks[0]), d => d[0].tracks[0].interpolation = -1,
    ];
    for (const mutate of mutations) { const bad = structuredClone(data); mutate(bad); expect(() => restoreAnimationClips(a.root, bad)).toThrow(); }
    expect(a.root.animations).toEqual([a.clip]); expect(a.bone.position.y).toBe(2);
  });
  it('preserves standard interpolation and rejects cubic-marked data without triple tangent dimensions', () => {
    const a = fixture(); a.clip.tracks[0].setInterpolation(THREE.InterpolateDiscrete); a.root.animations = [a.clip];
    expect(restoreAnimationClips(a.root, saveAnimationClips(a.root))[0].tracks[0].getInterpolation()).toBe(THREE.InterpolateDiscrete);
    Object.assign(a.clip.tracks[0].createInterpolant, { isInterpolantFactoryMethodGLTFCubicSpline: true });
    expect(() => saveAnimationClips(a.root)).toThrow();
    delete a.clip.tracks[0].createInterpolant.isInterpolantFactoryMethodGLTFCubicSpline;
  });
});

function fixture() {
  const root = new THREE.Group(); root.name = 'Actor'; root.position.set(4, 5, 6);
  const bone = new THREE.Bone(); bone.name = 'Hips'; bone.position.set(0, 2, 0); root.add(bone);
  const mesh = new THREE.SkinnedMesh(new THREE.BoxGeometry(), new THREE.MeshBasicMaterial());
  mesh.bind(new THREE.Skeleton([bone])); root.add(mesh);
  const clip = new THREE.AnimationClip('Walk', 2, [new THREE.VectorKeyframeTrack('Hips.position', [0, 2], [0, 0, 0, 0, 4, 0])]);
  return { root, bone, clip };
}

describe('session skeletal preview', () => {
  it('samples bone motion but never changes the actor transform', () => {
    const { root, bone, clip } = fixture(); const preview = new AnimationPreview(root, clip);
    preview.update(0.5); expect(bone.position.y).toBeCloseTo(1); expect(root.position.toArray()).toEqual([4, 5, 6]);
    preview.stop();
  });
  it('pause holds the sample and resume continues from it', () => {
    const { root, bone, clip } = fixture(); const preview = new AnimationPreview(root, clip);
    preview.update(0.5); preview.pause(); preview.update(1); expect(bone.position.y).toBeCloseTo(1);
    preview.resume(); preview.update(0.25); expect(bone.position.y).toBeCloseTo(1.5); preview.stop();
  });
  it('stop restores exact FK edits, releases bindings and cannot restart', () => {
    const { root, bone, clip } = fixture(); bone.quaternion.setFromEuler(new THREE.Euler(.1, .2, .3)); bone.scale.set(2, 3, 4);
    const before = [bone.position.toArray(), bone.quaternion.toArray(), bone.scale.toArray()];
    const preview = new AnimationPreview(root, clip); preview.update(.6); preview.stop(); preview.resume(); preview.update(.8); preview.stop();
    expect([bone.position.toArray(), bone.quaternion.toArray(), bone.scale.toArray()]).toEqual(before);
    expect(preview.mixer.stats.actions.total).toBe(0);
    expect(preview.mixer.stats.bindings.total).toBe(0);
  });
  it('switching clips after stop preserves the original edit pose', () => {
    const { root, bone, clip } = fixture(); const a = new AnimationPreview(root, clip); a.update(.2); a.stop();
    const other = new THREE.AnimationClip('Run', 1, [new THREE.VectorKeyframeTrack('Hips.position', [0, 1], [0, 10, 0, 0, 20, 0])]);
    const b = new AnimationPreview(root, other); b.update(.5); expect(bone.position.y).toBeCloseTo(15); b.stop(); expect(bone.position.y).toBe(2);
  });
  it('filters root, material, unknown node and indexed tracks without mutating input', () => {
    const { root, clip } = fixture(); clip.tracks.push(new THREE.VectorKeyframeTrack('Actor.position', [0, 2], [0, 0, 0, 9, 9, 9]), new THREE.NumberKeyframeTrack('Missing.position[x]', [0, 2], [0, 1]), new THREE.NumberKeyframeTrack('Hips.material.opacity', [0, 2], [0, 1]));
    const result = skeletalClips(root, [clip]); expect(result[0].tracks.map(t => t.name)).toEqual(['Hips.position']); expect(clip.tracks).toHaveLength(4);
  });
  it('rejects empty, non-skeletal and invalid-duration clips', () => {
    const { root } = fixture(); const empty = new THREE.AnimationClip('Empty', 1, []);
    expect(skeletalClips(root, [empty, new THREE.AnimationClip('Bad', Infinity, [])])).toEqual([]);
    expect(() => new AnimationPreview(root, empty)).toThrow('没有可预览');
  });
  it('samples absolute time deterministically, including backwards seeks and loops', () => {
    const { root, bone, clip } = fixture(); const preview = new AnimationPreview(root, clip);
    for (const time of [.75, 4.5, .25, 0, 2.75, .75]) {
      preview.sampleAt(time); expect(bone.position.y).toBeCloseTo((time % 2) * 2);
    }
    expect(root.position.toArray()).toEqual([4, 5, 6]);
    preview.stop(); preview.sampleAt(.5); expect(bone.position.y).toBe(2);
  });
  it('rejects invalid absolute sample time and restores edits after sampling', () => {
    const { root, bone, clip } = fixture(); const preview = new AnimationPreview(root, clip);
    preview.sampleAt(.25); const before = bone.position.toArray();
    for (const time of [NaN, Infinity, -1]) preview.sampleAt(time);
    expect(bone.position.toArray()).toEqual(before); preview.stop(); expect(bone.position.y).toBe(2);
  });
  it('loops and ignores invalid delta without poisoning the pose', () => {
    const { root, bone, clip } = fixture(); const preview = new AnimationPreview(root, clip);
    preview.update(2.5); expect(bone.position.y).toBeCloseTo(1); preview.update(NaN); preview.update(-1); expect(bone.position.y).toBeCloseTo(1); preview.stop();
  });
});
