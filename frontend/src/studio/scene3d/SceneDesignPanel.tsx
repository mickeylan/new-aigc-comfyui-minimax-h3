import { useEffect, useRef, useState } from 'react';
import { requestSceneDesign, type SceneDesignHost, type SceneDesignMessage, type SceneDesignPlan, type SceneDesignSnapshot } from './sceneDesign';

type Proposal = { plan: SceneDesignPlan; snapshot: SceneDesignSnapshot };
/** Intent first. No template picker, coordinate form or mandatory manual editing in this flow. */
export default function SceneDesignPanel({ disabled, sceneKey, host, request = requestSceneDesign }: {
  disabled: boolean; sceneKey: string; host: SceneDesignHost; request?: typeof requestSceneDesign;
}) {
  const [open, setOpen] = useState(true);
  const [input, setInput] = useState('');
  const [history, setHistory] = useState<SceneDesignMessage[]>([]);
  const [proposal, setProposal] = useState<Proposal | null>(null);
  const [pending, setPending] = useState(false);
  const [status, setStatus] = useState('');
  const [error, setError] = useState('');
  const controller = useRef<AbortController | null>(null);
  const ticket = useRef(0);
  const unavailable = useRef(disabled); unavailable.current = disabled;
  const cancel = () => { ticket.current++; controller.current?.abort(); controller.current = null; setPending(false); setStatus('已停止等待，尚未应用的草稿不会写入场景；模型端请求可能仍在运行'); };
  useEffect(() => () => { ticket.current++; controller.current?.abort(); }, []);
  useEffect(() => { ticket.current++; controller.current?.abort(); controller.current = null; setPending(false); setProposal(null); setHistory([]); setStatus(''); setError(''); setInput(''); }, [sceneKey]);
  useEffect(() => { if (disabled) { ticket.current++; controller.current?.abort(); setPending(false); } }, [disabled]);

  async function send() {
    const command = input.trim();
    if (!command || pending || unavailable.current) return;
    if (new TextEncoder().encode(command).byteLength > 12000) { setError('本轮需求超过12000字节，请分轮描述'); return; }
    if (history.length > 20 || new TextEncoder().encode(history.map(message => message.content).join('')).byteLength > 32000) { setError('本会话已达到上下文预算，请新建会话；场景会保留'); return; }
    const id = ++ticket.current; const operation = new AbortController(); controller.current = operation;
    const current = () => id === ticket.current && !operation.signal.aborted && !unavailable.current;
    setPending(true); setError(''); setProposal(null); setStatus('正在读取当前场景…');
    const nextHistory: SceneDesignMessage[] = [...history, { role: 'user', content: command }];
    setHistory(nextHistory); setInput('');
    const timeout = window.setTimeout(() => { if (current()) { setError('AI建景等待超时，未应用场景'); cancel(); } }, 390000);
    try {
      const snapshot = await host.capture(); if (!current()) return;
      const result = await request(command, history, snapshot, operation.signal, text => { if (current()) setStatus(text); });
      if (!current()) return;
      const latest = await host.capture(); if (!current()) return;
      if (latest.sceneKey !== snapshot.sceneKey) throw Error('原场景已切换，本轮提案已丢弃');
      const plan = result.plan;
      const question = plan.questions.length ? `需要确认：${plan.questions.join('；')}` : `未应用草稿：${plan.summary}\n${JSON.stringify(plan)}`;
      // Retain bounded context honestly: never describe an unapplied proposal as accepted state.
      setHistory([...nextHistory, { role: 'assistant', content: new TextEncoder().encode(question).byteLength <= 12000 ? question : `未应用草稿摘要节选：${plan.summary.slice(0, 1000)}（详细对象以当前场景为准，尚未应用）` }]);
      if (plan.questions.length) setStatus('只需回答关键问题，AI会继续设计');
      else { setProposal(result); setStatus('AI已给出静态建景草稿；确认整体应用即可，无需逐项配置'); }
    } catch (cause) { if (current()) { setError(cause instanceof Error ? cause.message : String(cause)); setStatus('本轮未应用；可直接补充要求重试'); } }
    finally { window.clearTimeout(timeout); if (current()) { setPending(false); controller.current = null; } }
  }
  async function apply() {
    if (!proposal || pending || unavailable.current) return;
    const id = ++ticket.current;
    const current = () => id === ticket.current && !unavailable.current;
    setPending(true); setError(''); setStatus('正在校验并构建静态布景…');
    try {
      const result = await host.apply(proposal.plan, proposal.snapshot, current);
      if (!current()) return;
      const message = `实际应用：新增${result.added}、更新${result.updated}、删除${result.removed}；保留人工改动${result.preserved}项。未生成图片或视频，请保存场景。`;
      setStatus(message); setHistory(value => [...value, { role: 'assistant', content: message }]); setProposal(null);
    } catch (cause) { if (current()) { setError(cause instanceof Error ? cause.message : String(cause)); setStatus('应用未完成，请核对错误后重试；不要重复已成功的操作'); } }
    finally { if (current()) setPending(false); }
  }
  function newSession() { cancel(); setHistory([]); setProposal(null); setStatus('已新建会话，当前场景保留'); setError(''); setInput(''); }
  return <aside aria-label="AI对话建景" className="fixed bottom-4 right-4 sm:right-[17.5rem] z-40 w-80 max-h-[calc(100vh-6rem)] max-w-[calc(100vw-2rem)] overflow-y-auto rounded-lg border border-white/20 bg-[#15151b]/95 p-3 text-xs text-white/90 shadow-xl backdrop-blur">
    <div className="flex items-center justify-between gap-2">
      <button type="button" aria-expanded={open} onClick={() => setOpen(value => !value)}>AI设计场景 {open ? '收起' : '展开'}</button>
      {open && <button type="button" disabled={pending} onClick={newSession}>新建会话</button>}
    </div>
    {open && <div className="mt-2 space-y-2">
      <p className="text-white/60">描述你想要的场景，AI负责选景、检索素材和布局。优先对话修改，手工编辑仅作保底。</p>
      <div role="log" aria-label="建景对话记录" className="max-h-48 space-y-2 overflow-y-auto whitespace-pre-wrap break-words">
        {history.map((message, index) => <p key={index}><strong>{message.role === 'user' ? '你：' : 'AI：'}</strong>{message.role === 'assistant' && message.content.includes('\n{') ? message.content.split('\n{')[0] : message.content}</p>)}
      </div>
      {proposal && <section aria-label="AI建景草稿" className="space-y-1 rounded border border-white/15 p-2">
        <p>{proposal.plan.summary}</p>
        <p>整套景：{proposal.plan.template?.id || '不新增'} · 对象变换/新增 {proposal.plan.objects.length} · 删除 {proposal.plan.remove_ids.length}</p>
        {proposal.plan.template && <p className="text-white/60">AI整套景只带建筑与陈设，不带模板演示群演；新增人物以本轮提案为准，已有人工人物不会因此删除。</p>}
        <details><summary>用户原话与AI设计补充</summary><p>用户原话：{proposal.plan.facts.join('；') || '无摘录'}</p><p>AI设计补充：{proposal.plan.assumptions.join('；') || '无'}</p></details>
        <p className="text-amber-200">{proposal.plan.limitations.join('；')}</p>
        <p className="text-white/50">仅结构和目录引用校验，尚无AI视口视觉自检；静态白模不代表剧情表演完成。</p>
        <button type="button" disabled={disabled || pending} onClick={() => void apply()}>应用AI设计</button>
        <button type="button" disabled={pending} onClick={() => { setProposal(null); setStatus('草稿已丢弃，场景未修改'); }}>丢弃草稿</button>
      </section>}
      <textarea aria-label="描述想要的场景" placeholder="例如：古镇渡口，两个人在岸边交谈；或：把柜台移到后墙，其他不动" value={input} disabled={disabled || pending} onChange={event => setInput(event.target.value)} onKeyDown={event => { if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); void send(); } }} className="min-h-20 w-full rounded bg-white/10 p-2" />
      <div className="flex gap-3"><button type="button" disabled={disabled || pending || !input.trim()} onClick={() => void send()}>让AI设计</button>{pending && <button type="button" onClick={cancel}>停止本轮</button>}</div>
      {proposal && <p className="text-white/50">继续描述会重新设计；尚未应用的草稿不叠加到当前场景。</p>}
      {status && <p role="status">{status}</p>}{error && <p role="alert" className="text-red-300">{error}</p>}
      <p className="text-white/40">发送会调用已配置的文本模型，不触发图像/视频生成。会话仅本次页面有效。</p>
    </div>}
  </aside>;
}
