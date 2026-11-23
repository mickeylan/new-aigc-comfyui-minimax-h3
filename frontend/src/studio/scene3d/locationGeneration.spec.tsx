import React, { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import LocationGenerationPanel from "./LocationGenerationPanel";
import { locationBlocker, locationReviewKey, locationJSON, type LocationDraft } from "./locationGeneration";
const template={code:'qwen_image_2_1_multi_edit',name:'Qwen',max_refs:3,reference_syntax:'<imageN>',template_sha256:'hash'};
const snapshot={blob:new Blob(['png'],{type:'image/png'}),camera:{position:[0,1,6],target:[0,1,0],fov:45}};
const draft=():LocationDraft=>({project:'1',scene:'2',shot:'',template,prompt:'保持第一张图的机位',snapshot,references:[],reviewed:true});
let root:Root|undefined, host:HTMLDivElement|undefined;
afterEach(async()=>{if(root)await act(async()=>root!.unmount());root=undefined;host?.remove();vi.unstubAllGlobals();vi.restoreAllMocks()});
const flush=()=>act(async()=>{for(let i=0;i<10;i++)await Promise.resolve()});
function element(label:string){return host!.querySelector(`[aria-label="${label}"]`) as HTMLInputElement}
async function change(label:string,value:string){await act(async()=>{const e=element(label);const proto=e.tagName==='TEXTAREA'?HTMLTextAreaElement.prototype:HTMLSelectElement.prototype;Object.getOwnPropertyDescriptor(proto,'value')!.set!.call(e,value);e.dispatchEvent(new Event(e.tagName==='TEXTAREA'?'input':'change',{bubbles:true}))});await flush()}
async function click(text:string){const b=[...host!.querySelectorAll('button')].find(b=>b.textContent===text)!;expect(b).toBeTruthy();await act(async()=>b.click());await flush()}

describe('Image to Location human-reviewed generation',()=>{
 it('blocks missing context, snapshot, prompt, capacity or review',()=>{
  expect(locationBlocker(draft())).toBe('');
  for(const patch of [{project:''},{scene:''},{snapshot:undefined},{prompt:' '},{reviewed:false},{template:undefined},{references:Array(3).fill(new File(['x'],'a.png'))}])expect(locationBlocker({...draft(),...patch})).not.toBe('');
 });
 it('invalidates review keys for context, engine/hash, prompt, references and camera',()=>{
  const d=draft(),key=locationReviewKey(d);
  for(const patch of [{project:'3'},{scene:'4'},{shot:'8'},{prompt:'new prompt'},{template:{...template,template_sha256:'changed'}},{references:[new File(['x'],'b.png')]},{snapshot:{...snapshot,camera:{...snapshot.camera,fov:60}}}])expect(locationReviewKey({...d,...patch})).not.toBe(key);
 });
 it('preserves backend error rather than claiming generated success',async()=>{
  vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify({error:'模板已变化'}),{status:400})));
  await expect(locationJSON('/fixture')).rejects.toThrow('模板已变化');
 });
 it('runs real React controls: explicit context, immutable preview, edit resets review, pending result and manual adoption',async()=>{
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT',true);
  Object.defineProperty(URL,'createObjectURL',{configurable:true,value:vi.fn(()=> 'blob:fixture')});Object.defineProperty(URL,'revokeObjectURL',{configurable:true,value:vi.fn()});
  const calls:{url:string;options?:RequestInit}[]=[],capture=vi.fn(async()=>snapshot),wait=vi.fn(async()=>({status:'success'}));let adopted=false;
  vi.stubGlobal('fetch',vi.fn(async(url:string,options?:RequestInit)=>{
   calls.push({url,options});let body:any;
   if(url==='/api/projects')body=[{id:1,title:'测试项目'}];
   else if(url==='/api/studio/location/templates')body={items:[template]};
   else if(url==='/api/projects/1')body={scenes:[{id:2,title:'房间'}]};
   else if(url.endsWith('/shots'))body=[{id:9,title:'正面'}];
   else if(url==='/api/upload')body={task_id:'upload',name:'snapshot.png'};
   else if(url.endsWith('/location-generations')&&options?.method==='POST')body={task_id:'task-1'};
   else if(url.endsWith('/location-generations'))body={items:[]};
   else if(url.endsWith('/candidates/7/select')){adopted=true;body={ok:true}}
   else if(url.endsWith('/location-generations/task-1'))body={task_id:'task-1',status:'success',result_url:'/api/input/1/result.png',candidate:{id:7,is_current:adopted}};
   else throw Error('Unexpected request '+url);
   return new Response(JSON.stringify(body),{status:200,headers:{'Content-Type':'application/json'}});
  }));
  host=document.createElement('div');document.body.append(host);root=createRoot(host);
  await act(async()=>root!.render(<LocationGenerationPanel capture={capture} waitForTask={wait} disabled={false} close={()=>{}}/>));await flush();
  await change('出图项目','1');await change('出图场次','2');await change('出图镜头','9');await click('捕获当前干净机位');
  expect(capture).toHaveBeenCalledTimes(1);expect(host.querySelector('img')!.alt).toContain('待审机位');
  await change('出图提示词','保持机位和人物身份');await act(async()=>element('确认出图审阅').click());
  expect(element('确认出图审阅').checked).toBe(true);
  await change('出图提示词','修改后重新审阅');expect(element('确认出图审阅').checked).toBe(false);
  await change('出图提示词','保持机位和人物身份');expect(element('确认出图审阅').checked).toBe(false);
  await act(async()=>element('确认出图审阅').click());await click('生成待审分镜候选');
  const submitted=calls.find(c=>c.url.endsWith('/location-generations')&&c.options?.method==='POST')!;
  expect(JSON.parse(submitted.options!.body as string)).toMatchObject({template_code:template.code,template_sha256:'hash',shot_id:9,reviewed:true,snapshot:{task_id:'upload',name:'snapshot.png'},camera:snapshot.camera,prompt:'保持机位和人物身份'});
  expect(wait).toHaveBeenCalledWith('task-1');expect(calls.filter(c=>c.url.endsWith('/select'))).toHaveLength(0);
  expect(host.textContent).toContain('当前分镜图未改变');
  vi.spyOn(window,'confirm').mockReturnValue(true);await click('手动采纳为当前分镜图');expect(adopted).toBe(true);
 });
});
