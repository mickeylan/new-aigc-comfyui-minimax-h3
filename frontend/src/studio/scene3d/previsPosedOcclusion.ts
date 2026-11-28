import * as THREE from 'three';
import type { ScriptShot } from './scriptShotPlan';
import type { ShotSceneEntry } from './shotNavigation';
import type { CoverageCandidate } from './previsCoverage';
export type PosedOcclusionFrame = { seconds: number; rays: number; blockedRays: number; blockers: string[] };
export type PosedOcclusion = { policy: 'posed-subject-aabb-v1'; visuallyApproved: false; frames: PosedOcclusionFrame[] };

/** Exact current CPU skin/morph vertices, conservative boxes, NOT optical visibility. */
export function diagnosePosedSubjects(shot: ScriptShot, candidates: readonly CoverageCandidate[], entries: readonly ShotSceneEntry[], makeSampler: () => { sampleAt(seconds: number): unknown; stop(): void }): PosedOcclusion[] {
  const ids = shot.execution?.actors?.map(a => a.targetId) ?? (shot.motion ? [shot.motion.targetId] : []);
  if (!ids.length || ids.length > 32 || new Set(ids).size !== ids.length || entries.length > 128 || new Set(entries.map(e => e.id)).size !== entries.length) throw Error('主体或条目预算/身份无效');
  if (!candidates.length || candidates.length > 3 || candidates.some(c => JSON.stringify(c.subjects) !== JSON.stringify(ids) || !c.camera.position.every(Number.isFinite))) throw Error('候选主体或机位无效');
  const length = shot.end - shot.start;
  if (!Number.isFinite(shot.start) || shot.start < 0 || !Number.isFinite(length) || length <= 0) throw Error('镜头区间无效');
  const times = [...new Set([shot.start, shot.start + length / 2, shot.end - Math.min(1 / 24, length / 2)])];
  let nodes = 0, vertices = 0;
  const subjects = ids.map(id => {
    const entry = entries.find(e => e.id === id);
    if (!entry || entry.splat) throw Error('主体缺失或为不支持的泼溅');
    for (let node: THREE.Object3D | null = entry.object; node; node = node.parent) if (!node.visible) throw Error('主体或祖先隐藏');
    const meshes: THREE.Mesh[] = [];
    entry.object.traverse(node => {
      if (++nodes > 2048) throw Error('主体节点超过2048预算');
      for (let parent: THREE.Object3D | null = node; parent; parent = parent.parent) if (!parent.visible) return;
      if ((node as THREE.InstancedMesh).isInstancedMesh) throw Error('不支持实例主体');
      if (!(node as THREE.Mesh).isMesh) { if (!['Group', 'Object3D', 'Bone'].includes(node.type)) throw Error('主体含不支持节点'); return; }
      const mesh = node as THREE.Mesh;
      if (mesh.getVertexPosition !== ((mesh as THREE.SkinnedMesh).isSkinnedMesh ? THREE.SkinnedMesh.prototype.getVertexPosition : THREE.Mesh.prototype.getVertexPosition)) throw Error('不支持自定义顶点方法');
      const count = mesh.geometry.attributes.position?.count ?? 0;
      vertices += count;
      if (!count || vertices > 100000 || vertices * times.length > 300000) throw Error('主体顶点预算超限或为空');
      meshes.push(mesh);
    });
    if (!meshes.length) throw Error('主体无可测网格');
    return { id, root: entry.object, meshes };
  });
  const output: PosedOcclusion[] = candidates.map(() => ({ policy: 'posed-subject-aabb-v1', visuallyApproved: false, frames: [] }));
  const sampler = makeSampler();
  try {
    for (const seconds of times) {
      sampler.sampleAt(seconds);
      const boxes = subjects.map(subject => {
        subject.root.updateWorldMatrix(true, true);
        const box = new THREE.Box3(), vertex = new THREE.Vector3();
        for (const mesh of subject.meshes) {
          for (let i = 0; i < mesh.geometry.attributes.position.count; i++) {
            mesh.getVertexPosition(i, vertex).applyMatrix4(mesh.matrixWorld);
            if (!vertex.toArray().every(Number.isFinite)) throw Error('主体采样顶点非有限');
            box.expandByPoint(vertex);
          }
        }
        if (box.isEmpty()) throw Error('主体采样包围体为空');
        return box;
      });
      candidates.forEach((candidate, c) => {
        const origin = new THREE.Vector3(...candidate.camera.position), ray = new THREE.Ray(), hit = new THREE.Vector3();
        const frame: PosedOcclusionFrame = { seconds, rays: 0, blockedRays: 0, blockers: [] }, blockers = new Set<string>();
        boxes.forEach((box, target) => {
          const points = [box.getCenter(new THREE.Vector3())];
          for (const x of [box.min.x, box.max.x]) for (const y of [box.min.y, box.max.y]) for (const z of [box.min.z, box.max.z]) points.push(new THREE.Vector3(x,y,z));
          for (const point of points) {
            const direction = point.clone().sub(origin), distance = direction.length();
            if (distance <= 2e-5) throw Error('机位与采样点重合');
            ray.set(origin, direction.normalize()); let blocked = false;
            boxes.forEach((other, index) => {
              if (index === target) return;
              if (other.containsPoint(origin) || ray.intersectBox(other, hit) && hit.distanceTo(origin) < distance - 1e-5) { blocked = true; blockers.add(subjects[index].id); }
            });
            frame.rays++; if (blocked) frame.blockedRays++;
          }
        });
        frame.blockers = [...blockers].sort(); output[c].frames.push(frame);
      });
    }
    return output;
  } finally { sampler.stop(); }
}
