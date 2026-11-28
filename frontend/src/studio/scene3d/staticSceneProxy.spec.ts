import * as THREE from 'three';
import { expect, it, vi } from 'vitest';
import { createStaticSceneProxy } from './staticSceneProxy';
const fixture = () => {
  const scene = new THREE.Scene(), geometry = new THREE.BoxGeometry(), material = new THREE.MeshStandardMaterial();
  const entries = [0, 1, 2].map(i => { const object = new THREE.Mesh(geometry, material); object.position.x = i * 2; scene.add(object); return { id: String(i), object }; });
  return { scene, entries, geometry, material, proxy: createStaticSceneProxy() };
};
it('draws three static originals as one proxy and restores identity before picking or export', () => {
  const { scene, entries, proxy, geometry, material } = fixture(); const original = entries.map(e => e.object.uuid), disposeGeometry = vi.spyOn(geometry, 'dispose'), disposeMaterial = vi.spyOn(material, 'dispose');
  const result = proxy.render(scene, entries, new Set(), () => {
    expect(entries.every(e => !e.object.visible)).toBe(true);
    const batch = scene.children.find(o => (o as THREE.InstancedMesh).isInstancedMesh) as THREE.InstancedMesh;
    expect(batch.count).toBe(3); const matrix = new THREE.Matrix4(); batch.getMatrixAt(2, matrix); expect(matrix.elements[12]).toBe(4); return 'draw';
  });
  expect(result).toEqual({ result: 'draw', batchedMeshes: 3, proxyCount: 1 });
  expect(scene.children).toHaveLength(3); expect(entries.every(e => e.object.visible)).toBe(true); expect(entries.map(e => e.object.uuid)).toEqual(original);
  proxy.dispose(); expect(disposeGeometry).not.toHaveBeenCalled(); expect(disposeMaterial).not.toHaveBeenCalled();
});
it('rebuilds after edits, excludes selected/active objects and restores after render failure', () => {
  const { scene, entries, proxy } = fixture();
  proxy.render(scene, entries, new Set(), () => {}); entries[1].object.position.x = 10;
  expect(() => proxy.render(scene, entries, new Set(['0']), () => { const batch = scene.children.find(o => (o as THREE.InstancedMesh).isInstancedMesh) as THREE.InstancedMesh; const m = new THREE.Matrix4(); batch.getMatrixAt(0, m); expect(m.elements[12]).toBe(10); expect(entries[0].object.visible).toBe(true); throw Error('GPU'); })).toThrow('GPU');
  expect(scene.children).toHaveLength(3); expect(entries.every(e => e.object.visible)).toBe(true); proxy.dispose();
});
const mergedFixture = () => {
  const scene = new THREE.Scene(), material = new THREE.MeshStandardMaterial();
  const entries = [new THREE.BoxGeometry(1, 2, 1), new THREE.SphereGeometry(.5, 8, 6)].map((g, i) => { const object = new THREE.Mesh(g, material); object.position.set(i * 3, 0, 0); scene.add(object); return { id: String(i), object }; });
  return { scene, entries, material, proxy: createStaticSceneProxy() };
};
it('merges different geometry with the same material into owned world-space geometry without changing editable objects', () => {
  const { scene, entries, material, proxy } = mergedFixture(); entries[1].object.scale.set(2, 1, .5);
  const before = entries.map(e => e.object.geometry.attributes.position.array.slice()), ids = entries.map(e => e.object.uuid);
  const originalDisposes = entries.map(e => vi.spyOn(e.object.geometry, 'dispose')), matDispose = vi.spyOn(material, 'dispose');
  let merged!: THREE.BufferGeometry;
  const result = proxy.render(scene, entries, new Set(), () => {
    expect(entries.every(e => !e.object.visible)).toBe(true);
    const batch = scene.children.find(o => !entries.some(e => e.object === o)) as THREE.Mesh;
    expect((batch as THREE.InstancedMesh).isInstancedMesh).not.toBe(true); expect(batch.material).toBe(material); merged = batch.geometry;
    expect(merged.attributes.position.count).toBe(entries.reduce((n, e) => n + e.object.geometry.attributes.position.count, 0));
    const expected = entries[1].object.geometry.clone().applyMatrix4(entries[1].object.matrixWorld);
    const offset = entries[0].object.geometry.attributes.position.count;
    const position = new THREE.Vector3().fromBufferAttribute(merged.attributes.position, offset), normal = new THREE.Vector3().fromBufferAttribute(merged.attributes.normal, offset);
    expect(position.toArray()).toEqual(new THREE.Vector3().fromBufferAttribute(expected.attributes.position, 0).toArray());
    expect(normal.toArray()).toEqual(new THREE.Vector3().fromBufferAttribute(expected.attributes.normal, 0).toArray()); expected.dispose();
  });
  expect(result.batchedMeshes).toBe(2); expect(result.proxyCount).toBe(1);
  expect(entries.map(e => e.object.uuid)).toEqual(ids); expect(entries.map(e => e.object.geometry.attributes.position.array)).toEqual(before); expect(scene.children).toHaveLength(2);
  const ownedDispose = vi.spyOn(merged, 'dispose'); proxy.dispose(); expect(ownedDispose).toHaveBeenCalledOnce(); originalDisposes.forEach(spy => expect(spy).not.toHaveBeenCalled()); expect(matDispose).not.toHaveBeenCalled();
});
it('invalidates merged geometry on attribute edits and restores visibility on drawing failure', () => {
  const { scene, entries, proxy } = mergedFixture(); let first!: THREE.BufferGeometry;
  proxy.render(scene, entries, new Set(), () => { first = (scene.children.find(o => !entries.some(e => e.object === o)) as THREE.Mesh).geometry; });
  const disposed = vi.spyOn(first, 'dispose'), positions = entries[1].object.geometry.attributes.position;
  positions.setX(0, 10); positions.needsUpdate = true;
  expect(() => proxy.render(scene, entries, new Set(), () => {
    const geometry = (scene.children.find(o => !entries.some(e => e.object === o)) as THREE.Mesh).geometry;
    expect(geometry).not.toBe(first); expect(geometry.attributes.position.getX(entries[0].object.geometry.attributes.position.count)).toBe(13); throw Error('GPU');
  })).toThrow('GPU');
  expect(disposed).toHaveBeenCalledOnce(); expect(entries.every(e => e.object.visible)).toBe(true); expect(scene.children).toHaveLength(2); proxy.dispose();
});
it.each(['selected', 'different-material', 'draw-range', 'custom-attribute', 'dynamic-attribute', 'vertex-budget'])('does not merge unsupported %s geometry', mode => {
  const { scene, entries, material, proxy } = mergedFixture(), excluded = new Set<string>();
  if (mode === 'selected') excluded.add('0');
  if (mode === 'different-material') entries[1].object.material = material.clone();
  if (mode === 'draw-range') entries[1].object.geometry.setDrawRange(1, 2);
  if (mode === 'custom-attribute') entries[1].object.geometry.setAttribute('custom', new THREE.Float32BufferAttribute([1, 2, 3], 1));
  if (mode === 'dynamic-attribute') entries[1].object.geometry.attributes.position.setUsage(THREE.DynamicDrawUsage);
  if (mode === 'vertex-budget') entries.forEach(e => e.object.geometry.setAttribute('position', new THREE.Float32BufferAttribute(new Float32Array(300003), 3)));
  expect(proxy.render(scene, entries, excluded, () => { expect(entries.every(e => e.object.visible)).toBe(true); }).proxyCount).toBe(0); proxy.dispose();
});
it.each(['animated', 'transparent', 'shader', 'hidden', 'negative'])('does not batch %s objects', mode => {
  const { scene, entries, material, proxy } = fixture();
  if (mode === 'animated') entries.forEach(e => e.object.animations.push(new THREE.AnimationClip('clip', 1, [])));
  if (mode === 'transparent') material.transparent = true;
  if (mode === 'shader') material.onBeforeCompile = () => {};
  if (mode === 'hidden') entries.forEach(e => { e.object.visible = false; });
  if (mode === 'negative') entries.forEach(e => { e.object.scale.x = -1; });
  expect(proxy.render(scene, entries, new Set(), () => {}).proxyCount).toBe(0); proxy.dispose();
});
