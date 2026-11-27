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
 it.each([false,true])('auto captures exact Shot, carries parent and blocks edits during upload=%s',async editDuringUpload=>{
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT',true);
  Object.defineProperty(URL,'createObjectURL',{configurable:true,value:vi.fn(()=> 'blob:linked')});Object.defineProperty(URL,'revokeObjectURL',{configurable:true,value:vi.fn()});
  let current=true;const source={version:1,revision:'a'.repeat(64),projectId:1,sceneIdNumber:2,shotId:9,planShotId:'director-shot:9',sampleSeconds:0};
  const capture=vi.fn(),captureShot=vi.fn(async()=>({...snapshot,isCurrent:()=>current,camera:{...snapshot.camera,storyboard_source:source}} as any));
  const writes:{url:string;options:RequestInit}[]=[];
  vi.stubGlobal('fetch',vi.fn(async(url:string,options?:RequestInit)=>{
   if(options?.method)writes.push({url,options});let body:any;
   if(url==='/api/projects')body=[{id:1,title:'project'}];
   else if(url.endsWith('/templates'))body={items:[template]};
   else if(url==='/api/projects/1')body={scenes:[{id:2,title:'scene'}]};
   else if(url.endsWith('/shots'))body=[{id:9,title:'精确镜头'}];
   else if(url==='/api/upload'){if(editDuringUpload)current=false;body={task_id:'upload',name:'snapshot.png'}}
   else if(url.endsWith('/location-generations')&&options?.method==='POST')body={task_id:'task-1'};
   else if(url.endsWith('/location-generations'))body={items:[]};
   else if(url.endsWith('/task-1'))body={status:'pending'};
   else throw Error('Unexpected '+url);
   return new Response(JSON.stringify(body),{status:200});
  }));
  host=document.createElement('div');document.body.append(host);root=createRoot(host);
  await act(async()=>root!.render(<LocationGenerationPanel context={{projectId:1,sceneId:2}} capture={capture} captureShot={captureShot} waitForTask={async()=>{}} disabled={false} close={()=>{}}/>));await flush();
  await change('出图镜头','9');expect(captureShot).toHaveBeenCalledExactlyOnceWith('clay',{projectId:1,sceneId:2},9);expect(capture).not.toHaveBeenCalled();expect(writes).toHaveLength(0);
  expect(host.textContent).toContain('同源版本 aaaaaaaaaaaa');
  await change('出图提示词','保持参考结构，不臆测外观');await act(async()=>element('确认出图审阅').click());await click('生成待审分镜候选');
  const submitted=writes.filter(c=>c.url.endsWith('/location-generations'));
  if(editDuringUpload){expect(submitted).toHaveLength(0);expect(host.querySelector('[role="alert"]')!.textContent).toContain('已变化')}
  else{expect(submitted).toHaveLength(1);expect(JSON.parse(submitted[0].options.body as string).camera.storyboard_source).toEqual(source)}
 });
 it('keeps an unchanged preview reviewed across busy playback, clears real staleness after undo but requires renewed review',async()=>{
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT',true);
  Object.defineProperty(URL,'createObjectURL',{configurable:true,value:vi.fn(()=> 'blob:freshness')});Object.defineProperty(URL,'revokeObjectURL',{configurable:true,value:vi.fn()});
  let current=true;
  const fetcher=vi.fn(async(url:string,_options?:RequestInit)=>{
   const body=url==='/api/projects'?[{id:1,title:'project'}]:url.endsWith('/templates')?{items:[template]}:url==='/api/projects/1'?{scenes:[{id:2,title:'scene'}]}:url.endsWith('/shots')?[]:{items:[]};
   return new Response(JSON.stringify(body),{status:200});
  });
  vi.stubGlobal('fetch',fetcher);
  const capture=vi.fn(async()=>({...snapshot,isCurrent:()=>current}));
  const panel=(disabled:boolean)=><LocationGenerationPanel context={{projectId:1,sceneId:2}} capture={capture} waitForTask={async()=>{}} disabled={disabled} close={()=>{}}/>;
  host=document.createElement('div');document.body.append(host);root=createRoot(host);
  await act(async()=>root!.render(panel(false)));await flush();
  vi.useFakeTimers();
  try{
   await click('捕获当前白模机位');await change('出图提示词','原图结构不变');await act(async()=>element('确认出图审阅').click());
   const generate=()=>[...host!.querySelectorAll('button')].find(b=>b.textContent==='生成待审分镜候选')!;
   expect(generate().disabled).toBe(false);
   await act(async()=>root!.render(panel(true)));await act(async()=>vi.advanceTimersByTime(1000));
   expect(element('确认出图审阅').checked).toBe(true);expect(host.querySelector('[role="alert"]')).toBeNull();
   await act(async()=>root!.render(panel(false)));expect(generate().disabled).toBe(false);
   current=false;await act(async()=>vi.advanceTimersByTime(500));
   expect(host.querySelector('[role="alert"]')!.textContent).toContain('场景或镜头表已变化');expect(generate().disabled).toBe(true);expect(element('确认出图审阅').checked).toBe(false);
   current=true;await act(async()=>vi.advanceTimersByTime(500));
   expect(host.querySelector('[role="alert"]')).toBeNull();expect(generate().disabled).toBe(true);
   await act(async()=>element('确认出图审阅').click());expect(generate().disabled).toBe(false);
   expect(capture).toHaveBeenCalledOnce();expect(fetcher.mock.calls.every(([,options])=>!options?.method)).toBe(true);
   await act(async()=>root!.unmount());root=undefined;
  }finally{vi.useRealTimers()}
 });
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
 it('prefills the linked Scene, captures clay only on request, resets review on mode change and submits ordered references without changing Scene state',async()=>{
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT',true);
  Object.defineProperty(URL,'createObjectURL',{configurable:true,value:vi.fn(()=> 'blob:clay')});Object.defineProperty(URL,'revokeObjectURL',{configurable:true,value:vi.fn()});
  const calls:{url:string;options?:RequestInit}[]=[],capture=vi.fn(async()=>snapshot);
  vi.stubGlobal('fetch',vi.fn(async(url:string,options?:RequestInit)=>{
   calls.push({url,options});let body:any;
   if(url==='/api/projects')body=[{id:1,title:'来源项目'}];
   else if(url==='/api/studio/location/templates')body={items:[template]};
   else if(url==='/api/projects/1')body={scenes:[{id:2,title:'来源场次'}]};
   else if(url.endsWith('/shots'))body=[{id:9,title:'人工关联镜头'}];
   else if(url==='/api/upload')body={task_id:'upload',name:((options!.body as FormData).get('file') as File).name};
   else if(url.endsWith('/location-generations')&&options?.method==='POST')body={task_id:'clay-task'};
   else if(url.endsWith('/location-generations'))body={items:[]};
   else if(url.endsWith('/clay-task'))body={task_id:'clay-task',status:'pending'};
   else throw Error('Unexpected request '+url);
   return new Response(JSON.stringify(body),{status:200});
  }));
  host=document.createElement('div');document.body.append(host);root=createRoot(host);
  await act(async()=>root!.render(<LocationGenerationPanel context={{projectId:1,sceneId:2}} capture={capture} waitForTask={async()=>{}} disabled={false} close={()=>{}}/>));await flush();
  expect(element('出图项目').value).toBe('1');expect(element('出图场次').value).toBe('2');
  expect(element('出图项目').disabled).toBe(true);expect(element('出图场次').disabled).toBe(true);
  expect(element('结构参考模式').value).toBe('clay');expect(capture).not.toHaveBeenCalled();
  expect(calls.every(c=>!c.options?.method)).toBe(true);
  await change('出图镜头','9');await click('捕获当前白模机位');expect(capture).toHaveBeenCalledWith('clay');
  await change('出图提示词','参考1只约束空间，参考2保持人物身份');await act(async()=>element('确认出图审阅').click());expect(element('确认出图审阅').checked).toBe(true);
  expect(host.textContent).toContain('已捕获不可变机位图');
  await change('结构参考模式','viewport');expect(element('确认出图审阅').checked).toBe(false);expect(host.querySelector('img')).toBeNull();
  expect(element('确认出图审阅').disabled).toBe(true);
  expect(host.textContent).not.toContain('已捕获不可变机位图');
  await change('结构参考模式','clay');await click('捕获当前白模机位');
  await act(async()=>{Object.defineProperty(element('出图参考图'),'files',{configurable:true,value:[new File(['identity'],'identity.png',{type:'image/png'})]});element('出图参考图').dispatchEvent(new Event('change',{bubbles:true}))});
  expect(host.querySelector('[aria-label="快照参考来源"]')!.textContent).toContain('白模结构图 · 场次 #2 / Shot #9');
  await click('生成待审分镜候选');expect(calls.filter(c=>c.options?.method==='POST')).toHaveLength(0);
  await act(async()=>element('确认出图审阅').click());await click('生成待审分镜候选');
  const post=calls.find(c=>c.url==='/api/projects/1/scenes/2/location-generations'&&c.options?.method==='POST')!;
  const payload=JSON.parse(post.options!.body as string);
  expect(payload).toMatchObject({shot_id:9,reviewed:true,camera:{...snapshot.camera,reference_pass:'clay'},references:[{task_id:'upload',name:'identity.png'}]});
  expect(payload.snapshot.name).toMatch(/^location-snapshot-\d+\.png$/);
  expect(calls.filter(c=>c.options?.method)).toHaveLength(3); // 两张上传+一条候选任务；没有PATCH场次或采纳请求。
  expect(element('确认出图审阅').checked).toBe(false);
 });
 it('rejects a linked Scene missing from the project instead of silently selecting another Scene',async()=>{
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT',true);
  const capture=vi.fn();vi.stubGlobal('fetch',vi.fn(async(url:string)=>new Response(JSON.stringify(url==='/api/projects'?[{id:1,title:'project'}]:url.endsWith('/templates')?{items:[template]}:{scenes:[{id:99,title:'other'}]}),{status:200})));
  host=document.createElement('div');document.body.append(host);root=createRoot(host);
  await act(async()=>root!.render(<LocationGenerationPanel context={{projectId:1,sceneId:2}} capture={capture} waitForTask={async()=>{}} disabled={false} close={()=>{}}/>));await flush();
  expect(element('出图场次').value).toBe('');expect(host.querySelector('[role="alert"]')!.textContent).toContain('禁止自动改投');
  await click('捕获当前白模机位');expect(capture).not.toHaveBeenCalled();
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
