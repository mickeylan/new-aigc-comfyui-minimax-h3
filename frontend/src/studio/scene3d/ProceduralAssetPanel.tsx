import { useEffect, useRef, useState } from 'react';
import type { Object3D } from 'three';
import type { ProceduralAssetRow } from './toonflowAssets';
type Engine = typeof import('./toonflowAssets');

export default function ProceduralAssetPanel({ disabled, onAdd }: { disabled: boolean; onAdd: (object: Object3D, name: string) => void }) {
  const [engine, setEngine] = useState<Engine | null>(null);
  const [query, setQuery] = useState('');
  const [page, setPage] = useState(0);
  const [error, setError] = useState('');
  const [parent, setParent] = useState<ProceduralAssetRow | null>(null);
  const [pending, setPending] = useState(false);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    let active = true;
    import('./toonflowAssets').then(value => { if (active) setEngine(value); }).catch(cause => { if (active) setError(String(cause)); });
    return () => { active = false; mounted.current = false; };
  }, []);
  const result = engine?.searchProceduralAssets(query, page);
  const counts = engine?.proceduralAssetCounts();
  const variants = engine && parent ? engine.proceduralAssetVariants(parent.id) : [];
  function add(row: ProceduralAssetRow, parentId?: string) {
    if (!engine || disabled || pending) return;
    setError(''); setPending(true);
    try {
      const object = engine.createProceduralAsset(row.id, parentId);
      onAdd(object, row.name);
    } catch (cause) { if (mounted.current) setError(cause instanceof Error ? cause.message : String(cause)); }
    finally { if (mounted.current) setPending(false); }
  }
  return <section aria-label="程序化素材库" className="space-y-2 p-2 text-xs">
    <h3 className="font-semibold">程序化素材 · Toonflow</h3>
    <p className="text-zinc-400">{counts ? `${counts.main} 主素材 + ${counts.derivatives} 衍生 = ${counts.total} 项` : '正在加载本地素材目录…'}</p>
    <p className="text-zinc-500">不联网生成；添加后可编辑。整套布景见下方；大场景配方存档与规模优化尚未验收。</p>
    <input aria-label="搜索程序化素材" value={query} disabled={!engine || disabled || pending} placeholder="名称、古建、家具、植物…" onChange={event => { setQuery(event.target.value); setPage(0); setParent(null); }} className="w-full rounded bg-zinc-800 p-2" />
    {error && <p role="alert" className="text-red-400">{error}</p>}
    {parent ? <>
      <button type="button" onClick={() => setParent(null)}>← 返回主素材</button>
      <p>{parent.name} · {variants.length} 个衍生</p>
      <div className="max-h-72 overflow-auto space-y-1">{variants.map(row => <button type="button" key={row.id} disabled={disabled || pending} onClick={() => add(row, parent.id)} className="block w-full rounded bg-zinc-800 p-2 text-left">添加 · {row.name}</button>)}</div>
    </> : <>
      <p>匹配 {result?.total ?? 0} 个主素材</p>
      <div className="max-h-72 overflow-auto space-y-1">{result?.rows.map(row => <div key={row.id} className="flex gap-1 rounded bg-zinc-800 p-1">
        <button type="button" disabled={disabled || pending} onClick={() => add(row)} className="min-w-0 flex-1 p-1 text-left" title={`${row.theme} · ${row.category}`}>添加 · {row.name}</button>
        {!!row.derivatives && <button type="button" disabled={disabled || pending} onClick={() => setParent(row)} className="shrink-0 p-1">衍生 {row.derivatives}</button>}
      </div>)}</div>
      <div className="flex items-center justify-between">
        <button type="button" disabled={!page || disabled || pending} onClick={() => setPage(value => value - 1)}>上一页</button>
        <span>{page + 1} / {Math.max(1, Math.ceil((result?.total ?? 0) / 24))}</span>
        <button type="button" disabled={!result || (page + 1) * 24 >= result.total || disabled || pending} onClick={() => setPage(value => value + 1)}>下一页</button>
      </div>
    </>}
  </section>;
}
