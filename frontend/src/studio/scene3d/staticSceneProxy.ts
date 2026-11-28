import * as THREE from 'three';
import { mergeGeometries } from 'three/addons/utils/BufferGeometryUtils.js';
type Entry = { id: string; object: THREE.Object3D; splat?: unknown; mediaKind?: string };
/** Viewport-only instancing. Borrow original geometry/material; never dispose them.
 * Originals remain the only editable/pickable/exportable scene graph between draws. */
export function createStaticSceneProxy() {
  let key = '', batches: { originals: THREE.Mesh[]; proxy: THREE.Mesh; ownedGeometry: boolean }[] = [];
  const identities = new WeakMap<object, number>(); let serial = 0;
  const identity = (value: object) => { if (!identities.has(value)) identities.set(value, ++serial); return identities.get(value)!; };
  const dispose = () => { batches.forEach(b => { b.proxy.removeFromParent(); if (b.ownedGeometry) b.proxy.geometry.dispose(); else (b.proxy as THREE.InstancedMesh).dispose(); }); batches = []; key = ''; };
  const drawKey = (m: THREE.Mesh) => [(m.material as THREE.Material).uuid, m.castShadow, m.receiveShadow, m.layers.mask, m.renderOrder].join(':');
  const attributes = (g: THREE.BufferGeometry) => [g.index, ...Object.keys(g.attributes).sort().map(k => g.attributes[k])].map(a => a ? [identity(a), a instanceof THREE.InterleavedBufferAttribute ? a.data.version : a.version] : null);
  const mergeLayout = (g: THREE.BufferGeometry) => {
    const names = Object.keys(g.attributes).sort();
    if (!g.attributes.position || g.drawRange.start !== 0 || g.drawRange.count !== Infinity || names.some(n => !['position', 'normal', 'uv', 'color', 'tangent'].includes(n))) return null;
    const attrs = names.map(n => g.attributes[n]);
    if (attrs.some(a => !(a instanceof THREE.BufferAttribute) || !(a.array instanceof Float32Array) || a.usage !== THREE.StaticDrawUsage) || (g.index && g.index.usage !== THREE.StaticDrawUsage)) return null;
    return JSON.stringify([!!g.index, names.map(n => [n, g.attributes[n].itemSize, g.attributes[n].normalized])]);
  };
  return {
    dispose,
    render<T>(scene: THREE.Scene, entries: readonly Entry[], excluded: ReadonlySet<string>, draw: () => T): { result: T; batchedMeshes: number; proxyCount: number } {
      scene.updateMatrixWorld(true);
      const groups = new Map<string, THREE.Mesh[]>();
      let count = 0;
      for (const entry of entries) {
        if (excluded.has(entry.id) || entry.splat || entry.mediaKind || !entry.object.visible) continue;
        let dynamic = false;
        entry.object.traverse(o => { if (o.animations.length || (o as THREE.SkinnedMesh).isSkinnedMesh || (o as THREE.Bone).isBone) dynamic = true; });
        if (dynamic) continue;
        entry.object.traverse(o => {
          if (++count > 2000 || o.type !== 'Mesh' || o.matrixWorld.determinant() <= 0 || !o.matrixWorld.elements.every(Number.isFinite)) return;
          const mesh = o as THREE.Mesh, material = mesh.material;
          for (let parent: THREE.Object3D | null = o; parent; parent = parent.parent) if (!parent.visible) return;
          if (Array.isArray(material) || !['MeshStandardMaterial', 'MeshBasicMaterial'].includes(material.type) || material.transparent || material.opacity !== 1 || material.alphaTest || material.clippingPlanes?.length || material.polygonOffset || material.wireframe || material.onBeforeCompile !== THREE.Material.prototype.onBeforeCompile || mesh.morphTargetInfluences?.length || Object.keys(mesh.geometry.morphAttributes).length || mesh.onBeforeRender !== THREE.Object3D.prototype.onBeforeRender || mesh.onAfterRender !== THREE.Object3D.prototype.onAfterRender || mesh.children.length || mesh.customDepthMaterial || mesh.customDistanceMaterial) return;
          const id = [mesh.geometry.uuid, material.uuid, mesh.castShadow, mesh.receiveShadow, mesh.layers.mask, mesh.renderOrder].join(':');
          const group = groups.get(id) ?? []; group.push(mesh); groups.set(id, group);
        });
      }
      const eligible = [...groups.entries()].filter(([, meshes]) => meshes.length >= 2);
      const mergeGroups = new Map<string, THREE.Mesh[]>(); let vertexBudget = 200000;
      for (const [, meshes] of groups) {
        if (meshes.length !== 1) continue;
        const mesh = meshes[0], layout = mergeLayout(mesh.geometry), vertices = mesh.geometry.attributes.position?.count ?? 0;
        if (!layout || vertices > vertexBudget) continue;
        vertexBudget -= vertices;
        const id = drawKey(mesh) + layout, list = mergeGroups.get(id) ?? []; list.push(mesh); mergeGroups.set(id, list);
      }
      const merged = [...mergeGroups.entries()].filter(([, meshes]) => meshes.length >= 2);
      const next = JSON.stringify([eligible, merged].map(list => list.map(([id, meshes]) => [id, ...meshes.map(m => [m.uuid, ...m.matrixWorld.elements, attributes(m.geometry)])])));
      if (next !== key) {
        dispose(); key = next;
        try {
          for (const [, originals] of eligible) {
            const first = originals[0], proxy = new THREE.InstancedMesh(first.geometry, first.material, originals.length);
            originals.forEach((m, i) => proxy.setMatrixAt(i, m.matrixWorld));
            proxy.instanceMatrix.needsUpdate = true; proxy.castShadow = first.castShadow; proxy.receiveShadow = first.receiveShadow; proxy.layers.mask = first.layers.mask; proxy.renderOrder = first.renderOrder; proxy.frustumCulled = false;
            batches.push({ originals, proxy, ownedGeometry: false });
          }
          for (const [, originals] of merged) {
            const copies: THREE.BufferGeometry[] = [];
            try {
              for (const original of originals) { const copy = original.geometry.clone(); copies.push(copy); copy.clearGroups(); copy.applyMatrix4(original.matrixWorld); }
              const geometry = mergeGeometries(copies, false);
              if (!geometry) throw Error('静态合并代理属性不兼容');
              const first = originals[0], proxy = new THREE.Mesh(geometry, first.material);
              proxy.castShadow = first.castShadow; proxy.receiveShadow = first.receiveShadow; proxy.layers.mask = first.layers.mask; proxy.renderOrder = first.renderOrder; proxy.frustumCulled = false;
              batches.push({ originals, proxy, ownedGeometry: true });
            } finally { copies.forEach(g => g.dispose()); }
          }
        } catch (error) { dispose(); throw error; }
      }
      // World-space proxy transforms require an identity scene transform.
      if (!scene.matrixWorld.equals(new THREE.Matrix4())) { dispose(); return { result: draw(), batchedMeshes: 0, proxyCount: 0 }; }
      try {
        batches.forEach(b => { b.originals.forEach(m => { m.visible = false; }); scene.add(b.proxy); });
        return { result: draw(), batchedMeshes: batches.reduce((n, b) => n + b.originals.length, 0), proxyCount: batches.length };
      } finally {
        batches.forEach(b => { b.proxy.removeFromParent(); b.originals.forEach(m => { m.visible = true; }); });
      }
    },
  };
}
