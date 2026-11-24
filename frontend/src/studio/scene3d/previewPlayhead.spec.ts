import { describe, it, expect } from 'vitest';
import * as THREE from 'three';
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js';
import { PreviewPlayhead, PREVIEW_FPS, previewFrameCount, previewFrameTime } from './previewPlayhead';

describe('24fps session playhead', () => {
  it('preserves exact random-access and stop baselines with real OrbitControls', () => {
    const camera = new THREE.PerspectiveCamera(47);
    camera.position.set(4.5, 3.199999999999998, 6);
    const controls = new OrbitControls(camera, document.createElement('canvas'));
    controls.target.set(.125, .75, .375); controls.enableDamping = true; controls.update();
    const snapshot = () => ({ p: camera.position.toArray(), q: camera.quaternion.toArray(), target: controls.target.toArray(), fov: camera.fov });
    const baseline = snapshot();
    const session = new PreviewPlayhead(camera, controls);
    const sample = (t: number) => {
      session.sample(t, 2, time => {
        camera.position.set(4 + time, 3, 2); controls.target.set(time / 3, .75, .375);
        camera.fov = 55; camera.updateProjectionMatrix(); controls.update();
      });
      return snapshot();
    };
    const first = sample(1); sample(.25); sample(3);
    expect(sample(1)).toEqual(first);
    session.stop(); expect(snapshot()).toEqual(baseline);
    expect(controls.enabled).toBe(true); expect(controls.enableDamping).toBe(true);
    controls.dispose();
  });
  it('clamps frame edges and represents a fractional final frame exactly', () => {
    expect(PREVIEW_FPS).toBe(24);
    expect(previewFrameCount(1.01)).toBe(25);
    expect(previewFrameTime(25, 1.01)).toEqual({ frame: 25, seconds: 1.01 });
    expect(previewFrameTime(24, 1.01).seconds).toBe(1);
    expect(previewFrameTime(-4, 1)).toEqual({ frame: 0, seconds: 0 });
    expect(previewFrameTime(Infinity, 1).seconds).toBe(0);
    expect(previewFrameCount(NaN)).toBe(0);
    expect(previewFrameTime(2, -1)).toEqual({ frame: 0, seconds: 0 });
  });
  it('seeks out of order, freezes skeletal time at the movement end and restores the exact baseline', () => {
    const camera = new THREE.PerspectiveCamera(47); camera.position.set(4, 3, 2);
    const controls = { target: new THREE.Vector3(1, 2, 3), enabled: true, update() {} };
    const mover = new THREE.Group(); mover.position.set(8, 0, 9); mover.rotation.y = .4;
    const bone = new THREE.Bone(); bone.name = 'joint'; bone.position.y = 7; mover.add(bone);
    const clip = new THREE.AnimationClip('test', 4, [new THREE.VectorKeyframeTrack('joint.position', [0, 4], [0, 0, 0, 0, 4, 0])]);
    const baseline = { p: mover.position.toArray(), q: mover.quaternion.toArray(), cp: camera.position.toArray(), cq: camera.quaternion.toArray(), target: controls.target.toArray() };
    const session = new PreviewPlayhead(camera, controls, mover, clip);
    expect(controls.enabled).toBe(false);
    const apply = (t: number) => { mover.position.x = Math.min(t, 2); camera.position.x = t + mover.position.x; camera.fov = 60; };
    for (const t of [1, .25, 3, 1]) {
      session.sample(t, 2, apply);
      expect(bone.position.y).toBeCloseTo(Math.min(t, 2), 6);
      expect(camera.position.x).toBe(t + Math.min(t, 2));
    }
    session.stop(); session.stop(); session.sample(2, 2, apply);
    expect(mover.position.toArray()).toEqual(baseline.p); expect(mover.quaternion.toArray()).toEqual(baseline.q);
    expect(camera.position.toArray()).toEqual(baseline.cp); expect(camera.quaternion.toArray()).toEqual(baseline.cq);
    expect(controls.target.toArray()).toEqual(baseline.target); expect(controls.enabled).toBe(true); expect(camera.fov).toBe(47);
    expect(bone.position.y).toBe(7);
  });
});
