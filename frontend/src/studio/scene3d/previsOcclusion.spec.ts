import * as THREE from 'three';
import { expect, it, vi } from 'vitest';
import { diagnoseCoverageOcclusion } from './previsOcclusion';
import type { CoverageCandidate } from './previsCoverage';
function fixture() {
  const subject = new THREE.Mesh(new THREE.BoxGeometry(1, 2, 1), new THREE.MeshBasicMaterial()); subject.position.y = 1;
  const wall = new THREE.Mesh(new THREE.BoxGeometry(8, 8, .2), new THREE.MeshBasicMaterial()); wall.position.set(0, 1, 3);
  const shot: any = { motion: { targetId: 'actor', start: { x: 0, z: 0 }, end: { x: 0, z: 0 } } };
  const candidate: CoverageCandidate = { camera: { position: [0, 1, 6], target: [0, 1, 0], fov: 50 }, axisRisk: 'undefined', subjects: ['actor'], frustumContainsBounds: true, occlusionTested: false };
  const entries = [{ id: 'actor', name: 'actor', object: subject }, { id: 'wall', name: 'wall', object: wall }];
  return { subject, wall, shot, candidate, entries };
}
it('hits actual wall triangles without changing editing resources or caches', () => {
  const f = fixture(), side = f.wall.material.side, matrix = f.wall.matrixWorld.toArray(), dispose = vi.spyOn(f.wall.geometry, 'dispose');
  expect(f.wall.geometry.boundingSphere).toBeNull();
  const result = diagnoseCoverageOcclusion(f.shot, f.candidate, f.entries);
  expect(result).toMatchObject({ status: 'completed', rays: 9, blockedRays: 9, triangles: 12, blockers: ['wall'], visuallyApproved: false });
  expect(f.wall.material.side).toBe(side); expect(f.wall.geometry.boundingSphere).toBeNull(); expect(f.wall.geometry.boundingBox).toBeNull(); expect(f.wall.matrixWorld.toArray()).toEqual(matrix); expect(dispose).not.toHaveBeenCalled();
});
it.each(['behind', 'hidden', 'off-ray'])('does not hit %s obstacles or the subjects themselves', kind => {
  const f = fixture(); if (kind === 'behind') f.wall.position.z = -3; if (kind === 'hidden') f.wall.visible = false; if (kind === 'off-ray') f.wall.position.x = 20;
  expect(diagnoseCoverageOcclusion(f.shot, f.candidate, f.entries)).toMatchObject({ status: 'completed', rays: 9, blockedRays: 0, visuallyApproved: false });
});
it('tests double-sided triangles and root endpoints with deterministic warnings', () => {
  const f = fixture(); f.wall.geometry = new THREE.PlaneGeometry(8, 8); f.wall.rotation.y = Math.PI;
  f.shot.motion.end.x = 1;
  const one = diagnoseCoverageOcclusion(f.shot, f.candidate, f.entries);
  expect(one).toMatchObject({ status: 'completed', rays: 18, blockedRays: 18, triangles: 2 }); expect(diagnoseCoverageOcclusion(f.shot, f.candidate, f.entries)).toEqual(one);
});
it.each([false, true])('releases all derived geometries/materials even when raycast throws: %s', fail => {
  const f = fixture(), derived: ReturnType<typeof vi.spyOn>[] = [], cloneGeometry = THREE.BufferGeometry.prototype.clone, cloneMaterial = THREE.MeshBasicMaterial.prototype.clone;
  const geometrySpy = vi.spyOn(THREE.BufferGeometry.prototype, 'clone').mockImplementation(function () { const copy = cloneGeometry.call(this); derived.push(vi.spyOn(copy, 'dispose')); return copy; });
  const materialSpy = vi.spyOn(THREE.MeshBasicMaterial.prototype, 'clone').mockImplementation(function () { const copy = cloneMaterial.call(this); derived.push(vi.spyOn(copy, 'dispose')); return copy; });
  const raycast = fail ? vi.spyOn(THREE.Raycaster.prototype, 'intersectObjects').mockImplementation(() => { throw Error('controlled ray failure'); }) : null;
  try {
    const result = diagnoseCoverageOcclusion(f.shot, f.candidate, f.entries);
    expect(result.status).toBe(fail ? 'unavailable' : 'completed'); expect(derived.length).toBeGreaterThanOrEqual(3);
    derived.forEach(dispose => expect(dispose).toHaveBeenCalledOnce()); if (fail) expect(result.reason).toBe('controlled ray failure');
  } finally { geometrySpy.mockRestore(); materialSpy.mockRestore(); raycast?.mockRestore(); }
});
it.each(['transparent', 'callback', 'skinned', 'instance', 'morph', 'triangles', 'entries', 'rays', 'ancestor', 'displacement', 'matrix-ancestor', 'vertices', 'indices', 'nan', 'nodes', 'raycast'])('reports unavailable rather than false clearance: %s', kind => {
  const f = fixture();
  if (kind === 'transparent') f.wall.material.transparent = true;
  if (kind === 'callback') f.wall.onBeforeRender = () => {};
  if (kind === 'skinned') (f.wall as any).isSkinnedMesh = true;
  if (kind === 'instance') f.wall.type = 'InstancedMesh';
  if (kind === 'morph') f.wall.geometry.morphAttributes.position = [new THREE.Float32BufferAttribute([0, 0, 0], 3)];
  if (kind === 'triangles') f.wall.geometry = new THREE.SphereGeometry(1, 256, 256);
  if (kind === 'entries') for (let i = 0; i < 127; i++) f.entries.push({ id: 'e'+i, name: 'extra', object: new THREE.Group() as any });
  if (kind === 'rays') { delete f.shot.motion; f.shot.execution = { actors: [{ targetId: 'actor', beats: Array.from({ length: 60 }, (_, i) => ({ from: { x: i, z: 0 }, to: { x: i+1, z: 0 } })) }] }; }
  if (kind === 'ancestor') { const p = new THREE.Group(); p.position.x = 1; p.add(f.wall); }
  if (kind === 'displacement') (f.wall.material as any).displacementMap = new THREE.Texture();
  if (kind === 'matrix-ancestor') { const p = new THREE.Group(); p.matrixAutoUpdate = false; p.matrix.makeTranslation(1, 0, 0); p.add(f.wall); }
  if (kind === 'vertices') f.wall.geometry.setAttribute('position', new THREE.Float32BufferAttribute(new Float32Array(300001 * 3), 3));
  if (kind === 'indices') f.wall.geometry.index!.setX(0, 99999);
  if (kind === 'nan') f.wall.geometry.attributes.position.setX(0, NaN);
  if (kind === 'nodes') for (let i = 0; i < 2048; i++) f.wall.add(new THREE.Group());
  if (kind === 'raycast') f.wall.raycast = () => {};
  const result = diagnoseCoverageOcclusion(f.shot, f.candidate, f.entries); expect(result.status).toBe('unavailable'); expect(result.reason).not.toBe(''); expect(result.rays).toBe(0); expect(result.blockedRays).toBe(0); expect(result.blockers).toEqual([]);
});
