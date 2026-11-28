import { expect, it } from 'vitest';
import { buildPrevisDelivery, deliveryTimecode } from './previsDelivery';
import { buildScriptShotReferencePack, validateScriptShotPlan } from './scriptShotPlan';
const fixture = () => validateScriptShotPlan({ version: 1, sceneId: 's', heading: 'h', location: 'l', sourceFingerprint: 'src', sceneFingerprint: 'rev', diagnostics: [], action: [], shots: [0, 1].map(i => ({ id: i ? '=EVIL<script>' : 'a', order: i + 1, start: i * 2, end: i * 2 + 2, dialogue: [{ index: i, speaker: '人', line: '<script>"你好"</script>', start: i * 2, end: i * 2 + 1 }], camera: { position: [0, 2, 5], target: [0, 1, 0], fov: 40 } })) });
it('builds four sidecars, continuous NDF cuts and independent still source ranges without mutation', () => {
  const plan = fixture(), pack = buildScriptShotReferencePack(plan), before = structuredClone({ plan, pack });
  const files = Object.fromEntries(buildPrevisDelivery(plan, pack).map(f => [f.name, new TextDecoder().decode(f.data)]));
  expect(Object.keys(files)).toHaveLength(4); expect(files['timeline.edl']).toContain('00:00:00:00 00:00:02:00 00:00:02:00 00:00:04:00');
  expect(files['timeline.edl']).toContain('shot-02-clay.png'); expect(files['shots.html']).not.toContain('<script>'); expect(files['shots.html']).toContain('&lt;script&gt;');
  expect(files['shots.csv']).toContain("'=EVIL<script>"); expect(files['shots.csv']).toContain('""你好""');
  expect(JSON.parse(files['delivery.json'])).toMatchObject({ visuallyApproved: false, fps: 24, media: 'png-stills-only' }); expect({ plan, pack }).toEqual(before);
});
it.each(['source', 'order', 'file', 'duplicate', 'clay'])('rejects %s mismatch before returning any files', mode => {
  const plan = fixture(), pack = buildScriptShotReferencePack(plan);
  if (mode === 'source') pack.sourceFingerprint = 'other';
  if (mode === 'order') pack.shots.reverse();
  if (mode === 'file') pack.shots[0].files[0].name = '../escape.png';
  if (mode === 'duplicate') pack.shots[1].files[0].name = pack.shots[0].files[0].name;
  if (mode === 'clay') pack.shots[0].files = pack.shots[0].files.filter(f => f.pass !== 'clay');
  expect(() => buildPrevisDelivery(plan, pack)).toThrow();
});
it('formats frame carry and rejects invalid timecode inputs', () => {
  expect(deliveryTimecode(24)).toBe('00:00:01:00'); expect(deliveryTimecode(86400)).toBe('01:00:00:00');
  for (const frame of [-1, NaN, 1.5, 24 * 3600 * 24]) expect(() => deliveryTimecode(frame)).toThrow();
});
