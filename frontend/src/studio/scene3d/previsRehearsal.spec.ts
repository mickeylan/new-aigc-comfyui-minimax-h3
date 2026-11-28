import { expect, it, vi } from 'vitest';
import { observePrevis, rehearsalTimes } from './previsRehearsal';
import { validateScriptShotPlan } from './scriptShotPlan';
const plan = () => validateScriptShotPlan({ version: 1, sceneId: 's', heading: 'h', location: 'l', sourceFingerprint: 'src', sceneFingerprint: 'rev', diagnostics: [], action: [], shots: [{ id: 'a', order: 1, start: 0, end: 1, dialogue: [], camera: { position: [0, 2, 5], target: [0, 1, 0], fov: 40 } }] });
function setup() {
  let active = false, time = 0;
  const stop = vi.fn(() => { active = false; });
  const options = { sourceRevision: 'baseline', assertCurrent: vi.fn(), actors: () => [{ id: 'actor', position: [time, 0, 0] }],
    sampler: vi.fn(() => { active = true; return { walkProof: { checked: true }, contactMetrics: {}, stop, sampleAt: (seconds: number) => { time = seconds; return { camera: plan().shots[0].camera }; } } as any; }),
    capture: vi.fn(async () => { expect(active).toBe(true); await Promise.resolve(); expect(active).toBe(false); return { blob: new Blob(['png']), width: 10, height: 10 }; }) };
  return { options, stop };
}
it('samples endpoints and captures real callbacks only with restoration before await', async () => {
  const { options, stop } = setup(), source = plan(), before = structuredClone(source);
  const result = await observePrevis(source, options);
  expect(result.report).toMatchObject({ status: 'completed', visuallyApproved: false, completedSamples: 11, initial: [{ id: 'actor', position: [0, 0, 0] }], final: [{ id: 'actor', position: [1, 0, 0] }] });
  expect(result.files.map(f => f.name)).toEqual(['observation-001.png', 'observation.json']); expect(stop).toHaveBeenCalledTimes(2); expect(source).toEqual(before);
});
it('reports blocked runtime with no partial images and restores', async () => {
  const { options, stop } = setup(); options.capture.mockRejectedValueOnce(Error('renderer failed'));
  const result = await observePrevis(plan(), options);
  expect(result.report).toMatchObject({ status: 'blocked', error: 'renderer failed', visuallyApproved: false, observations: [] });
  expect(result.files.map(f => f.name)).toEqual(['observation.json']); expect(stop).toHaveBeenCalledTimes(2);
});
it('rejects stale evidence rather than publishing a blocked report as current', async () => {
  const { options, stop } = setup(); options.capture.mockImplementationOnce(async () => { options.assertCurrent.mockImplementation(() => { throw Error('stale'); }); return { blob: new Blob(['png']), width: 1, height: 1 }; });
  await expect(observePrevis(plan(), options)).rejects.toThrow('stale'); expect(stop).toHaveBeenCalledTimes(2);
});
it('captures temporal evidence with stop-before-await for every image and an offline review page', async () => {
  const { options, stop } = setup();
  const result = await observePrevis(plan(), { ...options, temporalEvidence: true });
  expect(result.report.status).toBe('completed'); expect(result.report.capturePolicy).toContain('mid-tail');
  expect(result.report.observations.map(f => f.seconds)).toEqual([0, .5, 1 - 1/24]);
  expect(result.files.map(f => f.name)).toEqual(['observation-001.png', 'observation-002.png', 'observation-003.png', 'review.html', 'observation.json']);
  expect(stop).toHaveBeenCalledTimes(4); expect(options.capture).toHaveBeenCalledTimes(3);
  expect(new TextDecoder().decode(result.files[3].data)).toContain('observation-003.png');
});
it.each(['capture', 'bytes', 'format', 'dimensions'])('discards all temporal evidence if later image fails: %s', async mode => {
  const { options, stop } = setup();
  options.capture.mockImplementationOnce(options.capture.getMockImplementation()!);
  if (mode === 'capture') options.capture.mockRejectedValueOnce(Error('second render failed'));
  if (mode === 'bytes') options.capture.mockResolvedValueOnce({ blob: { size: 128 * 1024 * 1024 + 1, type: 'image/png', arrayBuffer: vi.fn() } as any, width: 1, height: 1 });
  if (mode === 'format') options.capture.mockResolvedValueOnce({ blob: new Blob(['bad'], { type: 'text/html' }), width: 1, height: 1 });
  if (mode === 'dimensions') options.capture.mockResolvedValueOnce({ blob: new Blob(['png']), width: 0, height: 1 });
  const result = await observePrevis(plan(), { ...options, temporalEvidence: true });
  expect(result.report.status).toBe('blocked'); expect(result.report.observations).toEqual([]); expect(result.files.map(f => f.name)).toEqual(['observation.json']); expect(stop).toHaveBeenCalledTimes(3); expect(options.capture).toHaveBeenCalledTimes(2);
});
it('rejects a plan with too many temporal images before creating runtime', async () => {
  const { options } = setup(), p = plan(); p.shots = Array.from({ length: 33 }, (_, i) => ({ ...p.shots[0], id: String(i), order: i+1, start: i, end: i+1 }));
  const result = await observePrevis(p, { ...options, temporalEvidence: true }); expect(result.report.status).toBe('blocked'); expect(result.report.error).toContain('96'); expect(options.sampler).not.toHaveBeenCalled();
});
it('rejects sampling over budget before allocation', () => {
  const p = plan(); p.shots[0].end = 600;
  expect(() => rehearsalTimes(p)).toThrow('6000');
  expect(rehearsalTimes(plan())).toEqual([0, .1, .2, .3, .4, .5, .6, .7, .8, .9, 1]);
});
