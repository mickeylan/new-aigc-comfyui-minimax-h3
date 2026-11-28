import { useState } from 'react';
import { buildRevisionProposals, type ObservationPoint, type RevisionProposal } from './previsRevision';
import type { ScriptShotPlan } from './scriptShotPlan';

/** Read-only list of deterministic framing proposals derived from an observation report.
 * Nothing is applied here: each row must be applied explicitly, and applying never re-observes
 * or marks the previs visually approved. */
export default function PrevisRevisionPanel({ report, plan, disabled, onApply }: {
  report: { observations: readonly ObservationPoint[] } | null; plan: ScriptShotPlan | null; disabled: boolean;
  onApply: (proposal: RevisionProposal) => void;
}) {
  const [dismissed, setDismissed] = useState<string[]>([]);
  const [error, setError] = useState('');
  if (!report || !plan) return null;
  const proposals = buildRevisionProposals(plan, report).filter(proposal => !dismissed.includes(proposal.id));
  return <section aria-label="观察修订提案" className="space-y-1 rounded border border-zinc-700 p-2 text-xs">
    <h4>观察修订提案（只做水平对准，需逐条确认）</h4>
    <p>从本次观察报告的机位与角色位置计算；不读像素、不调用模型、不自动应用。应用只改该镜机位目标，应用后必须重新观察，仍不构成视觉通过。</p>
    {error && <p role="alert">{error}</p>}
    {!proposals.length ? <p role="status">本次报告没有可计算的取景越界提案。画面内不代表表演、遮挡或剧情已通过。</p> : proposals.map(proposal => <div key={proposal.id} className="space-y-1 border-t border-zinc-800 pt-1">
      <p>{proposal.shotId} · 全片{proposal.evidence.seconds.toFixed(3)}秒 · {proposal.evidence.targetId} · 越界NDC x={proposal.evidence.ndc[0]} y={proposal.evidence.ndc[1]} · 依据{proposal.frameFile}</p>
      <p>现目标 {proposal.previousTarget.join(',')} → 建议 {proposal.target.join(',')}；对准后预测NDC x={proposal.predictedNdc[0]} y={proposal.predictedNdc[1]}</p>
      <p>{proposal.note}</p>
      <div className="flex flex-wrap gap-2">
        <button type="button" disabled={disabled} aria-label={`应用修订提案 ${proposal.id}`} onClick={() => {
          try { onApply(proposal); setError(''); } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
        }}>应用（改机位目标）</button>
        <button type="button" disabled={disabled} aria-label={`忽略修订提案 ${proposal.id}`} onClick={() => setDismissed(ids => [...ids, proposal.id])}>忽略</button>
      </div>
    </div>)}
  </section>;
}