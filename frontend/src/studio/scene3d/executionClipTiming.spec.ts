import { describe, expect, it } from 'vitest';
import { sampleExecutionActors, validateShotExecution, type ShotExecution } from './shotExecution';
const clip = { index: 0, name: 'idle', duration: 2 };
const execution = (timing?: { sourceIn?: number; rate?: number }): ShotExecution => ({ version: 1, actors: [{ targetId: 'a', targetName: 'A', beats: [{ start: 0, end: 1.5, from: { x: 0, z: 0 }, to: { x: 3, z: 0 }, yaw: 1, animation: clip, ...(timing ? { timing } : {}) }] }] });
describe('execution clip source window and rate', () => {
  it('keeps the legacy zero-based real-time behaviour when timing is absent', () => {
    const valid = validateShotExecution(execution(), 4);
    expect(valid.actors![0].beats[0]).toEqual({ start: 0, end: 1.5, from: { x: 0, z: 0 }, to: { x: 3, z: 0 }, yaw: 1, animation: clip });
    expect(sampleExecutionActors(valid, 1)[0].animationSeconds).toBeCloseTo(1, 9);
  });
  it('starts from the authored source second and keeps the root trajectory linear', () => {
    const valid = validateShotExecution(execution({ sourceIn: .5 }), 4), sample = sampleExecutionActors(valid, 1)[0];
    expect(sample.animationSeconds).toBeCloseTo(1.5, 9);
    expect(sample.position.x).toBeCloseTo(2, 9);
    expect(valid.actors![0].beats[0].timing).toEqual({ sourceIn: .5 });
  });
  it('scales playback with rate without speeding up the root motion', () => {
    const slow = sampleExecutionActors(validateShotExecution(execution({ rate: .5 }), 4), 1)[0];
    const fast = sampleExecutionActors(validateShotExecution(execution({ rate: 1.3 }), 4), 1)[0];
    expect(slow.animationSeconds).toBeCloseTo(.5, 9); expect(fast.animationSeconds).toBeCloseTo(1.3, 9);
    expect(slow.position).toEqual(fast.position);
  });
  it('clamps at the clip end instead of looping and never exceeds the clip duration', () => {
    const sample = sampleExecutionActors(validateShotExecution(execution({ sourceIn: .5, rate: 1 }), 4), 1.5)[0];
    expect(sample.animationSeconds).toBe(2);
  });
  it('rejects a window that does not fit, out-of-range values, unknown keys and a frozen start', () => {
    expect(() => validateShotExecution(execution({ sourceIn: .5, rate: 2 }), 4)).toThrow('执行配置无效');
    expect(() => validateShotExecution(execution({ rate: .1 }), 4)).toThrow('执行配置无效');
    expect(() => validateShotExecution(execution({ rate: 4.5 }), 4)).toThrow('执行配置无效');
    expect(() => validateShotExecution(execution({ sourceIn: -1 }), 4)).toThrow('执行配置无效');
    expect(() => validateShotExecution(execution({ sourceIn: 2 }), 4)).toThrow('执行配置无效');
    expect(() => validateShotExecution({ ...execution(), actors: [{ ...execution().actors![0], beats: [{ ...execution().actors![0].beats[0], timing: { sourceIn: 0, sprint: 1 } }] }] }, 4)).toThrow('执行配置无效');
  });
  it('accepts an exact-fit window and keeps both fields through validation', () => {
    const valid = validateShotExecution(execution({ sourceIn: .5, rate: 1 }), 4);
    expect(valid.actors![0].beats[0].timing).toEqual({ sourceIn: .5, rate: 1 });
    expect(() => validateShotExecution(execution({ sourceIn: .5, rate: 1 }), 4)).not.toThrow();
    expect(validateShotExecution(execution({ sourceIn: .5 }), 4).actors![0].beats[0].timing).toEqual({ sourceIn: .5 });
  });
});