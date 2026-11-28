import { validateScriptShotPlan, type ScriptShotPlan, type ScriptReferencePack } from './scriptShotPlan';

const html = (s: string) => s.replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]!));
const csv = (s: string) => `"${(/^[\s]*[=+@-]/.test(s) ? "'" + s : s).replace(/"/g, '""')}"`;
export function deliveryTimecode(frame: number) {
  if (!Number.isSafeInteger(frame) || frame < 0 || frame >= 24 * 3600 * 24) throw Error('交付帧号超出24小时非丢帧范围');
  return [Math.floor(frame / 86400), Math.floor(frame / 1440) % 60, Math.floor(frame / 24) % 60, frame % 24].map(n => String(n).padStart(2, '0')).join(':');
}
/** Text sidecars for the existing atomic PNG ZIP. No encoding, network or scene writes. */
export function buildPrevisDelivery(plan: ScriptShotPlan, pack: ScriptReferencePack) {
  validateScriptShotPlan(plan);
  if (pack.sceneId !== plan.sceneId || pack.sourceFingerprint !== plan.sourceFingerprint || pack.sceneFingerprint !== plan.sceneFingerprint || pack.shots.length !== plan.shots.length) throw Error('交付参考清单来源不一致');
  const names = new Set<string>();
  const rows = plan.shots.map((shot, i) => {
    const reference = pack.shots[i];
    if (reference.shotId !== shot.id || reference.order !== shot.order || reference.sampleSeconds !== shot.start) throw Error('交付镜序不一致');
    const files = reference.files.map(file => {
      if (!/^shot-\d+-(clay|normal|depth)\.png$/.test(file.name) || file.name !== `shot-${String(shot.order).padStart(2, '0')}-${file.pass}.png` || names.has(file.name)) throw Error('交付文件名不安全或重复');
      names.add(file.name); return file.name;
    });
    const clay = reference.files.find(f => f.pass === 'clay');
    if (!clay || !clay.name.endsWith('-clay.png')) throw Error('EDL需要每镜独立白模PNG');
    const startFrame = Math.round(shot.start * 24), endFrame = Math.round(shot.end * 24);
    if (endFrame <= startFrame) throw Error('镜头在24fps下为空');
    return { shotId: shot.id, order: shot.order, startFrame, endFrame, in: deliveryTimecode(startFrame), out: deliveryTimecode(endFrame), still: clay.name, files, dialogue: shot.dialogue.map(l => `${l.speaker}: ${l.line}`).join('\n') };
  });
  const header = ['镜序', '镜头ID', '记录入点(24fps NDF)', '记录出点', '参考文件', '对白（只读，无音频）'];
  const cells = rows.map(r => [String(r.order), r.shotId, r.in, r.out, r.files.join('; '), r.dialogue]);
  const table = '<!doctype html><html lang="zh"><meta charset="utf-8"><title>预演镜头表</title><h1>预演镜头表</h1><p>24fps NDF；PNG静帧，不含视频或音频。结构检查不等于视觉通过。</p><table border="1"><thead><tr>' + header.map(s => `<th>${html(s)}</th>`).join('') + '</tr></thead><tbody>' + cells.map(row => '<tr>' + row.map(s => `<td><pre>${html(s)}</pre></td>`).join('') + '</tr>').join('') + '</tbody></table></html>';
  const edl = 'TITLE: PREVIS STILLS\nFCM: NON-DROP FRAME\n\n' + rows.map((r, i) => `${String(i + 1).padStart(3, '0')}  AX       V     C        00:00:00:00 ${deliveryTimecode(r.endFrame - r.startFrame)} ${r.in} ${r.out}\n* FROM CLIP NAME: ${r.still}\n* STILL IMAGE; 24 FPS; NO AUDIO\n`).join('\n');
  const manifest = { version: 1, kind: 'previs-still-delivery', fps: 24, dropFrame: false, sourceFingerprint: plan.sourceFingerprint, sceneFingerprint: plan.sceneFingerprint, sceneId: plan.sceneId, media: 'png-stills-only', visuallyApproved: false, rows };
  return Object.entries({ 'shots.html': table, 'shots.csv': '\ufeff' + [header, ...cells].map(row => row.map(csv).join(',')).join('\r\n'), 'timeline.edl': edl, 'delivery.json': JSON.stringify(manifest, null, 2) }).map(([name, text]) => ({ name, data: new TextEncoder().encode(text) }));
}
