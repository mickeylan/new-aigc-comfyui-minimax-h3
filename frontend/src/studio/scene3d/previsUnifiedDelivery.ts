import { validateScriptShotPlan, type ScriptShotPlan } from './scriptShotPlan';
import type { ScriptVideoArtifact } from './ScriptVideoPanel';
import { buildPrevisVideoDelivery } from './previsVideoDelivery';
import { createStoreZip } from './zip.js';
export type ReferenceDeliveryArtifact = { blob: Blob; manifest: Record<string, any> };
/** Nests the two existing delivery formats, preserving their distinct EDL source semantics. */
export async function buildUnifiedPrevisDelivery(plan: ScriptShotPlan, video: ScriptVideoArtifact, reference: ReferenceDeliveryArtifact, options: { signal: AbortSignal; assertCurrent(): void }) {
  const check = () => { if (options.signal.aborted) throw Error('总交付已取消'); options.assertCurrent(); }; check();
  const ready = validateScriptShotPlan(plan), r = reference.manifest, v = video.manifest;
  if (r.kind !== 'script-shot-reference-pack' || r.version !== 1 || r.sceneId !== ready.sceneId || r.sourceFingerprint !== ready.sourceFingerprint || r.sceneFingerprint !== ready.sceneFingerprint || typeof v.sourceFingerprint !== 'string' || !v.sourceFingerprint || r.exportedSceneFingerprint !== v.sourceFingerprint || JSON.stringify(validateScriptShotPlan(r.motionPlan)) !== JSON.stringify(ready) || JSON.stringify(r.sessionEvents ?? []) !== JSON.stringify(v.sessionEvents ?? [])) throw Error('参考子包与视频场景、计划或事件来源不一致');
  if (!reference.blob.size || reference.blob.type !== 'application/zip' || reference.blob.size + video.blob.size > 256 * 1024 * 1024) throw Error('总交付子包大小/格式无效或超过256MiB，请分包下载');
  const videoPack = await buildPrevisVideoDelivery(ready, video, options); check();
  if (reference.blob.size + videoPack.size > 256 * 1024 * 1024) throw Error('总交付超过256MiB，请分包下载');
  const references = new Uint8Array(await reference.blob.arrayBuffer()); check();
  const videoBytes = new Uint8Array(await videoPack.arrayBuffer()); check();
  const manifest = { version: 1, kind: 'previs-unified-delivery', audio: false, visuallyApproved: false, fps: 24, artifactId: v.artifactId, sourceFingerprint: v.sourceFingerprint, sceneId: ready.sceneId, shots: ready, sessionEvents: v.sessionEvents ?? [],
    children: [{ file: 'reference-pack.zip', media: 'png', edlSource: 'independent-still-zero-in' }, { file: 'video-delivery.zip', media: 'webm', edlSource: 'full-video-absolute-cuts' }],
    notes: ['解压总包后再解压两个子包；各自保留镜头表、清单及正确EDL文件映射。', '无音频，不合成或改写对白；不代表视觉批准；未上传或提交生成。'] };
  const zip = createStoreZip([{ name: 'reference-pack.zip', data: references }, { name: 'video-delivery.zip', data: videoBytes }, { name: 'delivery.json', data: new TextEncoder().encode(JSON.stringify(manifest, null, 2)) }]); check();
  if (zip.byteLength > 256 * 1024 * 1024) throw Error('总交付ZIP超过256MiB，请分包下载');
  return new Blob([zip], { type: 'application/zip' });
}
