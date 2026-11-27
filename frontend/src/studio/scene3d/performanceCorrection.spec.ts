import { describe, expect, it, vi } from 'vitest';
import * as THREE from 'three';
import { correctExecutionBeatClip } from './performanceCorrection';
import { validateScriptShotPlan, type ScriptShotPlan, type ScriptShotMotionTarget } from './scriptShotPlan';
import { createScriptShotSampler } from './scriptShotSampler';

const clips = [{ index: 0, name: 'rest', duration: 2 }, { index: 1, name: 'raised-arm', duration: 2 }];
const targets = (): ScriptShotMotionTarget[] => ['a', 'b'].map(id => ({ id, name: id, visible: true, locked: false, animations: structuredClone(clips) }));
function plan(): ScriptShotPlan {
  return validateScriptShotPlan({ version: 1, sceneId: 'scene', heading: '人工动作稿', location: '平地', sourceFingerprint: 'source', sceneFingerprint: 'review', action: ['抬手后停下'], diagnostics: ['原文与模型补充仍需审阅'], shots: [0, 1].map(i => ({
    id: `s${i}`, order: i + 1, start: i * 4, end: i * 4 + 4, camera: { position: [0, 3, 8], target: [0, 1, 0], fov: 45 }, motion: null, dialogue: [],
    execution: { version: 1, actors: ['a', 'b'].map((id, j) => ({ targetId: id, targetName: id, beats: [0, 1].map(k => ({ start: k * 2, end: k * 2 + 2, from: { x: j * 3, z: 0 }, to: { x: j * 3, z: 0 }, yaw: 0, animation: clips[0], sourceIndex: k })) })) },
  })) });
}
const patch = () => ({ shotId: 's0', targetId: 'a', beatIndex: 1, animation: { ...clips[1] }, reason: '人工选择抬手片段，尚需核对剧情和接触安全' });

