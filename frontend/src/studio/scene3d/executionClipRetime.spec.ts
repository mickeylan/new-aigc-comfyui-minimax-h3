import { describe, expect, it } from 'vitest';
import { validateScriptShotPlan, type ScriptShotPlan, type ScriptShotMotionTarget } from './scriptShotPlan';
import { correctExecutionBeatClip, retimeExecutionBeat } from './performanceCorrection';
const clips = [{ index: 0, name: 'idle', duration: 2 }, { index: 1, name: 'slash', duration: 3 }];
const targets = (): ScriptShotMotionTarget[] => [{ id: 'a', name: 'A', visible: true, locked: false, animations: structuredClone(clips) }];
const plan = (): ScriptShotPlan => ({ version: 1, sceneId: 's', heading: '', location: '', sourceFingerprint: 's', sceneFingerprint: 's', action: [], diagnostics: [], shots: [{ id: 's', order: 1, start: 0, end: 4, camera: { position: [0, 3, 5], target: [0, 0, 0], fov: 45 }, dialogue: [], execution: { version: 1, actors: [{ targetId: 'a', targetName: 'A', beats: [{ start: 0, end: 1.5, from: { x: 0, z: 0 }, to: { x: 3, z: 0 }, yaw: 1, animation: clips[0] }] }] } }] });
const patch = { shotId: 's', targetId: 'a', beatIndex: 0, timing: { sourceIn: .5, rate: 1 }, reason: '只取后半段' };
const audit = (value: ScriptShotPlan) => value.diagnostics.map(text => JSON.parse(text));
describe('authored clip source window and rate', () => {
  it('retimes only the selected beat, keeps trajectory and records an audit entry', () => {
    const original = plan(), before = structuredClone(original), result = retimeExecutionBeat(original, patch, targets());
    expect(original).toEqual(before);
    expect(result.shots[0].execution!.actors![0].beats[0]).toMatchObject({ start: 0, end: 1.5, from: { x: 0, z: 0 }, to: { x: 3, z: 0 }, yaw: 1, timing: { sourceIn: .5, rate: 1 } });
    expect(result.shots[0].camera).toEqual(original.shots[0].camera);
    expect(audit(result)[0]).toMatchObject({ kind: 'manual-performance-correction', operation: 'retime-beat', previousTiming: null, timing: { sourceIn: .5, rate: 1 }, reason: '只取后半段', start: 0, end: 1.5 });
    expect(validateScriptShotPlan(result)).toEqual(result);
  });
  it('clears the window back to legacy playback through an explicit empty timing', () => {
    const timed = retimeExecutionBeat(plan(), patch, targets());
    const cleared = retimeExecutionBeat(timed, { ...patch, timing: {}, reason: '恢复实时播放' }, targets());
    expect(cleared.shots[0].execution!.actors![0].beats[0].timing).toBeUndefined();
    expect(audit(cleared)[1]).toMatchObject({ operation: 'retime-beat', previousTiming: { sourceIn: .5, rate: 1 }, timing: {} });
  });
  it('rejects a window that overflows the clip and never mutates the input', () => {
    const original = plan(), before = structuredClone(original);
    expect(() => retimeExecutionBeat(original, { ...patch, timing: { rate: 4 } }, targets())).toThrow('超出 2 秒');
    expect(original).toEqual(before);
  });
  it('rejects out-of-range values, unknown keys, a frozen start and a real-time no-op', () => {
    for (const timing of [{ rate: .1 }, { rate: 4.5 }, { sourceIn: -1 }, { sourceIn: 2 }, { sourceIn: 0, sprint: 1 }] as never[])
      expect(() => retimeExecutionBeat(plan(), { ...patch, timing }, targets())).toThrow();
    expect(() => retimeExecutionBeat(plan(), { ...patch, timing: { sourceIn: 0, rate: 1 } }, targets())).toThrow('请调整');
    expect(() => retimeExecutionBeat(plan(), { ...patch, reason: '  ' }, targets())).toThrow('人工纠正理由');
  });
  it('rejects a changed clip resource without touching the plan', () => {
    const original = plan(), stale = targets(); stale[0].animations[0] = { ...clips[0], duration: 2.5 };
    expect(() => retimeExecutionBeat(original, patch, stale)).toThrow();
    expect(original.shots[0].execution!.actors![0].beats[0].timing).toBeUndefined();
  });
  it('refuses replacement, splitting and boundary moves on a retimed beat', () => {
    const timed = retimeExecutionBeat(plan(), patch, targets());
    const base = { shotId: 's', targetId: 'a', beatIndex: 0, reason: '替换', animation: clips[1] };
    expect(() => correctExecutionBeatClip(timed, base, targets())).toThrow('带源区间或速率');
    expect(() => correctExecutionBeatClip(timed, { ...base, split: { at: .8, afterAnimation: clips[1] } }, targets())).toThrow('带源区间或速率');
    const next = structuredClone(timed); next.shots[0].execution!.actors![0].beats[0].to = { x: 0, z: 0 }; next.shots[0].end = 4;
    next.shots[0].execution!.actors![0].beats = [{ ...next.shots[0].execution!.actors![0].beats[0], to: { x: 0, z: 0 } }, { start: 1.5, end: 3, from: { x: 0, z: 0 }, to: { x: 0, z: 0 }, yaw: 1, animation: clips[0], timing: { rate: 1.5 } }];
    expect(() => correctExecutionBeatClip(validateScriptShotPlan(next), { ...base, animation: clips[0], boundary: { at: 1 } }, targets())).toThrow();
  });
  it('stops at 128 correction records instead of evicting older ones', () => {
    const full = plan();
    for (let i = 0; i < 128; i++) full.diagnostics.push(JSON.stringify({ kind: 'manual-performance-correction', version: 1 }));
    expect(() => retimeExecutionBeat(full, patch, targets())).toThrow('128');
    expect(full.diagnostics).toHaveLength(128);
  });
});