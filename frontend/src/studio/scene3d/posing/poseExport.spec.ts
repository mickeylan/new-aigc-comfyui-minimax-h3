import { describe, expect, it } from "vitest";
import * as THREE from "three";
import { GLTFExporter } from "three/examples/jsm/exporters/GLTFExporter.js";
import { GLTFLoader } from "three/examples/jsm/loaders/GLTFLoader.js";
import { clonePosedContent, hidePoseEditorHelpers } from "./poseExport";
import { applyBonePose, serializeBonePose, validateBonePose } from "./bonePose";
import { validateCompositionDocument } from "../planeDocument";

function rig() {
  const group = new THREE.Group(), hips = new THREE.Bone(), arm = new THREE.Bone();
  hips.name = 'mixamorigHips'; arm.name = 'Arm'; arm.position.y = 1; hips.add(arm);
  const geometry = new THREE.BoxGeometry(1, 1, 1), count = geometry.attributes.position.count;
  geometry.setAttribute('skinIndex', new THREE.Uint16BufferAttribute(new Uint16Array(count * 4), 4));
  const weights = new Float32Array(count * 4); for (let i = 0; i < count; i++) weights[i * 4] = 1;
  geometry.setAttribute('skinWeight', new THREE.Float32BufferAttribute(weights, 4));
  const mesh = new THREE.SkinnedMesh(geometry, new THREE.MeshBasicMaterial());
  mesh.add(hips); mesh.bind(new THREE.Skeleton([hips, arm])); group.add(mesh);
  return { group, hips, arm, mesh };
}

