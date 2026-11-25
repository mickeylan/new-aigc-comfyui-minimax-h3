import React, { useEffect, useState } from 'react';
import { draftShotDirection } from './shotDirection';
import { compileShotNavigation, type ShotSceneEntry } from './shotNavigation';
import { validateCharacterMotionSettings } from './characterMotion';
import { validateScriptShotPlan, type ScriptShotPlan, type ScriptShotMotionAnimation } from './toonflowSets';
import type { SceneEnvironment } from './ground-path';

type Props = { plan: ScriptShotPlan; entries: readonly ShotSceneEntry[]; environment: SceneEnvironment | null; disabled: boolean; onApply: (plan: ScriptShotPlan) => void };
const ready = (e: ShotSceneEntry) => !e.locked && !e.splat && e.object.visible;
export default function ShotEnhancementPanel({ plan, entries, environment, disabled, onApply }: Props) {
  const [mapping, setMapping] = useState<Record<string, string>>({});
  const [clips, setClips] = useState<Record<string, string>>({});
  const [framing, setFraming] = useState<'wide' | 'medium' | 'close'>('medium');
  const [azimuth, setAzimuth] = useState(0), [distance, setDistance] = useState(5), [travel, setTravel] = useState(1);
  const [avoid, setAvoid] = useState(false), [confirmed, setConfirmed] = useState(false);
  const [draft, setDraft] = useState<{ value: ScriptShotPlan; source: ScriptShotPlan; entries: readonly ShotSceneEntry[]; environment: SceneEnvironment | null } | null>(null);
  const [error, setError] = useState('');
  const [shotId, setShotId] = useState(''), [settings, setSettings] = useState('');
  const [navigation, setNavigation] = useState(false), [radius, setRadius] = useState(0.3), [speed, setSpeed] = useState(2);
  const speakers = [...new Set(plan.shots.flatMap(s => s.dialogue.map(l => l.speaker)))];
  const shot = plan.shots.find(s => s.id === shotId);
  const active = draft && draft.source === plan && draft.entries === entries && draft.environment === environment && !disabled;
  const invalidate = () => { setDraft(null); setConfirmed(false); };
  useEffect(() => { invalidate(); setError(''); }, [plan, entries, environment, disabled]);
  useEffect(() => {
    const motion = shot?.motion as (NonNullable<typeof shot>['motion'] & { characterMotion?: unknown; navigation?: { radius: number; speed: number } });
    setSettings(motion?.characterMotion ? JSON.stringify(motion.characterMotion, null, 2) : '');
    setNavigation(!!motion?.navigation); setRadius(motion?.navigation?.radius ?? 0.3); setSpeed(motion?.navigation?.speed ?? 2);
  }, [shot]);
  const attempt = (fn: () => void) => { setError(''); try { fn(); } catch (e) { setError(e instanceof Error ? e.message : String(e)); } };
  const generate = () => attempt(() => {
    invalidate();
    const animationRefs: Record<string, ScriptShotMotionAnimation> = {};
    for (const speaker of speakers) {
      const entry = entries.find(e => e.id === mapping[speaker]);
      if (!entry || !ready(entry)) throw Error(`请选择可用角色：${speaker}`);
      const selection = clips[entry.id];
      const index = selection === undefined || selection === '' ? -1 : Number(selection);
      const clip = entry.object.animations[index];
      if (!clip || clip.duration <= 0) throw Error(`请选择已有动画 clip：${entry.name}`);
      animationRefs[entry.id] = { index, name: clip.name, duration: clip.duration };
    }
    const value = draftShotDirection(plan, entries, { speakerTargets: mapping, animationRefs, framing, azimuth: azimuth * Math.PI / 180, distance, travel, avoid }, environment);
    if (avoid) compileShotNavigation(value, entries, environment);
    setDraft({ value, source: plan, entries, environment });
  });
  const enhance = () => attempt(() => {
    if (disabled || !shot?.motion) throw Error('本镜需要已有角色走位');
    const entry = entries.find(e => e.id === shot.motion!.targetId);
    if (!entry || !ready(entry) || entry.name !== shot.motion.targetName) throw Error('角色引用不可用');
    const next = structuredClone(plan);
    const motion = next.shots.find(s => s.id === shot.id)!.motion! as NonNullable<typeof shot.motion> & { characterMotion?: unknown; navigation?: unknown };
    if (settings.trim()) {
      const config = validateCharacterMotionSettings(JSON.parse(settings));
      if (config.blend) {
        const ref = config.blend.animation, clip = entry.object.animations[ref.index];
        if (!clip || clip.name !== ref.name || clip.duration !== ref.duration) throw Error('混合动画引用已失效');
      }
      if (config.feet) {
        const bones = new Map<string, any>();
        entry.object.traverse(n => { if ((n as any).isBone) { if (bones.has(n.name)) throw Error('骨骼名称重复'); bones.set(n.name, n); } });
        for (const leg of [config.feet.left, config.feet.right]) {
          const hip = bones.get(leg.hip), knee = bones.get(leg.knee), ankle = bones.get(leg.ankle);
          if (!hip || knee?.parent !== hip || ankle?.parent !== knee) throw Error('脚部骨骼链必须为已有 hip → knee → ankle');
        }
      }
      motion.characterMotion = config;
    } else delete motion.characterMotion;
    if (navigation) motion.navigation = { version: 1, mode: 'avoid', radius, speed }; else delete motion.navigation;
    validateScriptShotPlan(next);
    compileShotNavigation(next, entries, environment);
    invalidate(); onApply(next);
  });
  return <section aria-label="镜头增强" className="mt-3 min-w-0 space-y-2 border-t border-white/10 pt-2 text-[11px] [&_label]:my-1 [&_label]:block [&_input:not([type=checkbox])]:w-full [&_input]:bg-zinc-800 [&_select]:block [&_select]:w-full [&_select]:bg-zinc-800 [&_textarea]:min-h-24 [&_textarea]:w-full [&_textarea]:bg-zinc-800 [&_button]:my-1 [&_button]:rounded [&_button]:bg-zinc-800 [&_button]:px-2 [&_button]:py-1 [&_p]:break-words">
    <fieldset disabled={disabled}><legend>导演草稿（仅本地）</legend>
      <p>必须明确选择说话人、角色及已有 clip；每镜需要对白且只映射一个角色。保留已编写机位和走位。</p>
      {speakers.map(speaker => <div key={speaker}>
        <label>{speaker} 角色<select aria-label={`${speaker} 角色`} value={mapping[speaker] ?? ''} onChange={e => { invalidate(); setMapping({ ...mapping, [speaker]: e.target.value }); }}>
          <option value="">请选择角色</option>{entries.map(e => <option key={e.id} value={e.id} disabled={!ready(e)}>{e.name}</option>)}
        </select></label>
        <label>{speaker} clip<select aria-label={`${speaker} clip`} value={clips[mapping[speaker]!] ?? ''} onChange={e => { invalidate(); setClips({ ...clips, [mapping[speaker]!]: e.target.value }); }}>
          <option value="">请选择 clip</option>{entries.find(e => e.id === mapping[speaker])?.object.animations.map((c, i) => <option key={i} value={i}>{i}: {c.name} ({c.duration}s)</option>)}
        </select></label>
      </div>)}
      <label>景别<select value={framing} onChange={e => { invalidate(); setFraming(e.target.value as typeof framing); }}><option value="wide">全景</option><option value="medium">中景</option><option value="close">近景</option></select></label>
      {([['方位角（度）', azimuth, setAzimuth], ['距离', distance, setDistance], ['行程', travel, setTravel]] as const).map(([label, value, setter]) => <label key={label}>{label}<input aria-label={label} type="number" value={value} onChange={e => { invalidate(); setter(Number(e.target.value)); }} /></label>)}
      <label><input type="checkbox" checked={avoid} onChange={e => { invalidate(); setAvoid(e.target.checked); }} />草稿避障</label>
      <button type="button" onClick={generate}>生成导演草稿</button>
      {active && <div><output>草稿：{draft.value.shots.length} 镜，机位 {draft.value.shots.filter(s => s.camera).length}，目标 {new Set(draft.value.shots.flatMap(s => s.motion ? [s.motion.targetId] : [])).size}</output>
        {draft.value.shots.map(s => <p key={s.id}>{s.id}：机位 {s.camera ? JSON.stringify(s.camera) : '无'}；目标 {s.motion?.targetName ?? '无'}</p>)}
      </div>}
      <label><input aria-label="人工确认草稿" type="checkbox" disabled={!active} checked={confirmed && !!active} onChange={e => setConfirmed(e.target.checked)} />已人工检查导演草稿</label>
      <button type="button" disabled={!active || !confirmed} onClick={() => attempt(() => { if (active && confirmed) { validateScriptShotPlan(draft.value); compileShotNavigation(draft.value, entries, environment); onApply(structuredClone(draft.value)); invalidate(); } })}>确认应用导演草稿</button>
      <hr /><label>本镜<select aria-label="本镜" value={shotId} onChange={e => setShotId(e.target.value)}><option value="">请选择镜头</option>{plan.shots.map(s => <option key={s.id} value={s.id}>{s.id}</option>)}</select></label>
      <p>骨骼名称（按所选角色）：{entries.filter(e => Object.values(mapping).includes(e.id) || e.id === shot?.motion?.targetId).map(e => { const names: string[] = []; e.object.traverse(n => { if ((n as any).isBone) names.push(n.name); }); return `${e.name}: ${names.join(', ') || '无骨骼'}`; }).join('；')}</p>
      <label>角色增强 JSON<textarea aria-label="角色增强 JSON" value={settings} onChange={e => setSettings(e.target.value)} placeholder={'{"version":1,"feet":{"left":{"hip":"LHip","knee":"LKnee","ankle":"LAnkle"},"right":{"hip":"RHip","knee":"RKnee","ankle":"RAnkle"},"soleOffset":0,"maxCorrection":0.2}}'} /></label>
      <p>可选 version:1；feet 使用真实骨骼链；blend: animation 为 index/name/duration 引用，另填 weight、fadeSeconds。空白移除角色增强。</p>
      <label><input aria-label="本镜避障" type="checkbox" checked={navigation} onChange={e => setNavigation(e.target.checked)} />本镜启用避障</label>
      <label>避障半径<input aria-label="避障半径" type="number" value={radius} onChange={e => setRadius(Number(e.target.value))} /></label>
      <label>避障速度<input aria-label="避障速度" type="number" value={speed} onChange={e => setSpeed(Number(e.target.value))} /></label>
      <button type="button" disabled={!shot?.motion} onClick={enhance}>应用本镜增强</button>
    </fieldset>
    {error && <output role="alert">{error}</output>}
  </section>;
}
