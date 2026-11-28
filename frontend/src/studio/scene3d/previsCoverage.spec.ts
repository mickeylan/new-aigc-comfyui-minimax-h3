import * as THREE from 'three';
import { expect, it } from 'vitest';
import { coverageCandidates } from './previsCoverage';
const setup = () => {
  const entries = [-3, 3].map((x, i) => { const object = new THREE.Mesh(new THREE.BoxGeometry(1, 2, 1), new THREE.MeshBasicMaterial()); object.position.set(x, 1, 0); return { id: String(i), name: String(i), object }; });
  const shot: any = { id: 's', start: 0, end: 2, camera: { position: [0, 3, 10], target: [0, 1, 0], fov: 50 }, execution: { version: 1, actors: entries.map(e => ({ targetId: e.id, beats: [{ from: { x: e.object.position.x, z: 0 }, to: { x: e.object.position.x, z: 2 } }] })) } };
  return { entries, shot };
};
it.each([16 / 9, 9 / 16])('fits multiple subjects for aspect %s and warns crossing without claiming occlusion', aspect => {
  const { entries, shot } = setup(), before = JSON.stringify(shot), positions = entries.map(e => e.object.position.toArray());
  const candidates = coverageCandidates(shot, entries, aspect);
  expect(candidates).toHaveLength(3); expect(candidates[0].axisRisk).toBe('same-side'); expect(candidates[1].axisRisk).toBe('crosses');
  expect(candidates.every(c => c.frustumContainsBounds && !c.occlusionTested)).toBe(true);
  expect(candidates[0].subjects).toEqual(['0', '1']); expect(JSON.stringify(shot)).toBe(before); expect(entries.map(e => e.object.position.toArray())).toEqual(positions);
});
it.each(['missing', 'hidden', 'duplicate', 'track', 'aspect'])('rejects %s input', mode => {
  const { entries, shot } = setup();
  if (mode === 'missing') entries.pop(); if (mode === 'hidden') entries[0].object.visible = false;
  if (mode === 'duplicate') entries.push(entries[0]); if (mode === 'track') shot.execution.camera = { keys: [] };
  expect(() => coverageCandidates(shot, entries, mode === 'aspect' ? NaN : 1)).toThrow();
});
