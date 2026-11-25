import React, { useState } from 'react';
import * as THREE from 'three';
import { retargetSkeletalClip } from './characterMotion';
import { attachAnimationClip } from './animationPreview';
export interface CharacterMotionEntry { id: string; name: string; object: THREE.Object3D; locked?: boolean; hidden?: boolean; splat?: boolean; isSplat?: boolean }
export interface CharacterMotionPanelProps { entries: CharacterMotionEntry[]; disabled: boolean; onChanged: () => void }
/** Explicit calibrated-rest bake. Validation bakes temporarily but never attaches automatically. */
export function CharacterMotionPanel({ entries, disabled, onChanged }: CharacterMotionPanelProps) {
  const [sourceId,setSource] = useState(''), [targetId,setTarget] = useState(''), [index,setIndex] = useState('0');
  const [mapping,setMapping] = useState('{}'), [name,setName] = useState('Retargeted motion'), [message,setMessage] = useState('');
  const allowed = (e: CharacterMotionEntry) => !e.locked && !e.hidden && !e.splat && !e.isSplat && e.object.visible;
  const source=entries.find(e=>e.id===sourceId), target=entries.find(e=>e.id===targetId);
  const run = (attach: boolean) => {
    try {
      if (disabled || !source || !target || !allowed(source) || !allowed(target)) throw new Error('Choose editable visible rigs');
      const parsed=JSON.parse(mapping);
      if (!parsed || Array.isArray(parsed) || typeof parsed!=='object' || Object.values(parsed).some(v=>typeof v!=='string')) throw new Error('Mapping must be a JSON object of source → target bone names');
      const clip=source.object.animations[Number(index)]; if (!clip) throw new Error('Choose an existing source animation');
      const result=retargetSkeletalClip(source.object,target.object,clip,parsed,name);
      const next=attachAnimationClip(target.object,result);
      if (attach) { target.object.animations=next; onChanged(); }
      setMessage(attach?'Baked and attached':'Valid: ready to bake and attach');
    } catch(error) { setMessage(error instanceof Error?error.message:String(error)); }
  };
  return <section aria-label="Character motion" className="mt-3 min-w-0 space-y-2 border-t border-white/10 pt-2 text-[11px] [&_label]:my-1 [&_label]:block [&_input]:w-full [&_input]:bg-zinc-800 [&_select]:block [&_select]:w-full [&_select]:bg-zinc-800 [&_textarea]:min-h-24 [&_textarea]:w-full [&_textarea]:bg-zinc-800 [&_button]:my-1 [&_button]:rounded [&_button]:bg-zinc-800 [&_button]:px-2 [&_button]:py-1 [&_p]:break-words">
    <h3>Rest-calibrated retargeting</h3>
    <p>Both rigs must be in calibrated rest pose. No automatic bone matching. Grounding is geometric projection, not contact locking.</p>
    <label>Source rig<select disabled={disabled} value={sourceId} onChange={e=>{setSource(e.target.value);setIndex('0');setMessage('');}}><option value="">Choose source</option>{entries.map(e=><option key={e.id} value={e.id} disabled={!allowed(e)}>{e.name}</option>)}</select></label>
    <label>Target rig<select disabled={disabled} value={targetId} onChange={e=>{setTarget(e.target.value);setMessage('');}}><option value="">Choose target</option>{entries.map(e=><option key={e.id} value={e.id} disabled={!allowed(e)}>{e.name}</option>)}</select></label>
    <label>Source animation<select disabled={disabled} value={index} onChange={e=>{setIndex(e.target.value);setMessage('');}}>{source?.object.animations.map((c,i)=><option key={i} value={i}>{c.name} ({c.duration}s)</option>)}</select></label>
    {[['源角色实际骨骼',source],['目标角色实际骨骼',target]] .map(([label,entry]) => { const names:string[]=[]; (entry as CharacterMotionEntry | undefined)?.object.traverse(n=>{if((n as THREE.Bone).isBone)names.push(n.name)}); return <p key={label as string} aria-label={label as string}>{label as string}：{names.join(', ')||'未选择骨骼角色'}</p>; })}
    <label>Explicit bone mapping (JSON)<textarea disabled={disabled} value={mapping} onChange={e=>{setMapping(e.target.value);setMessage('');}} /></label>
    <label>New animation name<input disabled={disabled} value={name} onChange={e=>{setName(e.target.value);setMessage('');}} /></label>
    <button type="button" disabled={disabled} onClick={()=>run(false)}>Validate preview</button>
    <button type="button" disabled={disabled} onClick={()=>run(true)}>Bake and attach</button>
    <output aria-live="polite">{message}</output>
  </section>;
}
export default CharacterMotionPanel;
