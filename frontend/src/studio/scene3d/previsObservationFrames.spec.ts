import { expect, it } from 'vitest';
import { observationFrames, observationReviewHTML } from './previsObservationFrames';
import { validateScriptShotPlan } from './scriptShotPlan';
const plan = () => validateScriptShotPlan({ version: 1, sceneId: 's', heading: '', location: '', sourceFingerprint: 'src', sceneFingerprint: 'rev', diagnostics: [], action: [], shots: [{ id: 'a', order: 1, start: 0, end: 2, dialogue: [], camera: { position: [0, 2, 5], target: [0, 1, 0], fov: 40 } }, { id: 'b', order: 2, start: 2, end: 4, dialogue: [], camera: { position: [1, 2, 5], target: [0, 1, 0], fov: 40 } }] });
it('keeps legacy starts and adds chronological local-time-correct samples inside each shot', () => {
  const p = plan(), before = JSON.stringify(p);
  expect(observationFrames(p).map(f => f.seconds)).toEqual([0, 2]);
  const frames = observationFrames(p, true); expect(frames.map(f => f.seconds)).toEqual([0, 1, 2 - 1/24, 2, 3, 4 - 1/24]);
  expect(frames[3]).toEqual({ shotId: 'b', seconds: 2, localSeconds: 0 }); expect(frames.at(-1)!.localSeconds).toBeLessThan(2); expect(JSON.stringify(p)).toBe(before);
});
it('includes and deduplicates beat boundaries, never maps previous end into following shot', () => {
  const p = plan(); (p.shots[0] as any).execution = { version: 1, actors: [{ targetId: 'actor', targetName: 'A', beats: [{ start: 0, end: .5, from: { x: 0, z: 0 }, to: { x: 0, z: 0 }, yaw: 0, animation: { name: 'idle', duration: 2, index: 0 } }, { start: .5, end: 2, from: { x: 0, z: 0 }, to: { x: 0, z: 0 }, yaw: 0, animation: { name: 'talk', duration: 2, index: 1 } }] }] };
  const frames = observationFrames(p, true).filter(f => f.shotId === 'a');
  expect(frames.map(f => f.seconds)).toEqual([0, .5, 1, 2 - 1/24]);
});
it('rejects screenshot budget instead of truncating a long plan', () => {
  const p = plan(); p.shots = Array.from({ length: 33 }, (_, i) => ({ ...p.shots[0], id: `s${i}`, order: i+1, start: i*2, end: (i+1)*2 }));
  expect(() => observationFrames(p, true)).toThrow('96'); expect(observationFrames(p)).toHaveLength(33);
});
it('escapes identifiers, authoritative dialogue and clip metadata with no executable script', () => {
  const p = plan(); p.shots[0].id = '<img onerror="bad">'; p.shots[0].dialogue = [{ index: 0, speaker: '<S>', line: '<script>bad</script>', start: 0, end: 1 }];
  const text = observationReviewHTML(p, [{ file: 'observation-001.png', shotId: p.shots[0].id, seconds: .5, localSeconds: .5, width: 10, height: 10, activeBeats: [{ targetId: '<id>', targetName: '<actor>', beatIndex: 0, animation: '<clip>' }] }]);
  expect(text).toContain('&lt;script&gt;bad&lt;/script&gt;'); expect(text).toContain('&lt;clip&gt;'); expect(text).not.toContain('<script>'); expect(text).toContain('visuallyApproved=false'); expect(text).toContain('default-src');
});
it('selects authoritative dialogue using absolute time for nonzero-start shots', () => {
  const p = plan(); p.shots[1].dialogue = [{ index: 0, speaker: '乙', line: '原文对白', start: 2.2, end: 2.8 }];
  const text = observationReviewHTML(p, [{ file: 'observation-001.png', shotId: 'b', seconds: 2.5, localSeconds: .5, width: 10, height: 10 }]);
  expect(text).toContain('乙：原文对白');
});
it.each(['path', 'duplicate', 'time', 'local'])('rejects invalid review file/time mapping: %s', mode => {
  const p = plan(), f = { file: 'observation-001.png', shotId: 'a', seconds: 0, localSeconds: 0, width: 10, height: 10 };
  if (mode === 'path') f.file = '../outside.png'; if (mode === 'time') f.seconds = 2; if (mode === 'local') f.localSeconds = 1;
  expect(() => observationReviewHTML(p, mode === 'duplicate' ? [f, f] : [f])).toThrow();
});
