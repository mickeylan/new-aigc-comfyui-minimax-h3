import { useEffect, useRef, useState } from 'react';
import type { SceneTerrain } from './toonflow/sceneDocument';
import { terrainSurfaceOptions } from './toonflow/terrainSurfaces';
import { editEnvironmentTerrain, type TerrainBrush } from './environmentEdit';

export default function EnvironmentBrushPanel({ terrain, disabled, onChange }: { terrain: SceneTerrain; disabled: boolean; onChange: (next: SceneTerrain) => void }) {
  const [brush, setBrush] = useState<TerrainBrush>({ mode: 'raise', x: 0, z: 0, radius: 5, strength: 0.5, height: 0, surface: 15, endX: 10, endZ: 0, depth: 1, color: '#60abbc' });
  const [error, setError] = useState('');
  const [undoCount, setUndoCount] = useState(0);
  const history = useRef<SceneTerrain[]>([]);
  const expected = useRef(terrain);
  useEffect(() => {
    if (expected.current !== terrain) { history.current = []; setUndoCount(0); expected.current = terrain; }
  }, [terrain]);
  const apply = (resetPalette = false) => {
    try {
      const next = editEnvironmentTerrain(terrain, resetPalette ? { ...brush, mode: 'surfaceColor', color: undefined } : brush);
      history.current = [...history.current.slice(-9), terrain]; expected.current = next;
      onChange(next); setUndoCount(history.current.length); setError('');
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  };
  return <fieldset disabled={disabled} className="mt-2 min-w-0 space-y-1 border-t border-white/10 pt-2 text-[11px]">
    <legend>地形笔刷（圆形区域，参数确认后应用）</legend>
    <label>操作 <select aria-label="地形笔刷操作" value={brush.mode} onChange={e => setBrush({ ...brush, mode: e.target.value as TerrainBrush['mode'] })} className="bg-zinc-800">
      {Object.entries({ raise: '抬升', lower: '降低', flatten: '整平', smooth: '平滑', surface: '替换地表', surfacePaint: '现代材质绘图', blend: '混合地表', surfaceColor: '全局地表色', waterColor: '局部水色', river: '挖河道/湖面', drain: '抽水/恢复干地'  }).map(([id, name]) => <option key={id} value={id}>{name}</option>)}
    </select></label>
    <div className="grid grid-cols-2 gap-1">
      {(['x', 'z', 'radius', 'strength', 'height'] as const).map(key => <label key={key} className="flex flex-col">
        {{ x: '中心X', z: '中心Z', radius: '半径/米', strength: '笔刷强度', height: '整平高度/水位/米' }[key]}
        <input aria-label={`地形笔刷${key}`} type="number" step="0.1" value={brush[key]} onChange={e => setBrush({ ...brush, [key]: e.target.value === '' ? NaN : Number(e.target.value) })} className="w-full bg-zinc-800" />
      </label>)}
      {brush.mode === 'river' && (['endX', 'endZ', 'depth'] as const).map(key => <label key={key} className="flex flex-col">
        {{ endX: '终点X', endZ: '终点Z', depth: '河床深度/米' }[key]}
        <input aria-label={`地形笔刷${key}`} type="number" step="0.1" value={brush[key] ?? ''} onChange={e => setBrush({ ...brush, [key]: e.target.value === '' ? NaN : Number(e.target.value) })} className="w-full bg-zinc-800" />
      </label>)}
      <label>地表<select aria-label="地形地表" value={brush.surface} onChange={e => setBrush({ ...brush, surface: Number(e.target.value) })} className="w-full bg-zinc-800">
        {terrainSurfaceOptions.map(surface => <option key={surface.id} value={surface.id}>{surface.name}{surface.id === 2 ? '（仅颜色，不是水面）' : ''}</option>)}
      </select></label>
      {(brush.mode === 'surfaceColor' || brush.mode === 'waterColor' || brush.mode === 'surfacePaint') && <label>颜色<input aria-label="地形颜色" type="color" value={brush.color} onChange={e => setBrush({ ...brush, color: e.target.value })} /></label>}
    </div>
    {brush.mode === 'surfacePaint' && <div className="text-sky-200"><p>显式应用后启用完整59种程序材质库；整片地形外观可能改变。保留旧混合及水面；撤销首笔恢复旧渲染。原始地面38不可绘制。</p>{(['flow', 'hardness'] as const).map(key => <label key={key}>{key === 'flow' ? '流量' : '硬度'}<input aria-label={`地形绘图${key}`} type="number" min="0" max="1" step="0.05" value={brush[key] ?? (key === 'flow' ? 1 : 0.35)} onChange={e => setBrush({ ...brush, [key]: e.target.value === '' ? NaN : Number(e.target.value) })} /></label>)}</div>}
    {brush.mode === 'blend' && <p className="text-sky-200">保留基础地表，所选地表为第二层；本笔覆盖率为径向衰减×强度，不累加第三层。{!terrain.surfaceBlend && <strong className="block text-amber-200">首笔会将整片地形切换为平滑混合渲染；圆外原有地表边界和纹理外观也可能改变。撤销首笔可恢复原渲染。</strong>}</p>}
    {brush.mode === 'surfaceColor' && <p className="text-sky-200">修改整片地形中该地表的调色板，不限笔刷区域；原始地面38不可改色。<button type="button" onClick={() => apply(true)}>重置所选地表色</button></p>}
    {brush.mode === 'waterColor' && <p className="text-sky-200">仅覆盖圆内已有水面；默认水色#60abbc会清除局部覆盖，不改变河床或水位。</p>}
    <div className="flex gap-3"><button type="button" onClick={() => apply()}>应用地形笔刷</button><button type="button" disabled={!undoCount} onClick={() => {
      const previous = history.current.pop(); if (!previous) return;
      expected.current = previous; onChange(previous); setUndoCount(history.current.length); setError('');
    }}>撤销笔刷（{undoCount}）</button></div>
    {error && <p role="alert" className="text-red-300">{error}</p>}
    {brush.mode === 'river' && <p className="text-sky-200">从中心到终点挖河道；两点相同为湖面。高度是水位，深度是河床深度。水面独立渲染并存档，不支持涉水与流体模拟。</p>}
    <p className="text-white/40">最多撤销10笔；地形、混合覆盖率、地表色、现代材质绘图、水位与水色随场景保存。不会自动移动已有布景；请复核角色与走位。</p>
  </fieldset>;
}