describe('posing export and document behavior', () => {
  it('preserves ancestors above Mixamo hips and safely normalizes large finite quaternions', () => {
    const { group, hips } = rig(), ancestor = new THREE.Bone(); ancestor.name = 'RigRoot';
    hips.removeFromParent(); ancestor.add(hips); group.add(ancestor);
    const pose = serializeBonePose(group)!; expect(pose.name).toBe('RigRoot');
    pose.quaternion = [0, 0, 1e200, 1e200];
    expect(applyBonePose(group, pose)).toBe(true);
    expect(ancestor.quaternion.length()).toBeCloseTo(1);
    expect(ancestor.quaternion.z).toBeCloseTo(Math.SQRT1_2);
  });
  it('clones independent skinned bones and strips nested FK spheres without modifying live selection', () => {
    const { group, arm, mesh } = rig();
    const helper = new THREE.Mesh(new THREE.SphereGeometry(), new THREE.MeshBasicMaterial()); helper.name = '__FKHelperSphere__'; arm.add(helper);
    arm.rotation.z = 0.7;
    const copy = clonePosedContent(group); let clonedMesh!: THREE.SkinnedMesh;
    copy.traverse(o => { expect(o.name).not.toBe(helper.name); if ((o as THREE.SkinnedMesh).isSkinnedMesh) clonedMesh = o as THREE.SkinnedMesh; });
    expect(clonedMesh.skeleton).not.toBe(mesh.skeleton);
    expect(clonedMesh.skeleton.bones[1]).not.toBe(arm);
    expect(clonedMesh.skeleton.bones[1].rotation.z).toBeCloseTo(0.7);
    expect(helper.parent).toBe(arm);
  });
  it('hides all helper classes and restores original visibility after a failed capture', () => {
    const scene = new THREE.Scene(), model = new THREE.Group(); scene.add(model);
    const sphere = new THREE.Object3D(); sphere.name = '__FKHelperSphere__'; model.add(sphere);
    const gizmo = new THREE.Object3D(); gizmo.userData.poseEditorHelper = true; scene.add(gizmo);
    const hidden = new THREE.Object3D(); hidden.name = sphere.name; hidden.visible = false; model.add(hidden);
    const skeleton = new THREE.SkeletonHelper(model); scene.add(skeleton);
    const restore = hidePoseEditorHelpers(scene);
    try { expect([sphere, gizmo, skeleton, hidden].every(o => !o.visible)).toBe(true); throw Error('encode failed'); }
    catch { /* failure still restores */ } finally { restore(); }
    expect([sphere, gizmo, skeleton].every(o => o.visible)).toBe(true); expect(hidden.visible).toBe(false);
  });
  it('real GLB roundtrip preserves current bones and independently restores original pose', async () => {
    const { group, arm } = rig(), initial = serializeBonePose(group)!;
    arm.rotation.z = 0.9; const current = serializeBonePose(group)!;
    const sphere = new THREE.Mesh(new THREE.SphereGeometry(), new THREE.MeshBasicMaterial()); sphere.name = '__FKHelperSphere__'; arm.add(sphere);
    const scene = new THREE.Scene(); scene.add(clonePosedContent(group));
    const binary = await new GLTFExporter().parseAsync(scene, { binary: true }) as ArrayBuffer;
    const loaded = await new GLTFLoader().parseAsync(binary, '');
    let found = false; loaded.scene.traverse(o => { if (o.name === sphere.name) found = true; }); expect(found).toBe(false);
    expect(applyBonePose(loaded.scene, JSON.parse(JSON.stringify(current)))).toBe(true);
    expect(serializeBonePose(loaded.scene)!.children[0].quaternion[2]).toBeCloseTo(current.children[0].quaternion[2]);
    expect(applyBonePose(loaded.scene, JSON.parse(JSON.stringify(initial)))).toBe(true);
    expect(serializeBonePose(loaded.scene)!.children[0].quaternion[2]).toBeCloseTo(0);
    expect(arm.rotation.z).toBeCloseTo(0.9);
  });
  it('rejects malformed, cyclic, oversized and non-finite poses before mutation', () => {
    const { group, arm } = rig(), pose = serializeBonePose(group)!;
    for (const patch of [{ children: {} }, { quaternion: [0, 0, 0, 0] }, { position: [NaN, 0, 0] }, { scale: [1, Infinity, 1] }]) {
      expect(() => applyBonePose(group, { ...pose, ...patch } as any)).toThrow(); expect(arm.rotation.z).toBe(0);
    }
    const cycle = structuredClone(pose); cycle.children.push(cycle); expect(() => validateBonePose(cycle)).toThrow();
    const deep = structuredClone(pose); let node = deep; for (let i = 0; i < 130; i++) { const child = { ...pose, children: [] }; node.children = [child]; node = child; }
    expect(() => validateBonePose(deep)).toThrow();
    const wide = { ...pose, children: Array.from({length: 4097}, () => ({ ...pose, children: [] })) }; expect(() => validateBonePose(wide)).toThrow();
    const doc = { version: 4, model: { data: 'fixture' }, camera: { position: [0, 0, 1], target: [0, 0, 0] }, objects: [{ position: [0, 0, 0], quaternion: [0, 0, 0, 1], scale: [1, 1, 1], initialBonePose: { ...pose, children: {} } }] };
    expect(() => validateCompositionDocument(doc)).toThrow();
  });
  it('restores duplicate names, missing bones without stealing siblings, and independent roots', () => {
    const root = new THREE.Group(), a = new THREE.Bone(), b = new THREE.Bone(); a.name = 'RootA'; b.name = 'RootB'; root.add(a, b);
    const c = new THREE.Bone(), d = new THREE.Bone(); c.name = d.name = 'Same'; a.add(c, d); c.rotation.x = 0.2; d.rotation.x = 0.6; b.rotation.y = 0.8;
    const pose = serializeBonePose(root)!; c.rotation.x = d.rotation.x = b.rotation.y = 0; applyBonePose(root, pose);
    expect(c.rotation.x).toBeCloseTo(0.2); expect(d.rotation.x).toBeCloseTo(0.6); expect(b.rotation.y).toBeCloseTo(0.8);
    c.name = 'Keep'; d.name = 'Other'; const saved = serializeBonePose(root)!;
    const missing = structuredClone(saved.children[0]); missing.name = 'Missing'; saved.children.unshift(missing);
    c.rotation.x = 1; d.rotation.x = 1;
    expect(applyBonePose(root, saved)).toBe(false);
    expect(c.rotation.x).toBeCloseTo(1); expect(d.rotation.x).toBeCloseTo(1); // Reject atomically, never a false success/partial restore.
  });
});
