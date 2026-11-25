import { useEffect, useRef, useState } from 'react';
import type { ScriptShotPlan } from './toonflowSets';
import { FRAME_VIDEO_SIZES, type FrameAspect } from './sceneFrame';
export type ScriptVideoMode = 'viewport' | 'white' | 'normal' | 'depth';
export interface ScriptVideoArtifact { blob: Blob; manifest: Record<string, any>; source: string; epoch: number; projectId: number | null }

export default function ScriptVideoPanel({ plan, disabled, onExport, isCurrent, frameAspect = '16:9' }: {
  plan: ScriptShotPlan; disabled: boolean; frameAspect?: FrameAspect;
  onExport: (plan: ScriptShotPlan, fps: number, mode: ScriptVideoMode, signal: AbortSignal) => Promise<ScriptVideoArtifact>;
  isCurrent: (artifact: ScriptVideoArtifact) => boolean;
}) {
  const [fps, setFps] = useState(12), [mode, setMode] = useState<ScriptVideoMode>('viewport');
  const [approved, setApproved] = useState(false), [pending, setPending] = useState(false), [saving, setSaving] = useState(false);
  const [artifact, setArtifact] = useState<ScriptVideoArtifact | null>(null), [url, setUrl] = useState('');
  const [playbackError, setPlaybackError] = useState('');
  const [error, setError] = useState(''), [material, setMaterial] = useState<{ id: number; name: string } | null>(null);
  const controller = useRef<AbortController | null>(null);
  const [earlierUpload, setEarlierUpload] = useState('');
  // A fresh identity for every plan transition also handles A -> B -> A races.
  const generation = useRef({ plan, frameAspect });
  if (generation.current.plan !== plan || generation.current.frameAspect !== frameAspect) generation.current = { plan, frameAspect };
  const size = FRAME_VIDEO_SIZES[frameAspect];
  const mounted = useRef(true);
  useEffect(() => { setApproved(false); controller.current?.abort(); setArtifact(null); setMaterial(null); setError(''); }, [plan, frameAspect]);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; controller.current?.abort(); }; }, []);
  useEffect(() => {
    setPlaybackError('');
    if (!artifact) { setUrl(''); return; }
    const next = URL.createObjectURL(artifact.blob); setUrl(next);
    return () => URL.revokeObjectURL(next);
  }, [artifact]);
  const exportVideo = async () => {
    const request = new AbortController(); controller.current = request;
    const owner = generation.current;
    setPending(true); setError(''); setArtifact(null); setMaterial(null);
    try { const result = await onExport(plan, fps, mode, request.signal); if (mounted.current && owner === generation.current && !request.signal.aborted) { setArtifact(result); setApproved(false); } }
    catch (cause) { if (mounted.current && owner === generation.current) setError(cause instanceof Error ? cause.message : String(cause)); }
    finally { if (controller.current === request) { controller.current = null; if (mounted.current) setPending(false); } }
  };
  const save = async () => {
    if (!artifact || saving || material) return;
    const owner = generation.current;
    setSaving(true); setError('');
    try {
      if (!isCurrent(artifact)) throw Error('场景已变化，请重新导出后入库（旧视频仍可下载审阅）');
      const form = new FormData(); form.append('file', artifact.blob, `studio-shots-${crypto.randomUUID()}.webm`); form.append('type', 'video');
      form.append('derivation_manifest', JSON.stringify(artifact.manifest));
      if (artifact.projectId !== null) form.append('project_id', String(artifact.projectId));
      const response = await fetch('/api/materials', { method: 'POST', body: form });
      const body = await response.json().catch(() => ({}));
      if (!response.ok) throw Error(body.error || `存素材库失败（HTTP ${response.status}）`);
      if (!Number.isSafeInteger(body.id) || body.id < 1 || typeof body.name !== 'string' || !body.name) throw Error('入库响应未返回有效素材ID/名称，请先查素材库，避免重复上传');
      if (mounted.current) {
        if (owner === generation.current) setMaterial({ id: body.id, name: body.name });
        else setEarlierUpload(`先前视频已入库 #${body.id}：${body.name}；不属于当前镜头表，请勿重复上传或误关联。`);
      }
      // Upload succeeded: retain its ID and forbid duplicate submission even on an old backend.
      let retained: Record<string, unknown> | null = null;
      try { retained = typeof body.derivation_manifest === 'string' ? JSON.parse(body.derivation_manifest) : null; } catch { /* report below, never re-upload automatically */ }
      if (!retained || typeof artifact.manifest.artifactId !== 'string' || retained.artifactId !== artifact.manifest.artifactId) throw Error(`视频已入库 #${body.id}，但后端未确认保留同一来源清单；请勿重复上传。请保存本地追溯清单并升级后端后核对。`);
    } catch (cause) {
      if (mounted.current) {
        const message = cause instanceof Error ? cause.message : String(cause);
        if (owner === generation.current) setError(message);
        else setEarlierUpload(`先前视频入库结果未确认：${message}；请先查素材库，避免重复上传。不属于当前镜头表，不会自动重试。`);
      }
    }
    finally { if (mounted.current) setSaving(false); }
  };
  return <section aria-label="逐镜视频导出" className="mt-2 space-y-1 rounded border border-zinc-700 p-2 text-xs">
    <h3>逐镜视频导出与生成交接</h3>
    <p className="text-zinc-400">按已保存镜头表逐帧渲机位、走位、骨骼与天气。无音频；{frameAspect} · {size.width}×{size.height} WebM，最多2880帧/256MiB，超限整段拒绝，不截断。包含视频平面的场景暂不支持。</p>
    <label>FPS <select aria-label="逐镜视频FPS" value={fps} disabled={disabled || pending || saving} onChange={e => { setFps(Number(e.target.value)); setApproved(false); }} className="bg-zinc-800">{[1, 6, 12, 24].map(value => <option key={value} value={value}>{value}</option>)}</select></label>
    <label>画面 <select aria-label="逐镜视频画面" value={mode} disabled={disabled || pending || saving} onChange={e => { setMode(e.target.value as ScriptVideoMode); setApproved(false); }} className="bg-zinc-800">{Object.entries({ viewport: '材质原样', white: '白模', normal: '法线', depth: '深度（有损）' }).map(([id, label]) => <option key={id} value={id}>{label}</option>)}</select></label>
    <label className="flex gap-1"><input type="checkbox" aria-label="确认逐镜视频导出" checked={approved} disabled={disabled || pending || saving} onChange={e => setApproved(e.target.checked)} />已核对镜头表、机位、动作与时长，仅本地导出，不提交生成</label>
    <div className="flex gap-2"><button type="button" disabled={!approved || disabled || pending || saving} onClick={() => { void exportVideo(); }}>渲染逐镜视频</button><button type="button" disabled={!pending} onClick={() => controller.current?.abort()}>取消视频导出</button></div>
    {pending && <p role="status">正在逐帧渲染与编码；取消、失败或结束都会还原场景。</p>}
    {artifact && <div className="space-y-1">
      {url && <video controls src={url} className="w-full" onError={() => setPlaybackError('本地播放器解码失败；请下载WebM用独立播放器核验，或降低FPS重新导出。不能据此判断文件有效，不会自动重试或提交生成。')} />}
      {playbackError && <p role="alert" className="text-amber-300">{playbackError}</p>}
      <p role="status">已生成 {artifact.manifest.frameCount} 帧 · {artifact.manifest.width}×{artifact.manifest.height} · {(artifact.blob.size / 1024 / 1024).toFixed(2)} MiB；尚未提交任何生成。</p>
      <div className="flex flex-wrap gap-2"><a href={url} download="studio-shot-previs.webm">下载WebM</a><button type="button" onClick={() => {
        const u = URL.createObjectURL(new Blob([JSON.stringify(artifact.manifest, null, 2)], { type: 'application/json' }));
        const a = document.createElement('a'); a.href = u; a.download = 'studio-shot-previs.manifest.json'; a.click(); setTimeout(() => URL.revokeObjectURL(u), 10000);
      }}>下载追溯清单</button><button type="button" disabled={disabled || saving || !!material} onClick={() => { void save(); }}>{saving ? '正在入库…' : material ? '已入库' : '人工确认存视频素材库'}</button></div>
      {material && <p role="status">已入库 #{material.id}：{material.name}。进入项目 → Scene → 编辑视频提示词 →「3D结构参考视频」选择此名称，核对后保存；再由你显式生成。不会自动关联、覆盖首尾帧或提交任务。</p>}
    </div>}
    {earlierUpload && <p role="status" aria-label="先前视频入库回执" className="text-amber-300">{earlierUpload}</p>}
    {error && <p role="alert" className="text-red-300">{error}</p>}
  </section>;
}
