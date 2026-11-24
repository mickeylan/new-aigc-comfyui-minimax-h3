import { describe, expect, it } from 'vitest';
import { sequenceTiming, sequenceSampleTime } from './sequenceTiming';
import { previewFrameCount, previewFrameTime } from './previewPlayhead';
describe('endpoint inclusive sequence timing', () => {
  it('matches 24fps playhead including integer and fractional duration endpoints', () => {
    for (const seconds of [0, 0.01, 1, 1.1, 3.2]) {
      const plan = sequenceTiming(seconds, 24);
      expect(plan.frames).toBe(previewFrameCount(seconds) + 1);
      for (let index = 0; index < plan.frames; index++) expect(sequenceSampleTime(index, plan.frames, seconds, 24)).toBe(previewFrameTime(index, seconds).seconds);
      expect(sequenceSampleTime(plan.frames - 1, plan.frames, seconds, 24)).toBe(seconds);
    }
  });
  it('uses fixed fps for intermediate samples and exact duration at final sample', () => {
    const plan = sequenceTiming(1.1, 8);
    expect(plan.frames).toBe(10);
    expect(sequenceSampleTime(1, plan.frames, 1.1, 8)).toBe(0.125);
    expect(sequenceSampleTime(9, plan.frames, 1.1, 8)).toBe(1.1);
  });
  it('does not truncate over-budget durations', () => {
    expect(sequenceTiming(30, 8)).toEqual({ seconds: 30, frames: 241, exceedsLimit: true });
    expect(sequenceTiming(29, 8).exceedsLimit).toBe(false);
    expect(() => sequenceTiming(NaN, 8)).toThrow();
    expect(() => sequenceTiming(1, 0)).toThrow();
  });
});
