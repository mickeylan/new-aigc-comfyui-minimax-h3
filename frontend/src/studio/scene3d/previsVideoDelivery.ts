import { validateScriptShotPlan, type ScriptShotPlan } from './scriptShotPlan';
import { deliveryTimecode } from './previsDelivery';
import { createStoreZip } from './zip.js';
import type { ScriptVideoArtifact } from './ScriptVideoPanel';
const escapeHTML = (s: string) => s.replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!);
const csvCell = (s: string) => `"${(/^\s*[=+@-]/.test(s) ? "'" + s : s).replace(/"/g, '""')}"`;
export async function buildPrevisVideoDelivery(plan: ScriptShotPlan, artifact: ScriptVideoArtifact, options: { signal: AbortSignal; assertCurrent(): void }) {
  const check = () => { if (options.signal.aborted) throw Error('交付打包已取消'); options.assertCurrent(); }; check();
  const ready = validateScriptShotPlan(plan), m = artifact.manifest, duration = ready.shots.at(-1)!.end;
  if (m.kind !== 'studio-script-shot-video' || m.fps !== 24 || m.container !== 'webm' || m.sourceDurationMs !== duration * 1000 || JSON.stringify(validateScriptShotPlan(m.shots)) !== JSON.stringify(ready)) throw Error('视频交付需要同一镜头表的24fps WebM产物，请重新导出');
  const expectedFrames = Math.ceil(Math.ceil(duration * 1000) * 24 / 1000);
  if (m.frameCount !== expectedFrames || !Number.isFinite(m.durationMs) || Math.abs(m.durationMs - expectedFrames * 1000 / 24) > 1 || !Number.isSafeInteger(m.width) || !Number.isSafeInteger(m.height) || m.width <= 0 || m.height <= 0) throw Error('视频帧数、时长或尺寸与镜头表不一致');
  if (!artifact.blob.size || artifact.blob.size > 256 * 1024 * 1024) throw Error('视频交付大小无效或超过256MiB');
  const file = 'studio-shot-previs.webm';
  const rows = ready.shots.map(s => {
    const startFrame = Math.round(s.start * 24), endFrame = Math.round(s.end * 24);
    if (endFrame <= startFrame || endFrame > expectedFrames) throw Error('镜头舍入为空或超出视频帧数');
    return { shotId: s.id, order: s.order, startFrame, endFrame, in: deliveryTimecode(startFrame), out: deliveryTimecode(endFrame), file, dialogue: s.dialogue.map(d => `${d.speaker}: ${d.line}`).join('\n') };
  });
  const header = ['镜序', '镜头ID', '源/记录入点(24fps NDF)', '源/记录出点', '文件', '对白（无音频）'];
  const cells = rows.map(r => [String(r.order), r.shotId, r.in, r.out, file, r.dialogue]);
  const table = '<!doctype html><html lang="zh"><meta charset="utf-8"><title>预演视频交付</title><h1>预演视频交付</h1><p>24fps NDF；无音频；切点取最近整数帧。不是视觉批准。PNG参考包另行导出。</p><table border="1"><tr>' + header.map(s => `<th>${escapeHTML(s)}</th>`).join('') + '</tr>' + cells.map(row => '<tr>' + row.map(s => `<td><pre>${escapeHTML(s)}</pre></td>`).join('') + '</tr>').join('') + '</table></html>';
  const edl = 'TITLE: PREVIS VIDEO\nFCM: NON-DROP FRAME\n\n' + rows.map((r, i) => `${String(i + 1).padStart(3, '0')}  AX       V     C        ${r.in} ${r.out} ${r.in} ${r.out}\n* FROM CLIP NAME: ${file}\n* 24 FPS; NO AUDIO\n`).join('\n');
  const manifest = { version: 1, kind: 'previs-video-delivery', fps: 24, dropFrame: false, audio: false, visuallyApproved: false, artifactId: m.artifactId, sourceFingerprint: m.sourceFingerprint, rows };
  const texts = { 'shots.html': table, 'shots.csv': '\ufeff' + [header, ...cells].map(row => row.map(csvCell).join(',')).join('\r\n'), 'timeline.edl': edl, 'delivery.json': JSON.stringify(manifest, null, 2), 'studio-shot-previs.manifest.json': JSON.stringify(m, null, 2) };
  const entries = Object.entries(texts).map(([name, text]) => ({ name, data: new TextEncoder().encode(text) }));
  if (entries.reduce((n, e) => n + e.data.byteLength, artifact.blob.size) > 256 * 1024 * 1024) throw Error('交付包超过256MiB');
  const data = new Uint8Array(await artifact.blob.arrayBuffer()); check(); entries.push({ name: file, data });
  const zip = createStoreZip(entries); check();
  if (zip.byteLength > 256 * 1024 * 1024) throw Error('交付ZIP超过256MiB');
  return new Blob([zip], { type: 'application/zip' });
}
