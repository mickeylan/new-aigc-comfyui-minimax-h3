import * as THREE from 'three';
import { expect, it } from 'vitest';
import { createScriptShotSampler } from './scriptShotSampler';
import { validateScriptShotPlan } from './scriptShotPlan';
import { createPrevisEventRuntime, sessionEvents, setSessionEvents, type PrevisEvent } from './previsEvents';
const event = (kind: PrevisEvent['kind']): PrevisEvent => ({ kind, objectId: 'prop', targetId: 'actor', bone: 'hand', start: .5, end: 2, offset: [1, 0, 0], rotation: [0, 1, 0], scale: 1, velocity: [2, 3, 0] });
function fixture() { const scene = new THREE.Scene(), actor = new THREE.Group(), prop = new THREE.Mesh(new THREE.BoxGeometry(), new THREE.MeshBasicMaterial()), bone = new THREE.Bone(); bone.name = 'hand'; bone.position.set(0, 2, 0); actor.add(bone); scene.add(actor, prop); prop.position.set(10, 0, 0); return { scene, actor, prop, bone, entries: [{ id: 'actor', object: actor }, { id: 'prop', object: prop }] }; }
it('attaches by actual bone world matrix without reparenting and restores on seek/stop', () => {
  const { scene, prop, bone, entries } = fixture(); setSessionEvents(scene, [event('attach')]);
  const runtime = createPrevisEventRuntime(entries, 3, new Set(['actor'])); runtime.sample(1); expect(prop.parent).toBe(scene); expect(prop.position.toArray()).toEqual([1, 2, 0]);
  bone.position.x = 2; runtime.sample(1.5); expect(prop.position.toArray()).toEqual([3, 2, 0]); runtime.sample(.1); expect(prop.position.toArray()).toEqual([10, 0, 0]); runtime.sample(1); runtime.stop(); expect(prop.position.toArray()).toEqual([10, 0, 0]);
  expect(prop.userData).toEqual({}); expect(sessionEvents(new THREE.Scene())).toEqual([]);
});
it('samples deterministic fire/smoke and scatter, disposing derived particles at stop', () => {
  const { scene, entries, prop } = fixture(); setSessionEvents(scene, [event('fire'), event('smoke'), { ...event('scatter'), start: 0 }]);
  const runtime = createPrevisEventRuntime(entries, 3, new Set(['actor'])); runtime.sample(1);
  expect(prop.position.x).toBe(12); const particles = scene.children.filter(o => o.type === 'Points') as THREE.Points[]; expect(particles).toHaveLength(2);
  const first = Array.from(particles[0].geometry.attributes.position.array); runtime.sample(1); expect(Array.from((scene.children.find(o => o.type === 'Points') as THREE.Points).geometry.attributes.position.array)).toEqual(first);
  runtime.stop(); expect(scene.children).toHaveLength(2); expect(prop.position.toArray()).toEqual([10, 0, 0]);
});
it('shares actual timeline sampling, masks borrowed prop TRS and restores on stop and runtime failure', () => {
  const { scene, entries, prop, bone } = fixture(); setSessionEvents(scene, [{ ...event('attach'), scale: 2 }]);
  const plan = validateScriptShotPlan({ version: 1, sceneId: 's', heading: 'h', location: 'l', sourceFingerprint: 'src', sceneFingerprint: 'rev', diagnostics: [], action: [], shots: [{ id: 's', order: 1, start: 0, end: 3, dialogue: [], camera: { position: [0, 3, 8], target: [0, 1, 0], fov: 50 } }] });
  const baselines = new Map(), camera = new THREE.PerspectiveCamera(), controls = { target: new THREE.Vector3(), enabled: true, update() {} };
  const sampler = createScriptShotSampler(plan, { camera, controls, entries: () => entries.map(e => ({ ...e, name: e.id })), environment: null, baselines });
  sampler.sampleAt(1); expect(prop.scale.toArray()).toEqual([2, 2, 2]); expect(baselines.get('prop').scale.toArray()).toEqual([1, 1, 1]);
  bone.removeFromParent(); expect(() => sampler.sampleAt(1)).toThrow('附着骨骼'); expect(prop.position.toArray()).toEqual([10, 0, 0]); expect(prop.scale.toArray()).toEqual([1, 1, 1]); expect(baselines.size).toBe(0); expect(controls.enabled).toBe(true);
  sampler.stop(); prop.position.x = 12; sampler.stop(); expect(prop.position.x).toBe(12);
});
it.each(['bone', 'overlap', 'actor', 'duration', 'numeric'])('rejects invalid %s contracts before mutation', mode => {
  const { scene, entries, prop, bone } = fixture(), e = event('attach');
  if (mode === 'bone') bone.name = 'wrong'; if (mode === 'numeric') e.offset[0] = NaN;
  const run = () => { setSessionEvents(scene, mode === 'overlap' ? [e, e] : [e]); createPrevisEventRuntime(entries, mode === 'duration' ? 1 : 3, new Set(mode === 'actor' ? ['prop'] : ['actor'])); };
  expect(run).toThrow(); expect(prop.position.toArray()).toEqual([10, 0, 0]); expect(prop.parent).toBe(scene);
});
