import * as THREE from 'three';
import { expect, it } from 'vitest';
import { createStaticSceneProxy } from './staticSceneProxy';
const build = () => {
  const scene = new THREE.Scene(), geometry = new THREE.BoxGeometry(1, 1, 1), material = new THREE.MeshStandardMaterial();
  return { scene, geometry, material, entries: [0, 1, 2].map(i => { const object = new THREE.Mesh(geometry, material); object.position.set(i * 2, .5, 0); scene.add(object); return { id: String(i), object }; }), proxy: createStaticSceneProxy() };
};
const proxies = (scene: THREE.Scene) => scene.children.filter(o => (o as THREE.InstancedMesh).isInstancedMesh) as THREE.InstancedMesh[];
it('never groups meshes whose shadow casting or receiving flags differ', () => {
  const { scene, entries, proxy } = build();
  entries[1].object.castShadow = true; entries[2].object.receiveShadow = true;
  const result = proxy.render(scene, entries, new Set(), () => {
    const batches = proxies(scene); expect(batches).toHaveLength(0); return 'draw';
  });
  expect(result).toEqual({ result: 'draw', batchedMeshes: 0, proxyCount: 0 });
  expect(scene.children).toHaveLength(3); proxy.dispose();
});
it('copies shadow flags, layers and render order onto the proxy and keeps groups separate', () => {
  const { scene, entries, proxy } = build();
  entries[0].object.castShadow = true; entries[0].object.receiveShadow = false; entries[0].object.layers.mask = 2; entries[0].object.renderOrder = 3;
  entries[1].object.castShadow = true; entries[1].object.receiveShadow = false; entries[1].object.layers.mask = 2; entries[1].object.renderOrder = 3;
  entries[2].object.castShadow = false; entries[2].object.receiveShadow = true;
  const result = proxy.render(scene, entries, new Set(), () => {
    const batches = proxies(scene); expect(batches).toHaveLength(1);
    const batch = batches[0]; expect(batch.count).toBe(2); expect(batch.castShadow).toBe(true); expect(batch.receiveShadow).toBe(false); expect(batch.layers.mask).toBe(2); expect(batch.renderOrder).toBe(3);
    return 'draw';
  });
  expect(result).toEqual({ result: 'draw', batchedMeshes: 2, proxyCount: 1 });
  expect(entries[2].object.visible).toBe(true); expect(scene.children).toHaveLength(3);
  proxy.dispose(); expect(scene.children).toHaveLength(3);
});
it('drops the proxy and keeps originals drawable when the scene transform is not identity', () => {
  const { scene, entries, proxy } = build();
  proxy.render(scene, entries, new Set(), () => {});
  scene.position.x = .5;
  const result = proxy.render(scene, entries, new Set(), () => { expect(proxies(scene)).toHaveLength(0); return 'draw'; });
  expect(result).toEqual({ result: 'draw', batchedMeshes: 0, proxyCount: 0 }); expect(entries.every(e => e.object.visible)).toBe(true);
  proxy.dispose();
});
it('rebuilds when only a shadow flag changes so stale shadows cannot linger', () => {
  const { scene, entries, proxy } = build();
  proxy.render(scene, entries, new Set(), () => { expect(proxies(scene)[0].count).toBe(3); });
  entries[2].object.castShadow = true;
  proxy.render(scene, entries, new Set(), () => { const batches = proxies(scene); expect(batches).toHaveLength(1); expect(batches[0].count).toBe(2); expect(batches[0].castShadow).toBe(false); });
  entries[2].object.castShadow = false;
  proxy.render(scene, entries, new Set(), () => { expect(proxies(scene)[0].count).toBe(3); });
  proxy.dispose();
});