import { useEffect, useRef, useState } from 'react';
import type { PosedOcclusion } from './previsPosedOcclusion';
import { coverageCandidates, type CoverageCandidate } from './previsCoverage';
import { diagnoseCoverageOcclusion, type CoverageOcclusion } from './previsOcclusion';
import type { ShotSceneEntry } from './shotNavigation';
import type { ScriptShotPlan, ScriptShotCamera } from './scriptShotPlan';
export default function CoveragePanel({ disabled, plan, capture, apply, diagnosePosed }: {
  disabled: boolean; plan: ScriptShotPlan | null;
  capture(): { revision: string; entries: readonly ShotSceneEntry[]; aspect: number };
  apply(shotId: string, camera: ScriptShotCamera, revision: string, plan: ScriptShotPlan): void;
  diagnosePosed?(shotId: string, candidates: CoverageCandidate[], revision: string, plan: ScriptShotPlan): Promise<PosedOcclusion[]>;
}) {
  const [shotId, setShotId] = useState(''), [error, setError] = useState('');
  const [posed, setPosed] = useState<PosedOcclusion[] | null>(null), [pending, setPending] = useState(false);
  const generation = useRef(0);
  useEffect(() => { generation.current++; setPosed(null); setPending(false); return () => { generation.current++; }; }, [plan, disabled, shotId]);
  const off = disabled || pending;
  const [occlusion, setOcclusion] = useState<CoverageOcclusion[] | null>(null);
  useEffect(() => { setOcclusion(null); }, [plan, disabled, shotId]);
  const [draft, setDraft] = useState<{ revision: string; plan: ScriptShotPlan; candidates: CoverageCandidate[] } | null>(null);
  useEffect(() => { setShotId(''); setDraft(null); setError(''); }, [plan]);
  useEffect(() => { if (disabled) setDraft(null); }, [disabled]);
  return <section aria-label="多主体候选机位" className="space-y-1 rounded border border-zinc-700 p-2">
    <h4>多主体候选机位与越轴提示</h4>
    <p>仅框住已绑定主体的编辑包围体与根位移端点；生成候选时未计算动态骨姿与地形机位安全。可另行检查离散实际骨姿主体包围体和静态网格有限射线，命中0不代表无遮挡。候选不代表视觉通过；不会改镜序或对白。</p>
    <select aria-label="候选镜头" className="w-full bg-zinc-800 text-white disabled:opacity-50" disabled={off || !plan} value={shotId} onChange={e => { setShotId(e.target.value); setDraft(null); setOcclusion(null); }}><option value="">选择已有镜头</option>{plan?.shots.map(s => <option key={s.id} value={s.id}>{s.order} · {s.id}</option>)}</select>
    <button disabled={off || !shotId} onClick={() => { try { const shot = plan?.shots.find(s => s.id === shotId); if (!shot) throw Error('镜头已变化'); const source = capture(); setOcclusion(null); setPosed(null); setDraft({ revision: source.revision, plan: plan!, candidates: coverageCandidates(shot, source.entries, source.aspect) }); setError(''); } catch (e) { setDraft(null); setError(String(e)); } }}>生成三个机位候选（不应用）</button>
    {draft && <>
      <p>静态遮挡诊断仅测非主体静态网格的双面三角形与主体编辑包围体中心/8角根端点；不测主体互挡、动态骨姿、中间轨迹、环境地形/天气/事件，不是可见像素比例。</p>
      <button disabled={off} onClick={() => {
        try {
          if (!plan || draft.plan !== plan) throw Error('计划已变化，请重新生成候选');
          const source = capture(), shot = plan.shots.find(s => s.id === shotId);
          if (!shot || source.revision !== draft.revision) throw Error('场景或计划已变化，请重新生成候选');
          const results = draft.candidates.map(c => diagnoseCoverageOcclusion(shot, c, source.entries));
          if (capture().revision !== draft.revision) throw Error('诊断期间场景已变化，请重新生成候选');
          setOcclusion(results); setError('');
        } catch (cause) { setDraft(null); setOcclusion(null); setError(String(cause)); }
      }}>检查候选静态遮挡射线（不应用）</button>
      {diagnosePosed && <><p>实际骨姿主体互挡只测起中尾三个时刻的CPU顶点包围体；不测透明度/像素或连续轨迹，0命中不是无遮挡通过。采样后还原，不采用机位。</p>
      <button disabled={off} onClick={async () => {
        const token = generation.current; setPending(true); setPosed(null);
        try {
          if (!plan || draft.plan !== plan || capture().revision !== draft.revision) throw Error('场景或计划已变化，请重新生成候选');
          const results = await diagnosePosed(shotId, draft.candidates, draft.revision, draft.plan);
          if (token !== generation.current) return;
          if (capture().revision !== draft.revision) throw Error('诊断期间场景已变化，请重新生成候选');
          setPosed(results); setError('');
        } catch (cause) { if (token === generation.current) { setDraft(null); setPosed(null); setError(String(cause)); } }
        finally { if (token === generation.current) setPending(false); }
      }}>检查候选实际骨姿主体互挡（不应用）</button></>}
    </>}
    {draft?.candidates.map((c, i) => <div key={i}>候选{i + 1} · {({ crosses: '越轴风险', 'same-side': '同侧', 'on-axis': '轴上风险', undefined: '轴线未定义' })[c.axisRisk]} · 包围体视锥：{c.frustumContainsBounds ? '入画' : '未入画'} · 连续动态/像素遮挡未验证
      {occlusion?.[i] && <p aria-label={`候选${i + 1}静态遮挡诊断`}>{occlusion[i].status === 'completed' ? `静态几何射线：${occlusion[i].blockedRays}/${occlusion[i].rays}条命中；${occlusion[i].triangles}三角形；遮挡对象：${occlusion[i].blockers.join('、') || '无命中（非无遮挡通过）'}` : `静态遮挡诊断不可用：${occlusion[i].reason}（未输出无遮挡结论）`}</p>}
      {posed?.[i] && <div aria-label={`候选${i + 1}实际骨姿主体互挡`}>{posed[i].frames.map(frame => <p key={frame.seconds}>全片{frame.seconds.toFixed(3)}秒 · 主体包围体射线：{frame.blockedRays}/{frame.rays}命中 · 阻挡主体：{frame.blockers.join('、') || '无命中（非无遮挡通过）'}</p>)}</div>}
      <button disabled={off || !c.frustumContainsBounds} onClick={() => { try { if (!plan || draft.plan !== plan || capture().revision !== draft.revision) throw Error('场景或计划已变化，请重新生成候选'); apply(shotId, c.camera, draft.revision, draft.plan); setDraft(null); setError(''); } catch (e) { setDraft(null); setError(String(e)); } }}>确认采用候选{i + 1}机位（保留动作和对白）</button>
    </div>)}
    {error && <p role="alert">{error}</p>}
  </section>;
}
