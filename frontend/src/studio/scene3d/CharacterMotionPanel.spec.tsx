import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { it, expect, vi } from 'vitest';
import * as THREE from 'three';
import CharacterMotionPanel from './CharacterMotionPanel';
it('requires explicit mapping and never automatically attaches; excludes protected entries',async()=>{
  (globalThis as any).IS_REACT_ACT_ENVIRONMENT=true;
  const host=document.createElement('div'); document.body.append(host); const root=createRoot(host), changed=vi.fn();
  const source=new THREE.Group(), target=new THREE.Group(); const s=new THREE.Bone(),t=new THREE.Bone(); s.name=t.name='bone'; source.add(s); target.add(t);
  source.animations=[new THREE.AnimationClip('move',1,[new THREE.VectorKeyframeTrack(`${s.uuid}.position`,[0,1],[0,0,0,1,0,0])])];
  const entries=[{id:'s',name:'Source',object:source},{id:'t',name:'Target',object:target},{id:'locked',name:'Locked',object:new THREE.Group(),locked:true}];
  try {
    await act(async()=>root.render(<CharacterMotionPanel entries={entries} disabled={false} onChanged={changed}/>));
    const selects=host.querySelectorAll('select'); expect(selects[0].querySelector('option[value="locked"]')!.hasAttribute('disabled')).toBe(true);
    const change=async(node: HTMLSelectElement,value:string)=>act(async()=>{node.value=value;node.dispatchEvent(new Event('change',{bubbles:true}));});
    await change(selects[0],'s'); await change(selects[1],'t');
    const buttons=host.querySelectorAll('button'); await act(async()=>buttons[0].click()); expect(host.querySelector('output')!.textContent).toMatch(/mapping/i); expect(target.animations).toHaveLength(0);
    const textarea=host.querySelector('textarea')!;
    await act(async()=>{Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype,'value')!.set!.call(textarea,'{"bone":"bone"}');textarea.dispatchEvent(new Event('input',{bubbles:true}));});
    await act(async()=>buttons[0].click()); expect(host.querySelector('output')!.textContent).toMatch(/Valid:/); expect(target.animations).toHaveLength(0); expect(changed).not.toHaveBeenCalled();
    await act(async()=>buttons[1].click()); expect(target.animations).toHaveLength(1); expect(changed).toHaveBeenCalledTimes(1);
    await act(async()=>root.render(<CharacterMotionPanel entries={entries} disabled={true} onChanged={changed}/>)); expect(host.querySelector('button')!.disabled).toBe(true);
  } finally { await act(async()=>root.unmount());host.remove(); }
});
