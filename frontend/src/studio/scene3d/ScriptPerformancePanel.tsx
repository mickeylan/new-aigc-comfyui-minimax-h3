import { useState } from 'react';
import { buildScriptPerformancePlan, type PerformanceCast, type PerformanceSettings, type ScriptPerformanceContext } from './scriptPerformance';
import type { ScriptScene, ScriptSceneDraft, ScriptCharacterPlacement } from './toonflowSets';
import type { ScriptShotPlan, ScriptShotMotionAnimation } from './scriptShotPlan';

type Choices = Record<string, Partial<Record<'idle' | 'talk' | 'walk', ScriptShotMotionAnimation>>>;
const defaults: PerformanceSettings = { speed: 1.35, approachDistance: 4, departureDistance: 4 };
const labels = { idle: '待机', talk: '对白', walk: '行走' } as const;

/** Render persisted audit text defensively, including after archive import. */
export function ScriptPerformanceDiagnostic({ text }: { text: string }) {
  let record: any;
  try { record = JSON.parse(text); } catch { /* ordinary diagnostic text */ }
  const validLine = (item: any) => item && Number.isSafeInteger(item.line) && typeof item.quote === 'string';
  if (record?.kind !== 'script-performance-proposal' || record.version !== 1 || !Array.isArray(record.events) || !record.events.every(validLine) || !Array.isArray(record.unresolved) || !record.unresolved.every((item: any) => validLine(item) && typeof item.reason === 'string')) return <p role="note" className="break-words text-amber-400">{text}</p>;
  return <div aria-label="表演原文覆盖审阅" className="space-y-1 break-words text-amber-400">
    <p>{record.unresolved.length} 行动作未覆盖；仅覆盖明确对白与局部走位，不代表完整剧情表演。</p>
    {record.unresolved.map((item: any, i: number) => <p key={`unresolved-${i}`} role="note">未覆盖 · 原文第 {item.line} 行：{item.quote} — {item.reason}</p>)}
    <details><summary>逐行编排来源（{record.events.length} 行）</summary>
      {record.events.map((item: any, i: number) => <p key={i}>原文第 {item.line} 行 · {item.kind === 'dialogue' ? '对白' : item.kind === 'move' ? '走位' : '未覆盖'}{typeof item.shotId === 'string' ? ` → ${item.shotId}` : ''}：{item.quote}</p>)}
    </details>
    <details><summary>编排原始记录</summary><pre className="whitespace-pre-wrap break-all">{text}</pre></details>
  </div>;
}

/** Separate review after static cast insertion. It never inserts a second actor,
 * matches by confirmed IDs only, and never chooses a clip by filename or index zero. */
