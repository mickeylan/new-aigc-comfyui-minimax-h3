import * as THREE from 'three';
import { snapshotShotBounds, type ShotSceneEntry } from './shotNavigation';
import { validateScriptShotCamera, type ScriptShot } from './scriptShotPlan';
export type CoverageCandidate = { camera: NonNullable<ScriptShot['camera']>; axisRisk: 'crosses' | 'same-side' | 'on-axis' | 'undefined'; frustumContainsBounds: boolean; occlusionTested: false; subjects: string[] };
export function coverageCandidates(shot: ScriptShot, entries: readonly ShotSceneEntry[], aspect: number): CoverageCandidate[] {
  if (!Number.isFinite(aspect) || aspect < .2 || aspect > 5) throw Error('候选机位画幅无效');
  if (shot.execution?.camera) throw Error('已有镜内机位轨道，拒绝静态候选覆盖');
  if (new Set(entries.map(e => e.id)).size !== entries.length) throw Error('场景身份重复');
  const ids = shot.execution?.actors?.map(a => a.targetId) ?? (shot.motion ? [shot.motion.targetId] : []);
  if (!ids.length || ids.length > 32) throw Error('没有明确绑定的取景主体');
  const union = new THREE.Box3(), centers: THREE.Vector3[] = [];
  for (const id of ids) {
    const entry = entries.find(e => e.id === id);
    if (!entry || entry.splat || !entry.object.visible) throw Error('取景主体不可用');
    const box = snapshotShotBounds(entry.object);
    if (box.isEmpty() || ![...box.min.toArray(), ...box.max.toArray()].every(Number.isFinite)) throw Error('主体没有有效包围体');
    const actor = shot.execution?.actors?.find(a => a.targetId === id);
    const points = actor ? actor.beats.flatMap(b => [b.from, b.to]) : shot.motion ? [shot.motion.start, shot.motion.end] : [];
    for (const p of points) union.union(box.clone().translate(new THREE.Vector3(p.x - entry.object.position.x, 0, p.z - entry.object.position.z)));
    const first = points[0]; centers.push(box.getCenter(new THREE.Vector3()).add(new THREE.Vector3(first.x - entry.object.position.x, 0, first.z - entry.object.position.z)));
  }
  const center = union.getCenter(new THREE.Vector3()), radius = union.getSize(new THREE.Vector3()).length() / 2;
  if (!Number.isFinite(radius) || radius <= 0) throw Error('取景包围体退化');
  const vertical = THREE.MathUtils.degToRad(25), horizontal = Math.atan(Math.tan(vertical) * aspect);
  const distance = radius / Math.sin(Math.min(vertical, horizontal)) * 1.15;
  const original = shot.camera ? new THREE.Vector3(...shot.camera.position).sub(center) : new THREE.Vector3(0, .25, 1);
  const angle = Math.atan2(original.x, original.z);
  const side = (p: THREE.Vector3) => centers.length < 2 ? NaN : (centers[1].x - centers[0].x) * (p.z - centers[0].z) - (centers[1].z - centers[0].z) * (p.x - centers[0].x);
  return [angle, angle + Math.PI, angle + Math.PI / 2].map(a => {
    const position = center.clone().add(new THREE.Vector3(Math.sin(a), .2, Math.cos(a)).normalize().multiplyScalar(distance));
    const camera = validateScriptShotCamera({ position: position.toArray(), target: center.toArray(), fov: 50 });
    const view = new THREE.PerspectiveCamera(50, aspect, .01, Math.max(2000, distance * 4)); view.position.copy(position); view.lookAt(center); view.updateMatrixWorld(true);
    const frustum = new THREE.Frustum().setFromProjectionMatrix(new THREE.Matrix4().multiplyMatrices(view.projectionMatrix, view.matrixWorldInverse));
    const all = [union.min.x, union.max.x].every(x => [union.min.y, union.max.y].every(y => [union.min.z, union.max.z].every(z => frustum.containsPoint(new THREE.Vector3(x, y, z)))));
    const s = side(position), old = shot.camera ? side(new THREE.Vector3(...shot.camera.position)) : NaN;
    const axisRisk = !Number.isFinite(s) || !Number.isFinite(old) || centers[0].distanceTo(centers[1] ?? centers[0]) < 1e-6 ? 'undefined' : Math.abs(s) < 1e-6 || Math.abs(old) < 1e-6 ? 'on-axis' : s * old < 0 ? 'crosses' : 'same-side';
    return { camera, axisRisk, frustumContainsBounds: all, occlusionTested: false, subjects: [...ids] };
  });
}
