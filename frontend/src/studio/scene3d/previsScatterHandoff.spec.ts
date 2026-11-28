import * as THREE from 'three';
import { expect, it } from 'vitest';
import { createPrevisEventRuntime, sessionEvents, setSessionEvents, type PrevisEvent } from './previsEvents';
import { createScriptShotSampler } from './scriptShotSampler';
import { validateScriptShotPlan } from './scriptShotPlan';
const base = { offset: [0, 0, 0], rotation: [0, 0, 0], scale: 1, velocity: [2, 3, 0] } as const;
const round = (object: THREE.Object3D) => object.position.toArray().map(n => Math.round(n * 1e6) / 1e6);
function fixture(scatterStart = 1, velocity: [number, number, number] = [2, 3, 0]) {
  const scene = new THREE.Scene(), actor = new THREE.Group(), hand = new THREE.Bone(), prop = new THREE.Group();
  hand.name = 'hand'; hand.position.set(1, 2, 0); hand.rotation.y = Math.PI / 2; actor.add(hand);
  scene.add(actor, prop); prop.position.set(10, 0, 0);
  const attach = { ...base, kind: 'attach', objectId: 'prop', targetId: 'actor', bone: 'hand', start: 0, end: 1 } as PrevisEvent;
  const scatter = { ...base, kind: 'scatter', objectId: 'prop', start: scatterStart, end: 2, velocity } as PrevisEvent;
  setSessionEvents(scene, [attach, scatter]);
  const entries = [{ id: 'actor', name: 'actor', object: actor }, { id: 'prop', name: 'prop', object: prop }];
  return { scene, actor, hand, prop, entries, attach, scatter };
}
it('launches a contiguous scatter from the released bone pose, keeping the authored velocity', () => {
  const { hand, prop, entries } = fixture(), runtime = createPrevisEventRuntime(entries, 3, new Set());
  try {
    hand.rotation.y = 0; hand.updateMatrixWorld(true);
    const t = .25; runtime.sample(1 + t);
    // Release pose is the bone world transform at sample time: offset zero => exactly the bone.
    expect(round(prop)).toEqual([1 + 2 * t, 2 + 3 * t - 4.905 * t * t, 0].map(n => Math.round(n * 1e6) / 1e6));
    expect(prop.quaternion.angleTo(new THREE.Quaternion())).toBeLessThan(1e-9);
  } finally { runtime.stop(); }
});
it('still follows the authored formula when only the velocity changes', () => {
  const fast = fixture(1, [10, 3, 0]), slow = fixture(1, [2, 3, 0]);
  const a = createPrevisEventRuntime(fast.entries, 3, new Set()), b = createPrevisEventRuntime(slow.entries, 3, new Set());
  try {
    fast.hand.rotation.y = 0; slow.hand.rotation.y = 0; fast.hand.updateMatrixWorld(true); slow.hand.updateMatrixWorld(true);
    a.sample(1.5); b.sample(1.5);
    expect(fast.prop.position.x - slow.prop.position.x).toBeCloseTo(8 * .5, 9);
    expect(fast.prop.position.y).toBeCloseTo(slow.prop.position.y, 9);
  } finally { a.stop(); b.stop(); }
});
it('falls back to the editing pose when the boundary has a gap, keeping no implicit handoff', () => {
  const gapped = fixture(1.001), entries = gapped.entries, runtime = createPrevisEventRuntime(entries, 3, new Set());
  try {
    runtime.sample(1.001 + .25);
    expect(round(gapped.prop)).toEqual([10.5, .75 - 4.905 * .0625, 0].map(n => Math.round(n * 1e6) / 1e6));
  } finally { runtime.stop(); }
});
it('keeps the released parent rotation and applies spin about it', () => {
  const { hand, prop, entries, scatter } = fixture();
  const scene = prop.parent!; setSessionEvents(scene, [fixture().attach, { ...scatter, rotation: [0, 2, 0] }]);
  const runtime = createPrevisEventRuntime(entries, 3, new Set());
  try {
    hand.rotation.set(0, Math.PI / 2, 0); hand.updateMatrixWorld(true); runtime.sample(1.25);
    const expected = new THREE.Quaternion().setFromEuler(new THREE.Euler(0, Math.PI / 2, 0)).multiply(new THREE.Quaternion().setFromEuler(new THREE.Euler(0, .5, 0)));
    expect(prop.quaternion.angleTo(expected)).toBeLessThan(1e-9);
  } finally { runtime.stop(); }
});
it('degenerates to the parent editing pose when a chained parent attach is not active at release time', () => {
  const scene = new THREE.Scene(), actor = new THREE.Group(), hand = new THREE.Bone(), sword = new THREE.Group(), tassel = new THREE.Group();
  hand.name = 'hand'; hand.position.set(1, 2, 0); actor.add(hand); sword.position.set(6, 0, 0); tassel.position.set(-6, 0, 0);
  scene.add(actor, sword, tassel);
  const links = [
    { ...base, kind: 'attach', objectId: 'sword', targetId: 'actor', bone: 'hand', start: 0, end: .5 },
    { ...base, kind: 'attach', objectId: 'tassel', targetId: 'sword', bone: 'tip', start: 0, end: 1 },
    { ...base, kind: 'scatter', objectId: 'tassel', start: 1, end: 2, velocity: [1, 0, 0], offset: [0, 0, 0] },
  ] as PrevisEvent[];
  const tip = new THREE.Bone(); tip.name = 'tip'; sword.add(tip);
  setSessionEvents(scene, links);
  const entries = [{ id: 'actor', name: 'actor', object: actor }, { id: 'sword', name: 'sword', object: sword }, { id: 'tassel', name: 'tassel', object: tassel }];
  const runtime = createPrevisEventRuntime(entries, 3, new Set());
  try {
    runtime.sample(1.5);
    // Parent attach already ended, so the sword sits at its editing pose (6,0,0) and the tassel releases from there.
    expect(round(tassel)).toEqual([6.5, -1.22625, 0]);
  } finally { runtime.stop(); }
  expect(round(sword)).toEqual([6, 0, 0]); expect(round(tassel)).toEqual([-6, 0, 0]);
});
it('rejects an ambiguous handoff, a premature missing bone, and a bone removed mid-session', () => {
  const ambiguous = fixture();
  const scene = ambiguous.prop.parent!;
  expect(() => setSessionEvents(scene, [...sessionEvents(scene), { ...ambiguous.attach, start: .5, end: 1 }])).toThrow('区间重叠');
  const duplicated = fixture(), second = new THREE.Bone(); second.name = 'hand'; duplicated.actor.add(second);
  expect(() => createPrevisEventRuntime(duplicated.entries, 3, new Set())).toThrow('骨骼缺失或重名');
  const renamed = fixture(); renamed.hand.name = 'wrist';
  expect(() => createPrevisEventRuntime(renamed.entries, 3, new Set())).toThrow('骨骼缺失或重名');
  const removed = fixture(), runtime = createPrevisEventRuntime(removed.entries, 3, new Set());
  try { removed.hand.removeFromParent(); expect(() => runtime.sample(1.5)).toThrow('已移除'); } finally { runtime.stop(); }
  expect(round(removed.prop)).toEqual([10, 0, 0]);
});
it('restores on stop and shares the handoff through the production sampler', () => {
  const { scene, hand, prop, entries } = fixture();
  const plan = validateScriptShotPlan({ version: 1, sceneId: 's', heading: '', location: '', sourceFingerprint: 'src', sceneFingerprint: 'rev', diagnostics: [], action: [], shots: [{ id: 's', order: 1, start: 0, end: 3, dialogue: [], camera: { position: [0, 3, 8], target: [0, 1, 0], fov: 50 } }] });
  const camera = new THREE.PerspectiveCamera(50, 1, .1, 100), controls = { target: new THREE.Vector3(), enabled: true, enableDamping: true, update() {} };
  const sampler = createScriptShotSampler(plan, { camera, controls, entries: () => entries, environment: null, baselines: new Map(), isCurrent: () => true });
  try {
    hand.rotation.y = 0; hand.updateMatrixWorld(true);
    sampler.sampleAt(1.5); expect(round(prop)).toEqual([2, 3.5 - 4.905 * .25, 0].map(n => Math.round(n * 1e6) / 1e6));
  } finally { sampler.stop(); }
  expect(round(prop)).toEqual([10, 0, 0]); expect(controls.enabled).toBe(true); expect(prop.parent).toBe(scene);
});