export default function ScriptPerformancePanel({ draft, scene, source, captureContext, buildBasePlan, disabled, onDirty, onStage }: {
  draft: ScriptSceneDraft; scene: ScriptScene; source: string;
  captureContext?: () => ScriptPerformanceContext;
  buildBasePlan: () => ScriptShotPlan;
  disabled: boolean;
  onDirty: (dirty: boolean) => void;
  onStage: (plan: ScriptShotPlan, revision: string) => void;
}) {
  const [context, setContext] = useState<ScriptPerformanceContext | null>(null);
  const [choices, setChoices] = useState<Choices>({});
  const [settings, setSettings] = useState<PerformanceSettings>({ ...defaults });
  const [saved, setSaved] = useState<{ choices: Choices; settings: PerformanceSettings } | null>(null);
  const [error, setError] = useState('');
  const placements: ScriptCharacterPlacement[] = draft.review.bindings ?? [];
  function read() {
    if (disabled) return;
    onDirty(true); setContext(null); setChoices({}); setError('');
    try {
      if (!captureContext) throw Error('当前场景没有接入同步演员快照');
      const next = captureContext();
      if (!placements.length || placements.length !== draft.review.characters.length) throw Error('缺少已确认的角色绑定，请重新追加该场演员');
      if (placements.some(p => !next.targets.some(t => t.id === p.objectId && t.visible && !t.locked && t.animations.length))) throw Error('已确认演员缺失、隐藏、锁定或没有骨骼片段；不会按名字寻找替代演员');
      setContext(next);
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  }
  function stage() {
    if (disabled || !context || !captureContext) return;
    try {
      if (captureContext().revision !== context.revision) throw Error('场景或演员已变化，请重新读取已确认演员');
      const cast: PerformanceCast[] = placements.map(p => {
        const selected = choices[p.character];
        if (!selected?.idle || !selected.talk || !selected.walk) throw Error(`请为「${p.character}」显式选择三类骨骼片段`);
        return { character: p.character, targetId: p.objectId, idle: selected.idle, talk: selected.talk, walk: selected.walk };
      });
      const plan = buildScriptPerformancePlan(buildBasePlan(), scene, source, cast, context, settings);
      onStage(plan, context.revision); onDirty(false); setSaved(structuredClone({ choices, settings })); setError('');
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  }
  const complete = !!context && placements.length > 0 && placements.every(p => choices[p.character]?.idle && choices[p.character]?.talk && choices[p.character]?.walk);
  return <section aria-label="剧本对白与走位编排" className="space-y-1 border-t border-zinc-700 pt-2">
    <h3>剧本对白与走位编排</h3>
    <p>已追加的是静态演员，表演尚未应用。按已确认的对象 ID 读取实际站位；移动演员后须重新读取。表演预检失败不会撤销之前已确认的静态布景。</p>
    <p className="text-amber-400">只识别整行动作「角色走到另一角色面前」「角色走近另一角色」「角色离开」。速度、接近距离、离开距离属于人工布景推定，不是原文事实；不自动绕障碍，不支持抓腕、扑胸、涉水、口型或微表情。通用交谈片段不等于剧情正确。</p>
    <p>「面前」取对方当前朝向的前方；「走近」沿两人连线缩短距离；「离开」沿当前朝向的反方向直走，不解析门口或出口。路径不安全就拒绝，须在视口调整站位或朝向后重新读取。</p>
    <button type="button" disabled={disabled || !captureContext} onClick={read}>读取已确认演员</button>
    {context && placements.map(p => {
      const target = context.targets.find(t => t.id === p.objectId)!;
      return <div key={p.character} className="space-y-1 border-t border-zinc-800 pt-1">
        <p>{p.character} → {target.name} · {target.id} · 当前世界站位 {target.position.x.toFixed(2)},{target.position.z.toFixed(2)}</p>
        {(Object.keys(labels) as (keyof typeof labels)[]).map(key => <label key={key} className="block">{labels[key]}片段
          <select aria-label={`${p.character} ${labels[key]}片段`} disabled={disabled} value={choices[p.character]?.[key]?.index ?? ''} onChange={event => {
            const clip = event.target.value === '' ? undefined : target.animations.find(c => c.index === Number(event.target.value));
            setChoices(current => ({ ...current, [p.character]: { ...current[p.character], [key]: clip ? { ...clip } : undefined } })); onDirty(true);
          }} className="w-full bg-zinc-800 p-1">
            <option value="">未选择（不按名称猜动作）</option>
            {target.animations.map(clip => <option key={clip.index} value={clip.index}>{clip.name} · {clip.duration.toFixed(2)} 秒</option>)}
          </select>
        </label>)}
      </div>;
    })}
    <label className="block">走位速度（米/秒）<input aria-label="表演走位速度" type="number" step="0.1" min="0.2" max="3" value={settings.speed} disabled={disabled} onChange={e => { setSettings(s => ({ ...s, speed: e.target.valueAsNumber })); onDirty(true); }} className="w-20 bg-zinc-800" /></label>
    <label className="block">接近后间距（米）<input aria-label="表演接近距离" type="number" step="0.1" min="1" max="10" value={settings.approachDistance} disabled={disabled} onChange={e => { setSettings(s => ({ ...s, approachDistance: e.target.valueAsNumber })); onDirty(true); }} className="w-20 bg-zinc-800" /></label>
    <label className="block">离开距离（米）<input aria-label="表演离开距离" type="number" step="0.1" min="0.2" max="20" value={settings.departureDistance} disabled={disabled} onChange={e => { setSettings(s => ({ ...s, departureDistance: e.target.valueAsNumber })); onDirty(true); }} className="w-20 bg-zinc-800" /></label>
    <p>草稿按原文顺序排列对白与支持的动作，走位单独占镜；未覆盖动作保留缺项。片段重复时各从零播放，不保证边界姿态连续；生成会替换本面板未应用的镜头草稿，已有场景镜头表仍须另行确认才替换。</p>
    <button type="button" disabled={disabled || !complete} onClick={stage}>生成对白与走位表演草稿</button>
    <button type="button" disabled={disabled} onClick={() => { setChoices(saved?.choices ?? {}); setSettings(saved?.settings ?? { ...defaults }); setError(''); onDirty(false); }}>放弃表演参数修改</button>
    {error && <p role="alert">{error}</p>}
  </section>;
}
