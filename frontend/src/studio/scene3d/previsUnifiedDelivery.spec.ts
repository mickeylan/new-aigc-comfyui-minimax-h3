import { expect, it, vi } from 'vitest';
import { buildUnifiedPrevisDelivery } from './previsUnifiedDelivery';
import { validateScriptShotPlan } from './scriptShotPlan';
import type { ScriptVideoArtifact } from './ScriptVideoPanel';
function fixture() {
  const plan = validateScriptShotPlan({ version: 1, sceneId: 's', heading: '', location: '', sourceFingerprint: 'source', sceneFingerprint: 'scene', diagnostics: [], action: [], shots: [{ id: 's', order: 1, start: 0, end: 1, camera: null, dialogue: [] }] });
  const video: ScriptVideoArtifact = { blob: new Blob(['video']), source: 'src', epoch: 1, projectId: null, manifest: { kind: 'studio-script-shot-video', fps: 24, container: 'webm', frameCount: 24, sourceDurationMs: 1000, durationMs: 1000, width: 832, height: 480, shots: plan, sourceFingerprint: 'exported', artifactId: 'artifact', sessionEvents: [] } };
  const reference = { blob: new Blob(['reference-child'], { type: 'application/zip' }), manifest: { kind: 'script-shot-reference-pack', version: 1, sceneId: 's', sourceFingerprint: 'source', sceneFingerprint: 'scene', exportedSceneFingerprint: 'exported', motionPlan: plan, sessionEvents: [] } };
  return { plan, video, reference };
}
it('nests existing complete deliveries and keeps source semantics explicit without uploading', async () => {
  const f = fixture(), check = vi.fn(); const blob = await buildUnifiedPrevisDelivery(f.plan, f.video, f.reference, { signal: new AbortController().signal, assertCurrent: check });
  const text = new TextDecoder().decode(await blob.arrayBuffer());
  for (const token of ['reference-pack.zip', 'video-delivery.zip', 'reference-child', 'studio-shot-previs.webm', '"audio": false', 'independent-still-zero-in', 'full-video-absolute-cuts', 'artifact']) expect(text).toContain(token);
  expect(check.mock.calls.length).toBeGreaterThanOrEqual(5); expect(blob.type).toBe('application/zip');
});
it.each(['fingerprint', 'plan', 'events', 'kind', 'empty'])('rejects %s reference mismatch before consuming reference bytes', async mode => {
  const f = fixture(), read = vi.spyOn(f.reference.blob, 'arrayBuffer');
  if (mode === 'fingerprint') f.reference.manifest.exportedSceneFingerprint = 'other';
  if (mode === 'plan') f.reference.manifest.motionPlan = { ...f.plan, heading: 'other' };
  if (mode === 'events') (f.reference.manifest.sessionEvents as unknown[]).push({ changed: true });
  if (mode === 'kind') f.reference.manifest.kind = 'unknown';
  if (mode === 'empty') f.reference.blob = new Blob([], { type: 'application/zip' });
  await expect(buildUnifiedPrevisDelivery(f.plan, f.video, f.reference, { signal: new AbortController().signal, assertCurrent() {} })).rejects.toThrow(); expect(read).not.toHaveBeenCalled();
});
it('cancels after reference bytes resolve without returning a partial total bundle', async () => {
  const f = fixture(), controller = new AbortController();
  vi.spyOn(f.reference.blob, 'arrayBuffer').mockImplementation(async () => { controller.abort(); return new ArrayBuffer(5); });
  await expect(buildUnifiedPrevisDelivery(f.plan, f.video, f.reference, { signal: controller.signal, assertCurrent() {} })).rejects.toThrow('取消');
});
