import * as THREE from 'three';
import { expect, it } from 'vitest';
import { attachSamplingOrder, createPrevisEventRuntime, sessionEvents, setSessionEvents, type PrevisEvent } from './previsEvents';
import { createScriptShotSampler } from './scriptShotSampler';
import { validateScriptShotPlan } from './scriptShotPlan';
const base = { offset: [0, 0, 0], rotation: [0, 0, 0], scale: 1, velocity: [0, 0, 0] } as const;
const round = (object: THREE.Object3D) => object.position.toArray().map(n => Math.round(n * 1e6) / 1e6);
function fixture(order: 'parent-first' | 'child-first' = 'parent-first') {
  const scene = new THREE.Scene(), actor = new THREE.Group(), hand = new THREE.Bone(), sword = new THREE.Group(), tip = new THREE.Bone(), tassel = new THREE.Group();
  hand.name = 'hand'; hand.position.set(1, 2, 0); actor.add(hand);
  tip.name = 'tip'; tip.position.set(.5, 0, 0); sword.add(tip);
  scene.add(actor, sword, tassel); sword.position.set(9, 0, 0); tassel.position.set(-9, 0, 0);
  const parent = { ...base, kind: 'attach', objectId: 'sword', targetId: 'actor', bone: 'hand', start: .5, end: 2 } as PrevisEvent;
  const child = { ...base, kind: 'attach', objectId: 'tassel', targetId: 'sword', bone: 'tip', start: .5, end: 2 } as PrevisEvent;
  const events = order === 'parent-first' ? [parent, child] : [child, parent];
  setSessionEvents(scene, events);
  const entries = [{ id: 'actor', name: 'actor', object: actor }, { id: 'sword', name: 'sword', object: sword }, { id: 'tassel', name: 'tassel', object: tassel }];
  return { scene, actor, hand, sword, tip, tassel, entries, events, parent, child };
}
it('places a chained child on its parent bone for both external list orders', () => {
  let reference: number[] | undefined;
  for (const order of ['parent-first', 'child-first'] as const) {
    const { hand, tassel, sword, scene, entries, events } = fixture(order), before = JSON.stringify(events);
    const runtime = createPrevisEventRuntime(entries, 3, new Set());
    try {
      hand.rotation.y = Math.PI / 2; hand.updateMatrixWorld(true);
      for (const time of [1.5, .5, 1.5]) runtime.sample(time);
      expect(round(tassel)).toEqual([1, 2, -.5]);
      if (!reference) reference = tassel.position.toArray(); else expect(tassel.position.toArray()).toEqual(reference);
      expect(JSON.stringify(sessionEvents(scene))).toBe(before);
      runtime.sample(2); expect(round(sword)).toEqual([9, 0, 0]); expect(round(tassel)).toEqual([-9, 0, 0]);
    } finally { runtime.stop(); }
  }
});
it('orders attach chains by depth, then external index, and ignores non-overlapping parents', () => {
  const { events, parent, child } = fixture('child-first');
  expect(attachSamplingOrder(events)).toEqual([1, 0]);
  expect(attachSamplingOrder([{ ...parent, start: 0, end: .5 }, { ...child, start: .5, end: 1 }])).toEqual([0, 1]);
  const veil = { ...base, kind: 'attach', objectId: 'veil', targetId: 'tassel', bone: 'pin', start: 0, end: 3 } as PrevisEvent;
  const three = [{ ...parent, start: 0, end: 3 }, { ...child, start: 0, end: 3 }, veil];
  const ordered = attachSamplingOrder(three);
  expect(ordered).toHaveLength(3);
  expect(ordered.indexOf(0)).toBeLessThan(ordered.indexOf(1));
  expect(ordered.indexOf(1)).toBeLessThan(ordered.indexOf(2));
  // Same dependency ranks regardless of external order: depths 0/1/2.
  const reversed = attachSamplingOrder([veil, { ...child, start: 0, end: 3 }, { ...parent, start: 0, end: 3 }]);
  expect(reversed).toEqual([2, 1, 0]);
});
it('keeps an active child following the parent editing pose while the parent attach is inactive', () => {
  const { scene, hand, sword, tassel, entries, parent, child } = fixture();
  setSessionEvents(scene, [{ ...parent, start: 1 }, child]);
  const runtime = createPrevisEventRuntime(entries, 3, new Set());
  try {
    runtime.sample(.75); expect(round(sword)).toEqual([9, 0, 0]); expect(round(tassel)).toEqual([9.5, 0, 0]);
    hand.rotation.y = Math.PI / 2; runtime.sample(1.5); expect(round(tassel)).toEqual([1, 2, -.5]);
  } finally { runtime.stop(); }
  expect(round(tassel)).toEqual([-9, 0, 0]);
});
it('rejects an overlapping attach cycle and a ballistic parent without writing the session', () => {
  const { scene, events, child } = fixture();
  const before = JSON.stringify(sessionEvents(scene));
  expect(() => setSessionEvents(scene, [...events, { ...child, objectId: 'actor', targetId: 'tassel', start: .5, end: 2 }])).toThrow('循环依赖');
  expect(() => setSessionEvents(scene, [{ ...child, objectId: 'tassel', targetId: 'sword', bone: 'tip' }, { ...base, kind: 'scatter', objectId: 'sword', start: 1, end: 2 } as PrevisEvent])).toThrow('飞散部件');
  expect(JSON.stringify(sessionEvents(scene))).toBe(before);
});
it('fails before sampling when a chained bone is missing or ambiguous, and mid-sample when removed', () => {
  const missing = fixture(); missing.tip.name = 'renamed';
  expect(() => createPrevisEventRuntime(missing.entries, 3, new Set())).toThrow('骨骼缺失或重名');
  expect(round(missing.sword)).toEqual([9, 0, 0]); expect(round(missing.tassel)).toEqual([-9, 0, 0]);
  const duplicate = fixture(); const second = new THREE.Bone(); second.name = 'tip'; duplicate.sword.add(second);
  expect(() => createPrevisEventRuntime(duplicate.entries, 3, new Set())).toThrow('骨骼缺失或重名');
  const removed = fixture(), runtime = createPrevisEventRuntime(removed.entries, 3, new Set());
  try {
    runtime.sample(.75); expect(round(removed.tassel)).not.toEqual([-9, 0, 0]);
    removed.tip.removeFromParent();
    expect(() => runtime.sample(1)).toThrow('已移除');
  } finally { runtime.stop(); }
  expect(round(removed.sword)).toEqual([9, 0, 0]); expect(round(removed.tassel)).toEqual([-9, 0, 0]);
});
it('restores every chained object on stop and after both effects end', () => {
  const { hand, sword, tassel, entries } = fixture();
  const runtime = createPrevisEventRuntime(entries, 3, new Set());
  try {
    hand.rotation.y = Math.PI / 2; runtime.sample(1); expect(sword.position.x).not.toBe(9);
    runtime.sample(2); expect(round(sword)).toEqual([9, 0, 0]); expect(round(tassel)).toEqual([-9, 0, 0]);
  } finally { runtime.stop(); }
  expect(round(sword)).toEqual([9, 0, 0]); expect(round(tassel)).toEqual([-9, 0, 0]);
});
it('shares the chain order through the production sampler and restores on stop', () => {
  const { scene, hand, sword, tassel, entries } = fixture('child-first');
  const plan = validateScriptShotPlan({ version: 1, sceneId: 's', heading: '', location: '', sourceFingerprint: 'src', sceneFingerprint: 'rev', diagnostics: [], action: [], shots: [{ id: 's', order: 1, start: 0, end: 3, dialogue: [], camera: { position: [0, 3, 8], target: [0, 1, 0], fov: 50 } }] });
  const camera = new THREE.PerspectiveCamera(50, 1, .1, 100), controls = { target: new THREE.Vector3(), enabled: true, enableDamping: true, update() {} };
  const sampler = createScriptShotSampler(plan, { camera, controls, entries: () => entries, environment: null, baselines: new Map(), isCurrent: () => true });
  try {
    hand.rotation.y = Math.PI / 2; const sample = sampler.sampleAt(1);
    expect(sample.seconds).toBe(1); expect(round(tassel)).toEqual([1, 2, -.5]); expect(round(sword)).toEqual([1, 2, 0]);
  } finally { sampler.stop(); }
  expect(round(sword)).toEqual([9, 0, 0]); expect(round(tassel)).toEqual([-9, 0, 0]); expect(controls.enabled).toBe(true); expect(scene.children).toContain(sword);
});
it('roundtrips a chain through the independent sidecar and still rejects a cycle there', async () => {
  const { serializeEventSidecar, parseEventSidecar } = await import('./previsEventSidecar');
  const { events, child } = fixture('child-first');
  const text = serializeEventSidecar('source', events);
  expect(parseEventSidecar(text, 'source').events).toEqual(events);
  expect(() => serializeEventSidecar('source', [...events, { ...child, objectId: 'actor', targetId: 'tassel' }])).toThrow('循环依赖');
});
