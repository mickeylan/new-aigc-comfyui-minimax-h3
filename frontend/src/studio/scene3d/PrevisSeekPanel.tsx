import { useEffect, useState } from 'react';
import type { ScriptShotPlan } from './scriptShotPlan';
/** Requested frame is not evidence: actual time is published by the host sampler. */
export default function PrevisSeekPanel({ plan, disabled, holding, onSeek, onStop }: {
  plan: ScriptShotPlan; disabled: boolean; holding: boolean;
  onSeek: (seconds: number) => void; onStop: () => void;
}) {
  const [frame, setFrame] = useState(0);
  useEffect(() => setFrame(0), [plan]);
  const total = plan.shots.at(-1)?.end ?? 0, last = Math.ceil(total * 24) - 1;
  const valid = Number.isFinite(total) && total > 0 && Number.isSafeInteger(last) && last >= 0;
  const seek = (value: number) => {
    if (disabled || !valid || !Number.isInteger(value) || value < 0 || value > last) return;
    setFrame(value); onSeek(value / 24);
  };
  return <section aria-label="已应用计划真实定位" className="space-y-1 rounded border border-cyan-700 p-2">
    <h4>视口逐帧定位（24fps）</h4>
    <p>拖动定位已应用计划，不自动播放、不改对白或动作；定位姿态临时占用视口，停止后还原。实际位置以真实播放头为准。</p>
    <input type="range" aria-label="真实视口定位帧" min={0} max={valid ? last : 0} step={1} value={valid ? Math.min(frame, last) : 0} disabled={disabled || !valid} onChange={e => seek(Number(e.target.value))} />
    <p>请求帧{frame} · 全片{(frame / 24).toFixed(3)}秒（不是采样成功证据）</p>
    <button type="button" disabled={disabled || !valid || frame <= 0} onClick={() => seek(frame - 1)}>定位上一帧</button>
    <button type="button" disabled={disabled || !valid || frame >= last} onClick={() => seek(frame + 1)}>定位下一帧</button>
    <button type="button" disabled={disabled || !valid} onClick={() => seek(frame)}>定位当前请求帧</button>
    <button type="button" disabled={disabled && !holding} onClick={onStop}>停止定位并还原</button>
  </section>;
}
