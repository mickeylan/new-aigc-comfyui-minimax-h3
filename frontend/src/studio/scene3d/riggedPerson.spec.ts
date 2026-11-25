import { describe, expect, it } from 'vitest';
import * as THREE from 'three';
import { GLTFExporter } from 'three/addons/exporters/GLTFExporter.js';
import { GLTFLoader } from 'three/addons/loaders/GLTFLoader.js';
import { createRiggedPerson } from './toonflow/riggedPerson';
import { AnimationPreview, markAnimationArchivePaths, restoreAnimationClips, saveAnimationClips } from './animationPreview';
import { clonePosedContent } from './posing/poseExport';
import { disposeObject } from './toonflow/sceneResources';
import { characterAnimationCatalogRevision, createCharacterAnimationClip } from './toonflow/characterAnimations';

describe('真实24骨多材质人物存档', () => {
  it('37片段通过真实GLB两轮往返，保留骨骼绑定与蒙皮播放，源对象不污染', async () => {
    const model = createRiggedPerson('#c7ced4');
    let root = new THREE.Group(); root.add(model); root.animations = model.animations;
    const original = root;
    const first = saveAnimationClips(root);
    expect(first).toHaveLength(37);
    for (let cycle = 0; cycle < 2; cycle++) {
      const clips = saveAnimationClips(root);
      const exported = clonePosedContent(root); markAnimationArchivePaths(exported);
      const scene = new THREE.Scene(); scene.add(exported);
      const bytes = await new GLTFExporter().parseAsync(scene, { binary: true, onlyVisible: false }) as ArrayBuffer;
      const loaded = (await new GLTFLoader().parseAsync(bytes, '')).scene.children[0];
      loaded.animations = restoreAnimationClips(loaded, clips);
      expect(saveAnimationClips(loaded)).toEqual(first);
      const bones: THREE.Bone[] = []; loaded.traverse(object => { if ((object as THREE.Bone).isBone) bones.push(object as THREE.Bone); });
      expect(bones).toHaveLength(24);
      const arm = bones.find(bone => bone.name.startsWith('leftUpperArm'))!;
      const baseline = arm.quaternion.toArray();
      const run = loaded.animations.find(clip => clip.name === 'cc0Run')!;
      const preview = new AnimationPreview(loaded, run); preview.sampleAt(0.25);
      expect(arm.quaternion.toArray()).not.toEqual(baseline);
      preview.stop(); expect(arm.quaternion.toArray()).toEqual(baseline);
      let meshes = 0; loaded.traverse(object => { if ((object as THREE.SkinnedMesh).isSkinnedMesh) { meshes++; expect((object as THREE.SkinnedMesh).skeleton.bones).toHaveLength(24); } });
      expect(meshes).toBeGreaterThan(0);
      if (root !== original) disposeObject(root);
      root = loaded as THREE.Group;
    }
    original.traverse(object => expect(object.userData.sceneAnimationPath).toBeUndefined());
    disposeObject(original); disposeObject(root);
    console.log('RIGGED_PERSON_GLB_ROUNDTRIP=PASS bones=24 clips=37 cycles=2 animation=cc0Run restored=1');
  });
  it('目录来源随真实GLB和完整轨道两轮往返，旧档不猜来源且历史修订不被当前目录替换', async () => {
    let root: THREE.Object3D = createRiggedPerson('#cccccc');
    const run = createCharacterAnimationClip('cc0Run'); run.name = '人工审阅奔跑'; root.animations = [run];
    const expected = saveAnimationClips(root);
    expect(expected[0].catalogSource).toEqual({ catalog: 'toonflow-quaternius', id: 'cc0Run', revision: characterAnimationCatalogRevision, license: 'CC0-1.0' });
    for (let cycle = 0; cycle < 2; cycle++) {
      const exported = clonePosedContent(root); markAnimationArchivePaths(exported);
      const scene = new THREE.Scene(); scene.add(exported);
      const bytes = await new GLTFExporter().parseAsync(scene, { binary: true, onlyVisible: false }) as ArrayBuffer;
      const loaded = (await new GLTFLoader().parseAsync(bytes, '')).scene.children[0];
      loaded.animations = restoreAnimationClips(loaded, JSON.parse(JSON.stringify(expected)));
      expect(saveAnimationClips(loaded)).toEqual(expected);
      disposeObject(root); root = loaded;
    }
    const legacy = structuredClone(expected); delete legacy[0].catalogSource;
    root.animations = restoreAnimationClips(root, legacy);
    expect(saveAnimationClips(root)[0].catalogSource).toBeUndefined();
    const historical = structuredClone(expected); historical[0].catalogSource!.revision = 1;
    root.animations = restoreAnimationClips(root, historical);
    expect(saveAnimationClips(root)).toEqual(historical);
    const previous = root.animations;
    for (const bad of [null, [], { ...expected[0].catalogSource, revision: 0 }, { ...expected[0].catalogSource, revision: 1.5 }, { ...expected[0].catalogSource, id: '../run' }, { ...expected[0].catalogSource, license: 'MIT' }, { ...expected[0].catalogSource, surprise: true }]) {
      expect(() => restoreAnimationClips(root, [{ ...expected[0], catalogSource: bad }])).toThrow('目录来源无效');
      expect(root.animations).toBe(previous);
    }
    disposeObject(root);
    console.log('ANIMATION_CLIP_PROVENANCE=PASS cycles=2 glb=real legacy=compatible historicalRevision=retained invalid=atomic');
  });
  it('导出路径重复、畸形和缺失映射都拒绝，不退回可能绑错的层级', () => {
    const root = createRiggedPerson('#cccccc'); const saved = saveAnimationClips(root);
    markAnimationArchivePaths(root);
    const bones: THREE.Bone[] = []; root.traverse(object => { if ((object as THREE.Bone).isBone) bones.push(object as THREE.Bone); });
    const first = bones[0].userData.sceneAnimationPath;
    bones[0].userData.sceneAnimationPath = [-1]; expect(() => restoreAnimationClips(root, saved)).toThrow('骨骼绑定');
    bones[0].userData.sceneAnimationPath = bones[1].userData.sceneAnimationPath; expect(() => restoreAnimationClips(root, saved)).toThrow('骨骼绑定');
    bones[0].userData.sceneAnimationPath = first; delete bones[1].userData.sceneAnimationPath; expect(() => restoreAnimationClips(root, saved)).toThrow('骨骼绑定');
    disposeObject(root);
  });
});
