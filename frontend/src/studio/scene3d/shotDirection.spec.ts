import { describe, expect, it } from 'vitest';
import * as THREE from 'three';
import { draftShotDirection, type ShotDirectionGuide } from './shotDirection';
import type { ScriptShotPlan } from './toonflowSets';

const fixture = () => {
  const object = new THREE.Mesh(new THREE.BoxGeometry(1, 2, 1), new THREE.MeshBasicMaterial());
  object.position.y = 1;
  object.animations = [new THREE.AnimationClip('explicit-human-selection', 2, [])];
  const entries = [{ id: 'actor', name: 'Actor', object }];
  const guide: ShotDirectionGuide = { speakerTargets: { Alice: 'actor' }, animationRefs: { actor: { index: 0, name: object.animations[0]!.name, duration: 2 } }, framing: 'medium', azimuth: 0, distance: 5, travel: 2, avoid: true };
  const plan: ScriptShotPlan = { version: 1, sceneId: 'scene', heading: 'heading', location: 'location', sourceFingerprint: 'source', sceneFingerprint: 'scene', action: ['untouched'], diagnostics: [], shots: [0, 1].map(i => ({ id: `s${i}`, order: i + 1, start: i * 4, end: (i + 1) * 4, camera: null, dialogue: [{ index: i, speaker: 'Alice', line: 'Original text', start: i * 4, end: (i + 1) * 4 }] })) };
  return { plan, entries, guide };
};

describe('draftShotDirection', () => {
  it('clones source and dialogue, binds only explicit target and exact selected clip', () => {
    const { plan, entries, guide } = fixture(), before = JSON.stringify(plan);
    const result = draftShotDirection(plan, entries, guide, null);
    expect(JSON.stringify(plan)).toBe(before);
    expect(result).not.toBe(plan);
    expect(result.sourceFingerprint).toBe(plan.sourceFingerprint);
    expect(result.shots.map(s => s.dialogue)).toEqual(plan.shots.map(s => s.dialogue));
    expect(result.shots[0]!.motion?.animation).toEqual(guide.animationRefs.actor);
    expect(result.shots[1]!.motion?.start).toEqual(result.shots[0]!.motion?.end);
    expect(result.shots[0]!.camera?.position).toEqual([0, 1.4, 5]);
    expect(draftShotDirection(plan, entries, guide, null)).toEqual(result);
    expect(entries[0]!.object.matrixWorld.elements).toEqual(new THREE.Matrix4().elements);
  });
  it('contains geometry offset from the movement origin in its yaw-invariant default radius', () => {
    const {plan,entries,guide}=fixture(); entries[0].object.geometry.translate(3,0,-2);
    const result=draftShotDirection(plan,entries,guide,null);
    expect(result.shots[0].motion!.navigation!.radius).toBeCloseTo(Math.hypot(3.5,2.5),6);
    expect(entries[0].object.position.toArray()).toEqual([0,1,0]);
  });
  it('retains existing camera and motion fields', () => {
    const { plan, entries, guide } = fixture();
    const first = draftShotDirection(plan, entries, guide, null);
    expect(draftShotDirection(first, entries, guide, null)).toEqual(first);
  });
  it('alternates azimuth only when explicitly requested', () => {
    const { plan, entries, guide } = fixture();
    const same = draftShotDirection(plan, entries, guide, null);
    expect(same.shots[1]!.motion?.end.z).toBe(4);
    const alternating = draftShotDirection(plan, entries, { ...guide, alternateAzimuth: true }, null);
    expect(alternating.shots[1]!.motion?.end.z).toBeCloseTo(0);
  });
  it('rejects missing speakers, invalid/locked targets, absent or stale clip refs and invalid framing', () => {
    const { plan, entries, guide } = fixture();
    expect(() => draftShotDirection(plan, entries, { ...guide, speakerTargets: {} }, null)).toThrow('speaker');
    expect(() => draftShotDirection(plan, entries, { ...guide, speakerTargets: { Alice: 'missing' } }, null)).toThrow('target');
    expect(() => draftShotDirection(plan, [{ ...entries[0]!, locked: true }], guide, null)).toThrow('target');
    expect(() => draftShotDirection(plan, entries, { ...guide, animationRefs: {} }, null)).toThrow('reference');
    entries[0]!.object.animations[0]!.duration = 3;
    expect(() => draftShotDirection(plan, entries, guide, null)).toThrow('reference');
    expect(() => draftShotDirection(plan, entries, { ...guide, framing: 'guess' as 'wide' }, null)).toThrow('framing');
  });
  it('rejects mixed-subject and unbound action-only shots instead of guessing', () => {
    const { plan, entries, guide } = fixture();
    plan.shots[0]!.dialogue = [];
    expect(() => draftShotDirection(plan, entries, guide, null)).toThrow('exactly one');
  });
});
