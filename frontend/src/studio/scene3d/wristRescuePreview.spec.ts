import { expect, it } from 'vitest';
import * as THREE from 'three';
import { createWristRescuePreview } from './wristRescuePreview';
import { createScriptShotSampler } from './scriptShotSampler';
import { serializeBonePose } from './posing/bonePose';
import { disposeObject } from './toonflow/sceneResources';
import { proveWalkableLine } from './ground-path';

it('stages the man in front of the woman facing her, never side by side', () => {
  const { entries } = createWristRescuePreview();
  try {
    const [man, woman] = entries.map(e => e.object);
    const towardMan = man.position.clone().sub(woman.position).normalize();
    const forward = (root: THREE.Object3D) => new THREE.Vector3(0, 0, 1).applyQuaternion(root.quaternion);
    expect(forward(woman).dot(towardMan)).toBeGreaterThan(.99);
    expect(forward(man).dot(towardMan)).toBeLessThan(-.99);
    expect(man.position.distanceTo(woman.position)).toBeGreaterThan(.7);
  } finally { entries.forEach(e => disposeObject(e.object)); }
});

it('keeps roots on the dry bank while the backward fall projects over the real pond behind her', () => {
  const { entries, plan, environment } = createWristRescuePreview();
  try {
    expect(environment.terrain).not.toBeNull();
    const terrain = environment.terrain!;
    for (const e of entries) {
      const p = { x: e.object.position.x, z: e.object.position.z };
      expect(proveWalkableLine(terrain, p, p).ok).toBe(true);
    }
    const waterPoint = { x: 0, z: -.5 };
    expect(proveWalkableLine(terrain, waterPoint, waterPoint).reason).toContain('水域');
    const sampler = createScriptShotSampler(plan, { camera: new THREE.PerspectiveCamera(), controls: { target: new THREE.Vector3(), enabled: true, update() {} }, entries: () => entries, environment });
    try {
      sampler.sampleAt(1.9);
      const head = entries[1].object.getObjectByName('head')!.getWorldPosition(new THREE.Vector3());
      const projection = { x: head.x, z: head.z };
      expect(proveWalkableLine(terrain, projection, projection).reason).toContain('水域');
      expect(entries[1].object.position.z).toBe(0);
      expect(new THREE.Vector3(0, 0, 1).applyQuaternion(entries[0].object.quaternion).z).toBeLessThan(-.99);
    } finally { sampler.stop(); }
  } finally { entries.forEach(e => disposeObject(e.object)); }
}, 60_000);

it('actually leans the woman backwards, extends then retracts the man arm, and recovers with locked support feet', () => {
  const { entries, plan } = createWristRescuePreview();
  const man = entries[0].object, woman = entries[1].object;
  const position = (r: THREE.Object3D, name: string) => r.getObjectByName(name)!.getWorldPosition(new THREE.Vector3());
  const baseline = entries.map(e => serializeBonePose(e.object));
  const sampler = createScriptShotSampler(plan, { camera: new THREE.PerspectiveCamera(), controls: { target: new THREE.Vector3(), update() {} }, entries: () => entries, environment: null });
  const distance = () => position(man, 'leftUpperArm').distanceTo(position(man, 'leftHand'));
  try {
    sampler.sampleAt(0);
    const start = position(woman, 'head'), hips = position(woman, 'hips');
    const feet = ['leftFoot', 'rightFoot'].map(n => position(woman, n));
    const readyReach = distance();
    sampler.sampleAt(1.9);
    const fallen = position(woman, 'head'), fallingHips = position(woman, 'hips'), fullReach = distance();
    expect(fallen.z).toBeLessThan(start.z - .45);
    expect(fallen.y).toBeLessThan(start.y - .25);
    expect(fallingHips.y).toBeLessThan(hips.y - .1);
    expect(fallen.z).toBeLessThan(Math.min(...feet.map(p => p.z)) - .4);
    expect(fullReach).toBeGreaterThan(readyReach + .08);
    sampler.sampleAt(4.1);
    expect(position(woman, 'head').z).toBeGreaterThan(fallen.z + .4);
    expect(position(woman, 'head').y).toBeGreaterThan(fallen.y + .25);
    expect(distance()).toBeLessThan(fullReach - .08);
    const pose = serializeBonePose(woman);
    for (const time of [6, 0, 1.9, 3.1, 4.1]) {
      sampler.sampleAt(time);
      const a = man.getObjectByName('leftHand')!.localToWorld(new THREE.Vector3(.066, -.02, .0047));
      const b = woman.getObjectByName('rightHand')!.localToWorld(new THREE.Vector3(-.066, -.02, .0047));
      expect(a.distanceTo(b)).toBeLessThan(1e-5);
      ['leftFoot', 'rightFoot'].forEach((n, i) => expect(position(woman, n).distanceTo(feet[i])).toBeLessThan(.003));
    }
    expect(serializeBonePose(woman)).toEqual(pose);
    expect(sampler.walkProof.contact!.maxSurfaceErrorMetres).toBeLessThan(.002);
  } finally {
    sampler.stop(); expect(entries.map(e => serializeBonePose(e.object))).toEqual(baseline);
    entries.forEach(e => disposeObject(e.object));
  }
}, 60_000);
