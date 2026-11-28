import * as THREE from 'three';
import { expect, it, vi } from 'vitest';
import { createPrevisEventRuntime, sessionEvents, setSessionEvents, type PrevisEvent } from './previsEvents';
import { createScriptShotSampler } from './scriptShotSampler';
import { validateScriptShotPlan } from './scriptShotPlan';
const permutations = [[0,1,2],[0,2,1],[1,0,2],[1,2,0],[2,0,1],[2,1,0]];
function fixture(kind: 'attach' | 'scatter', order: number[]) {
  const scene = new THREE.Scene(), actor = new THREE.Group(), prop = new THREE.Group(), bone = new THREE.Bone();
  scene.position.set(3, 1, -2); scene.rotation.y = .7; scene.scale.setScalar(2);
  bone.name = 'hand'; bone.position.set(0, 2, 0); actor.add(bone); scene.add(actor, prop); prop.position.set(10, 0, 0);
  const base = { objectId: 'prop', start: .5, end: 2, offset: [1, 0, 0], rotation: [0, 1, 0], scale: 1, velocity: [2, 3, 0] } as const;
  const all = [{ ...base, kind, targetId: 'actor', bone: 'hand' }, { ...base, kind: 'fire' }, { ...base, kind: 'smoke' }] as unknown as PrevisEvent[];
  const events = order.map(i => all[i]); setSessionEvents(scene, events);
  const entries = [{ id: 'actor', name: 'actor', object: actor }, { id: 'prop', name: 'prop', object: prop }];
  return { scene, actor, bone, prop, entries, events };
}
const points = (scene: THREE.Scene) => scene.children.filter(o => (o as THREE.Points).isPoints) as THREE.Points[];
function assertAnchors(scene: THREE.Scene, prop: THREE.Object3D) {
  const expected = prop.getWorldPosition(new THREE.Vector3()).add(new THREE.Vector3(1, 0, 0));
  expect(points(scene)).toHaveLength(2);
  for (const p of points(scene)) expect(p.getWorldPosition(new THREE.Vector3()).distanceTo(expected)).toBeLessThan(1e-6);
}
it.each(['attach', 'scatter'] as const)('makes %s -> fire/smoke deterministic for all six event orders and reverse seek', kind => {
  let reference: unknown;
  for (const order of permutations) {
    const { scene, prop, bone, entries, events } = fixture(kind, order), before = JSON.stringify(events);
    const runtime = createPrevisEventRuntime(entries, 3, new Set(['actor']));
    try {
      bone.position.x = 2;
      for (const time of [1.5, .5, 1, 1]) {
        runtime.sample(time); assertAnchors(scene, prop);
      }
      const current = { position: prop.position.toArray(), quaternion: prop.quaternion.toArray(), particles: points(scene).map(p => ({ color: (p.material as THREE.PointsMaterial).color.getHex(), position: p.position.toArray(), vertices: Array.from(p.geometry.attributes.position.array) })).sort((a,b) => a.color - b.color) };
      if (!reference) reference = current; else expect(current).toEqual(reference);
      expect(JSON.stringify(sessionEvents(scene))).toBe(before);
      const prior = points(scene), dispose = prior.map(p => [vi.spyOn(p.geometry, 'dispose'), vi.spyOn(p.material as THREE.Material, 'dispose')]);
      runtime.sample(2); expect(points(scene)).toHaveLength(0); expect(prop.position.toArray()).toEqual([10, 0, 0]);
      for (const pair of dispose) for (const spy of pair) expect(spy).toHaveBeenCalledTimes(1);
      runtime.sample(.49); expect(points(scene)).toHaveLength(0); runtime.sample(.5); assertAnchors(scene, prop);
    } finally { runtime.stop(); }
    expect(points(scene)).toHaveLength(0); expect(prop.position.toArray()).toEqual([10, 0, 0]);
    runtime.stop(); expect(prop.parent).toBe(scene);
  }
});
it.each(['attach', 'scatter'] as const)('keeps independent effect lifetimes when %s ends or starts during fire/smoke', kind => {
  const { scene, prop, entries, events } = fixture(kind, [1,2,0]);
  for (const e of events) { if (e.kind === kind) { e.start = 1; e.end = 1.5; } else { e.start = .5; e.end = 2; } }
  setSessionEvents(scene, events); const runtime = createPrevisEventRuntime(entries, 3, new Set(['actor']));
  try {
    for (const time of [.5, 1, 1.5, 1.25, .75, 1.5]) {
      runtime.sample(time); assertAnchors(scene, prop);
      if (time < 1 || time >= 1.5) expect(prop.position.toArray()).toEqual([10,0,0]);
      else if (kind === 'scatter') {
        const t = time - 1;
        expect(prop.position.x).toBeCloseTo(10 + 2 * t, 8);
        expect(prop.position.y).toBeCloseTo(3 * t - 4.905 * t * t, 8);
      } else expect(prop.position.toArray()).not.toEqual([10,0,0]);
    }
    runtime.sample(2); expect(points(scene)).toHaveLength(0);
  } finally { runtime.stop(); }
});
// Expectation intentionally changed by ADR-0030: chained attach (target itself
// attached at the same time) is now allowed; overlapping cycles remain rejected.
it('allows a chain parent but still rejects an overlapping cycle and overlapping transform owners', () => {
  const { scene, events } = fixture('attach', [1,2,0]);
  const attach = events.find(e => e.kind === 'attach')!;
  expect(() => setSessionEvents(scene, [...events, { ...attach, objectId: 'actor', targetId: 'prop' }])).toThrow('循环依赖');
  expect(() => setSessionEvents(scene, [...events, { ...attach, kind: 'scatter' }])).toThrow('区间重叠');
});
it('shares dependency order through production sampler and releases all effects on bone identity failure', () => {
  const { scene, prop, bone, entries } = fixture('attach', [1,2,0]);
  const plan = validateScriptShotPlan({ version: 1, sceneId: 's', heading: '', location: '', sourceFingerprint: 'src', sceneFingerprint: 'rev', diagnostics: [], action: [], shots: [{ id: 's', order: 1, start: 0, end: 3, dialogue: [], camera: { position: [0,3,8], target: [0,1,0], fov: 50 } }] });
  const baselines = new Map(), controls = { target: new THREE.Vector3(), enabled: true, update() {} };
  const sampler = createScriptShotSampler(plan, { camera: new THREE.PerspectiveCamera(), controls, entries: () => entries, environment: null, baselines });
  try {
    sampler.sampleAt(1); assertAnchors(scene, prop); expect(baselines.has('prop')).toBe(true);
    bone.position.x = 3; sampler.sampleAt(1.5); assertAnchors(scene, prop);
    bone.removeFromParent(); expect(() => sampler.sampleAt(1)).toThrow('附着骨骼');
    expect(points(scene)).toHaveLength(0); expect(prop.position.toArray()).toEqual([10,0,0]); expect(baselines.size).toBe(0); expect(controls.enabled).toBe(true);
  } finally { sampler.stop(); }
});
