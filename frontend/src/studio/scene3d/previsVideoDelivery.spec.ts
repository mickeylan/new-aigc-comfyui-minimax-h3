import { expect, it, vi } from 'vitest';
import { buildPrevisVideoDelivery } from './previsVideoDelivery';
import { validateScriptShotPlan } from './scriptShotPlan';
import type { ScriptVideoArtifact } from './ScriptVideoPanel';
const plan = () => validateScriptShotPlan({ version: 1, sceneId: 's', heading: 'h', location: 'l', sourceFingerprint: 'src', sceneFingerprint: 'rev', diagnostics: [], action: [], shots: [0, 1].map(i => ({ id: i ? '=formula' : '<unsafe>', order: i + 1, start: i, end: i + 1, camera: { position: [0, 3, 8], target: [0, 1, 0], fov: 50 }, dialogue: [] })) });
function fixture(): ScriptVideoArtifact { return { blob: new Blob(['fake-media']), source: 'source', epoch: 1, projectId: null, manifest: { kind: 'studio-script-shot-video', fps: 24, container: 'webm', shots: plan(), frameCount: 48, sourceDurationMs: 2000, durationMs: 2000, width: 832, height: 480, artifactId: 'artifact', sourceFingerprint: 'fp' } }; }
// The store-only archive has readable UTF8 entries; this test does not claim media decode.
async function archiveText(blob: Blob) { return new TextDecoder().decode(await blob.arrayBuffer()); }
it('packages the same video with safe text tables and absolute source EDL cuts', async () => {
  const check = vi.fn(); const blob = await buildPrevisVideoDelivery(plan(), fixture(), { signal: new AbortController().signal, assertCurrent: check });
  const text = await archiveText(blob); expect(blob.type).toBe('application/zip'); expect(check).toHaveBeenCalledTimes(3);
  for (const name of ['studio-shot-previs.webm', 'studio-shot-previs.manifest.json', 'delivery.json', 'shots.html', 'shots.csv', 'timeline.edl']) expect(text).toContain(name);
  expect(text).toContain('fake-media'); expect(text).toContain('&lt;unsafe&gt;'); expect(text).toContain('"\'=formula"');
  expect(text).toContain('00:00:01:00 00:00:02:00 00:00:01:00 00:00:02:00'); expect(text).toContain('"audio": false');
});
it.each(['fps', 'plan', 'count', 'duration', 'size'])('rejects %s mismatch', async mode => {
  const a = fixture(); if (mode === 'fps') a.manifest.fps = 12;
  if (mode === 'plan') a.manifest.shots.shots[0].id = 'different';
  if (mode === 'count') a.manifest.frameCount = 47;
  if (mode === 'duration') a.manifest.durationMs = 1000;
  if (mode === 'size') a.blob = new Blob([]);
  await expect(buildPrevisVideoDelivery(plan(), a, { signal: new AbortController().signal, assertCurrent() {} })).rejects.toThrow();
});
it('checks cancellation and stale sources after async blob read', async () => {
  const a = fixture(), controller = new AbortController();
  vi.spyOn(a.blob, 'arrayBuffer').mockImplementation(async () => { controller.abort(); return new ArrayBuffer(1); });
  await expect(buildPrevisVideoDelivery(plan(), a, { signal: controller.signal, assertCurrent() {} })).rejects.toThrow('取消');
  const check = vi.fn().mockImplementationOnce(() => {}).mockImplementation(() => { throw Error('stale'); });
  await expect(buildPrevisVideoDelivery(plan(), fixture(), { signal: new AbortController().signal, assertCurrent: check })).rejects.toThrow('stale');
});