describe('manual execution clip correction', () => {
  it('changes only one beat clip, records human provenance, and never mutates the reviewed plan', () => {
    const original = plan(), before = structuredClone(original), input = patch();
    const result = correctExecutionBeatClip(original, input, targets());
    expect(original).toEqual(before);
    const expected = structuredClone(before); expected.shots[0].execution!.actors![0].beats[1].animation = clips[1];
    expect({ ...result, diagnostics: before.diagnostics }).toEqual(expected);
    const record = JSON.parse(result.diagnostics.at(-1)!);
    expect(record).toMatchObject({ kind: 'manual-performance-correction', version: 1, shotId: 's0', targetId: 'a', beatIndex: 1, start: 2, end: 4, sourceIndex: 1, previous: clips[0], replacement: clips[1], reason: input.reason, sourceFingerprint: 'source', sceneFingerprint: 'review' });
    input.animation.name = 'tampered'; expect(result.shots[0].execution!.actors![0].beats[1].animation.name).toBe('raised-arm');
    result.shots[1].execution!.actors![0].beats[0].from.x = 10; expect(original).toEqual(before);
  });
  it.each(['missing-shot', 'missing-actor', 'negative-beat', 'fractional-beat', 'missing-beat', 'blank-reason', 'long-reason', 'same-clip', 'stale-clip', 'invalid-index'])("rejects %s without editing input", failure => {
    const original = plan(), before = structuredClone(original), input = patch();
    if (failure === 'missing-shot') input.shotId = 'missing';
    if (failure === 'missing-actor') input.targetId = 'missing';
    if (failure === 'negative-beat') input.beatIndex = -1;
    if (failure === 'fractional-beat') input.beatIndex = .5;
    if (failure === 'missing-beat') input.beatIndex = 2;
    if (failure === 'blank-reason') input.reason = '  ';
    if (failure === 'long-reason') input.reason = 'x'.repeat(1001);
    if (failure === 'same-clip') input.animation = { ...clips[0] };
    if (failure === 'stale-clip') input.animation.name = 'replaced-after-selection';
    if (failure === 'invalid-index') input.animation.index = 1.5;
    expect(() => correctExecutionBeatClip(original, input, targets())).toThrow(); expect(original).toEqual(before);
  });
  it.each(['hidden', 'locked', 'missing', 'duplicate', 'old-clip-changed', 'new-clip-changed'])("rejects %s current resources", failure => {
    let live = targets();
    if (failure === 'hidden') live[0].visible = false;
    if (failure === 'locked') live[0].locked = true;
    if (failure === 'missing') live = live.slice(1);
    if (failure === 'duplicate') live.push(structuredClone(live[0]));
    if (failure === 'old-clip-changed') live[0].animations[0].duration = 1;
    if (failure === 'new-clip-changed') live[0].animations[1].duration = 1;
    expect(() => correctExecutionBeatClip(plan(), patch(), live)).toThrow();
  });
  it('preserves whole-shot contact declarations and still rejects unsafe root distance', () => {
    const original = plan(), execution = original.shots[0].execution!;
    execution.contacts = [{ version: 1, policy: 'whole-skin-separating-plane-v1', sourceId: 'a', targetId: 'b', start: 0, end: 4, normal: [-1, 0, 0], binding: { version: 1, arm: { shoulder: 'upper', elbow: 'lower', wrist: 'hand' }, targetBone: 'hand', sourceAnchor: [0, 0, 0], targetAnchor: [0, 0, 0], rotation: [0, 0, 0, 1], pole: [0, 0, 1] } }];
    expect(correctExecutionBeatClip(original, patch(), targets()).shots[0].execution!.contacts).toEqual(execution.contacts);
    execution.actors![1].beats.forEach(b => { b.from.x = .7; b.to.x = .7; });
    expect(() => correctExecutionBeatClip(original, patch(), targets())).toThrow('间距');
  });
  it('bounds correction history without dropping earlier records', () => {
    const original = plan(); original.diagnostics.push(...Array(128).fill(JSON.stringify({ kind: 'manual-performance-correction', version: 1 })));
    expect(() => correctExecutionBeatClip(original, patch(), targets())).toThrow('纠正记录');
    expect(original.diagnostics).toHaveLength(129);
  });
  it('splits one existing beat into two explicitly chosen clips without changing root trajectory, source or other tracks', () => {
    const original = plan(); const beat = original.shots[0].execution!.actors![0].beats[1]; beat.to = { x: .6, z: .4 };
    const before = structuredClone(original);
    const result = correctExecutionBeatClip(original, { ...patch(), animation: clips[0], split: { at: 3, afterAnimation: clips[1] } }, targets());
    const actor = result.shots[0].execution!.actors![0];
    expect(actor.beats).toHaveLength(3);
    expect(actor.beats.slice(1)).toEqual([
      { ...beat, end: 3, to: { x: .3, z: .2 }, animation: clips[0] },
      { ...beat, start: 3, from: { x: .3, z: .2 }, animation: clips[1] },
    ]);
    const expected = structuredClone(before); expected.shots[0].execution!.actors![0].beats = actor.beats;
    expect({ ...result, diagnostics: before.diagnostics }).toEqual(expected); expect(original).toEqual(before);
    expect(JSON.parse(result.diagnostics.at(-1)!)).toMatchObject({ kind: 'manual-performance-correction', operation: 'split-beat', splitAt: 3, previousBeat: beat, replacementBeats: actor.beats.slice(1), sourceIndex: 1 });
  });
  it.each([NaN, Infinity, 2, 4, 1, 5, 2 + 1e-7, 4 - 1e-7])('rejects invalid split time %s atomically', at => {
    const original = plan(), before = structuredClone(original);
    expect(() => correctExecutionBeatClip(original, { ...patch(), split: { at, afterAnimation: clips[0] } }, targets())).toThrow('分界');
    expect(original).toEqual(before);
  });
  it('revalidates both selected split clips, preserves absent source indices, and enforces the existing beat budget', () => {
    const original = plan(); delete original.shots[0].execution!.actors![0].beats[1].sourceIndex;
    expect(() => correctExecutionBeatClip(original, { ...patch(), split: { at: 3, afterAnimation: { ...clips[0], name: 'stale' } } }, targets())).toThrow('资源已变化');
    const split = correctExecutionBeatClip(original, { ...patch(), split: { at: 3, afterAnimation: clips[0] } }, targets());
    expect(split.shots[0].execution!.actors![0].beats.slice(1).every(b => b.sourceIndex === undefined)).toBe(true);
    const crowded = plan(); const actors = crowded.shots[0].execution!.actors!;
    actors[0].beats = Array.from({ length: 126 }, (_, i) => ({ ...actors[0].beats[0], start: i / 32, end: (i + 1) / 32 }));
    expect(() => correctExecutionBeatClip(crowded, { ...patch(), beatIndex: 0, split: { at: .01, afterAnimation: clips[0] } }, targets())).toThrow();
    expect(actors[0].beats).toHaveLength(126);
  });
  it('produces an actual local bone-pose change in the shared sampler, not a camera/position proxy, and restores it', () => {
    const entries = ['a', 'b'].map((id, j) => {
      const object = new THREE.Group(); object.position.x = j * 3;
      const bone = new THREE.Bone(); bone.name = 'arm'; object.add(bone);
      object.animations = clips.map((clip, i) => new THREE.AnimationClip(clip.name, 2, [new THREE.QuaternionKeyframeTrack('arm.quaternion', [0, 2], [0, 0, 0, 1, 0, 0, Math.sin(i * Math.PI / 4), Math.cos(i * Math.PI / 4)])]));
      return { id, name: id, object };
    });
    const camera = new THREE.PerspectiveCamera(); const controls = { target: new THREE.Vector3(), enabled: true, update: vi.fn() };
    const baseline = entries.map(e => e.object.position.toArray());
    const sample = (value: ScriptShotPlan) => {
      const sampler = createScriptShotSampler(value, { camera, controls, entries: () => entries, environment: null });
      try { sampler.sampleAt(3); return entries.map(e => e.object.getObjectByName('arm')!.quaternion.toArray()); }
      finally { sampler.stop(); }
    };
    const original = plan(), corrected = correctExecutionBeatClip(original, patch(), targets());
    const before = sample(original), after = sample(corrected);
    expect(after[0]).not.toEqual(before[0]); expect(after[0][2]).toBeCloseTo(Math.sin(Math.PI / 8)); expect(after[1]).toEqual(before[1]);
    expect(entries.map(e => e.object.position.toArray())).toEqual(baseline);
    expect(entries[0].object.getObjectByName('arm')!.quaternion.toArray()).toEqual([0, 0, 0, 1]);
    console.log('MANUAL_PERFORMANCE_POSE=PASS changedBone=arm unchangedOtherActor=true stop=restored');
  });
});
