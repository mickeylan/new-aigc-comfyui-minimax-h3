import * as THREE from 'three';
import { snapshotShotBounds, type ShotSceneEntry } from './shotNavigation';
import type { ScriptShot } from './scriptShotPlan';
import type { CoverageCandidate } from './previsCoverage';
export type CoverageOcclusion = { status: 'completed' | 'unavailable'; reason: string; rays: number; triangles: number; blockedRays: number; blockers: string[]; visuallyApproved: false };
/** Finite static triangle-ray warnings only, not optical visibility or dynamic clearance. */
export function diagnoseCoverageOcclusion(shot: ScriptShot, candidate: CoverageCandidate, entries: readonly ShotSceneEntry[]): CoverageOcclusion {
  const result: CoverageOcclusion = { status: 'unavailable', reason: '', rays: 0, triangles: 0, blockedRays: 0, blockers: [], visuallyApproved: false };
  const meshes: THREE.Mesh[] = [], owned: THREE.BufferGeometry[] = [], materials: THREE.Material[] = [];
  try {
    if (entries.length > 128 || new Set(entries.map(e => e.id)).size !== entries.length) throw Error('条目预算超限或身份重复');
    let nodes = 0, vertices = 0;
    const pending = entries.map(e => e.object);
    while (pending.length) {
      const node = pending.pop()!;
      if (++nodes > 2048) throw Error('层级节点超过2048预算');
      pending.push(...node.children);
      const g = (node as THREE.Mesh).geometry;
      if (g) {
        vertices += g.attributes.position?.count ?? 0;
        if (vertices > 300000 || (g.index?.count ?? 0) > 300000) throw Error('几何顶点或索引预算超限');
      }
    }
    const ids = shot.execution?.actors?.map(a => a.targetId) ?? (shot.motion ? [shot.motion.targetId] : []);
    if (!ids.length || ids.length > 32 || JSON.stringify(ids) !== JSON.stringify(candidate.subjects)) throw Error('取景主体不一致');
    const origin = new THREE.Vector3(...candidate.camera.position), endpoints: THREE.Vector3[] = [];
    if (!origin.toArray().every(Number.isFinite)) throw Error('候选机位无效');
    for (const id of ids) {
      const entry = entries.find(e => e.id === id);
      if (!entry || !entry.object.visible || entry.splat) throw Error('取景主体不可用');
      const bounds = snapshotShotBounds(entry.object);
      if (bounds.isEmpty() || ![...bounds.min.toArray(), ...bounds.max.toArray()].every(Number.isFinite)) throw Error('主体包围体无效');
      const actor = shot.execution?.actors?.find(a => a.targetId === id);
      const points = actor ? actor.beats.flatMap(b => [b.from, b.to]) : shot.motion ? [shot.motion.start, shot.motion.end] : [];
      const unique = new Map(points.map(p => [`${p.x}:${p.z}`, p]));
      for (const p of unique.values()) {
        const box = bounds.clone().translate(new THREE.Vector3(p.x - entry.object.position.x, 0, p.z - entry.object.position.z));
        endpoints.push(box.getCenter(new THREE.Vector3()));
        for (const x of [box.min.x, box.max.x]) for (const y of [box.min.y, box.max.y]) for (const z of [box.min.z, box.max.z]) endpoints.push(new THREE.Vector3(x, y, z));
        if (endpoints.length > 512) throw Error('遮挡射线超过512预算');
      }
    }
    if (!endpoints.length || endpoints.some(p => !p.toArray().every(Number.isFinite))) throw Error('采样端点无效');
    for (const entry of entries) {
      if (ids.includes(entry.id) || !entry.object.visible) continue;
      if (entry.splat) throw Error('场景含未支持的泼溅对象');
      // Preflight originals: Object3D.clone does not preserve custom callbacks/flags.
      for (let parent = entry.object.parent; parent; parent = parent.parent) {
        if (!parent.visible) throw Error('遮挡物祖先隐藏，未支持该层级');
        if (parent.position.lengthSq() > 1e-12 || parent.quaternion.angleTo(new THREE.Quaternion()) > 1e-6 || parent.scale.distanceTo(new THREE.Vector3(1, 1, 1)) > 1e-6 || !parent.matrixAutoUpdate && !parent.matrix.equals(new THREE.Matrix4())) throw Error('遮挡物祖先变换未支持');
      }
      entry.object.traverse(node => {
        for (let parent: THREE.Object3D | null = node; parent; parent = parent.parent) if (!parent.visible) return;
        if (node.type !== 'Mesh' && node.type !== 'Group' && node.type !== 'Object3D' || node.animations.length || (node as THREE.SkinnedMesh).isSkinnedMesh || (node as THREE.InstancedMesh).isInstancedMesh || (node as THREE.Bone).isBone || node.onBeforeRender !== THREE.Object3D.prototype.onBeforeRender || node.onAfterRender !== THREE.Object3D.prototype.onAfterRender) throw Error('遮挡物含未支持的动态/实例/自定义行为');
        const mesh = node as THREE.Mesh;
        if (node.type === 'Mesh' && mesh.raycast !== THREE.Mesh.prototype.raycast) throw Error('未支持自定义射线行为');
        const material = mesh.material;
        if (material && (Array.isArray(material) || (material as THREE.MeshStandardMaterial).displacementMap || (material as THREE.MeshBasicMaterial).wireframe)) throw Error('遮挡物材质含未支持的位移或线框');
      });
      const clone = entry.object.clone(true); clone.updateMatrixWorld(true);
      clone.traverse(node => {
        for (let parent: THREE.Object3D | null = node; parent; parent = parent.parent) if (!parent.visible) return;
        if (node.animations.length || (node as THREE.Bone).isBone || (node as THREE.SkinnedMesh).isSkinnedMesh) throw Error('遮挡物含动画或骨骼，未支持动态遮挡');
        if (node.type === 'Group' || node.type === 'Object3D') return;
        if (node.type !== 'Mesh') throw Error('遮挡物含未支持的非静态Mesh');
        const mesh = node as THREE.Mesh, g = mesh.geometry, m = mesh.material;
        if (meshes.length >= 256) throw Error('遮挡Mesh超过256预算');
        if (Array.isArray(m) || !['MeshBasicMaterial', 'MeshStandardMaterial'].includes(m.type) || m.transparent || m.opacity !== 1 || m.alphaTest || m.alphaHash || m.clippingPlanes?.length || m.onBeforeCompile !== THREE.Material.prototype.onBeforeCompile || !m.visible || mesh.layers.mask !== 1 || mesh.onBeforeRender !== THREE.Object3D.prototype.onBeforeRender || mesh.onAfterRender !== THREE.Object3D.prototype.onAfterRender || Object.keys(g.morphAttributes).length || g.drawRange.start !== 0 || g.drawRange.count !== Infinity) throw Error('遮挡材质/几何含未支持的透明、裁切或自定义行为');
        const position = g.attributes.position;
        if (!position || position.itemSize !== 3) throw Error('遮挡物顶点无效');
        for (let i = 0; i < position.count; i++) if (![position.getX(i), position.getY(i), position.getZ(i)].every(Number.isFinite)) throw Error('遮挡物顶点非有限');
        if (g.index) for (let i = 0; i < g.index.count; i++) { const index = g.index.getX(i); if (!Number.isInteger(index) || index < 0 || index >= position.count) throw Error('遮挡物索引无效'); }
        const triangles = (g.index?.count ?? position.count) / 3;
        result.triangles += triangles;
        if (!Number.isInteger(triangles) || triangles <= 0 || result.triangles > 100000 || result.triangles * endpoints.length > 2000000) throw Error('遮挡三角形或射线乘积预算超限');
        const geometry = g.clone(); owned.push(geometry);
        const material = m.clone(); materials.push(material); material.side = THREE.DoubleSide;
        const proxy = new THREE.Mesh(geometry, material); proxy.matrixAutoUpdate = false; proxy.matrix.copy(mesh.matrixWorld); proxy.updateMatrixWorld(true); proxy.name = entry.id;
        if (!proxy.matrixWorld.elements.every(Number.isFinite) || Math.abs(proxy.matrixWorld.determinant()) < 1e-12) throw Error('遮挡物世界变换无效');
        meshes.push(proxy);
      });
    }
    const blockers = new Set<string>(), ray = new THREE.Raycaster();
    for (const endpoint of endpoints) {
      const direction = endpoint.clone().sub(origin), distance = direction.length();
      if (distance <= 2e-5) throw Error('机位与采样点重合，不能诊断');
      ray.set(origin, direction.normalize()); ray.near = 1e-5; ray.far = distance - 1e-5;
      const hits = ray.intersectObjects(meshes, false);
      result.rays++; if (hits.length) { result.blockedRays++; for (const hit of hits) blockers.add(hit.object.name); }
    }
    result.blockers = [...blockers].sort(); result.status = 'completed';
  } catch (error) { result.reason = error instanceof Error ? error.message : String(error); result.rays = 0; result.blockedRays = 0; result.blockers = []; }
  finally { owned.forEach(g => g.dispose()); materials.forEach(m => m.dispose()); }
  return result;
}
