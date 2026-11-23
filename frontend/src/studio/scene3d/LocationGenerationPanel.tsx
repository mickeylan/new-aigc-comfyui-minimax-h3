import { useEffect, useRef, useState } from "react";
import { projectChoices, scenePromptChoices } from "./motion-prompt";
import { locationBlocker, locationJSON, locationReviewKey, uploadLocationImage, type LocationSnapshot, type LocationTemplate } from "./locationGeneration";

type Props = { capture: () => Promise<LocationSnapshot>; waitForTask: (taskID:string) => Promise<unknown>; disabled: boolean; close: () => void };
export default function LocationGenerationPanel({capture,waitForTask,disabled,close}:Props) {
  const [projects,setProjects]=useState<any[]>([]), [scenes,setScenes]=useState<any[]>([]), [shots,setShots]=useState<any[]>([]), [templates,setTemplates]=useState<LocationTemplate[]>([]);
  const [project,setProject]=useState(""),[scene,setScene]=useState(""),[shot,setShot]=useState(""),[code,setCode]=useState(""),[prompt,setPrompt]=useState("");
  const [snapshot,setSnapshot]=useState<LocationSnapshot>(),[references,setReferences]=useState<File[]>([]),[review,setReview]=useState("");
  const [working,setWorking]=useState(false),[error,setError]=useState(""),[message,setMessage]=useState(""),[history,setHistory]=useState<any[]>([]),[result,setResult]=useState<any>(null);
  const mounted=useRef(true),lock=useRef(false);
  const template=templates.find(t=>t.code===code),draft={project,scene,shot,prompt,template,snapshot,references},key=locationReviewKey(draft),reviewed=review===key;
  const blocker=locationBlocker({...draft,reviewed});
  const [preview,setPreview]=useState("");
  useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[]);
  useEffect(()=>{if(!snapshot){setPreview("");return}const url=URL.createObjectURL(snapshot.blob);setPreview(url);return()=>URL.revokeObjectURL(url)},[snapshot]);
  useEffect(()=>{let active=true;Promise.all([locationJSON('/api/projects'),locationJSON('/api/studio/location/templates')]).then(([p,t])=>{if(!active)return;setProjects(projectChoices(p));setTemplates(t.items||[]);setCode(t.items?.[0]?.code||"")}).catch(e=>{if(active)setError(e.message)});return()=>{active=false}},[]);
  useEffect(()=>{let active=true;setScene("");setScenes([]);setResult(null);setSnapshot(undefined);setReview("");if(project)locationJSON(`/api/projects/${project}`).then(p=>{if(active)setScenes(scenePromptChoices(p))}).catch(e=>{if(active)setError(e.message)});return()=>{active=false}},[project]);
  useEffect(()=>{let active=true;setShot("");setShots([]);setHistory([]);setResult(null);setSnapshot(undefined);setReview("");if(project&&scene)Promise.all([locationJSON(`/api/projects/${project}/scenes/${scene}/shots`),locationJSON(`/api/projects/${project}/scenes/${scene}/location-generations`)]).then(([s,h])=>{if(active){setShots(Array.isArray(s)?s:s.shots||s.items||[]);setHistory(h.items||[])}}).catch(e=>{if(active)setError(e.message)});return()=>{active=false}},[project,scene]);
  async function action(fn:()=>Promise<void>){if(lock.current)return;lock.current=true;setWorking(true);setError("");try{await fn()}catch(e){if(mounted.current)setError(e instanceof Error?e.message:String(e))}finally{lock.current=false;if(mounted.current)setWorking(false)}}
  async function inspect(taskID:string,base=`/api/projects/${project}/scenes/${scene}`){const value=await locationJSON(`${base}/location-generations/${encodeURIComponent(taskID)}`);if(mounted.current){setResult({...value,base,project});setMessage(value.candidate?'生成结果已保存为待审候选，当前分镜图未改变':`任务状态：${value.status}${value.error?'：'+value.error:''}`)}}
  const generate=()=>action(async()=>{
    if(blocker)throw Error(blocker);
    const base=`/api/projects/${project}/scenes/${scene}`;
    // The preview is immutable: moving the editor camera after capture cannot change the reviewed image.
    const uploaded=await uploadLocationImage(snapshot!.blob,`location-snapshot-${Date.now()}.png`);
    const refs=[];for(const file of references)refs.push(await uploadLocationImage(file,file.name));
    const task=await locationJSON(`${base}/location-generations`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({template_code:template!.code,template_sha256:template!.template_sha256,prompt,reviewed:true,shot_id:Number(shot)||0,snapshot:uploaded,references:refs,camera:snapshot!.camera})});
    setHistory(old=>[{task_id:task.task_id,status:'pending',prompt,shot_id:Number(shot)||0},...old]);setMessage(`任务 ${task.task_id} 已提交；离开页面不影响后台登记，可从本场次历史恢复。`);setReview("");
    await waitForTask(task.task_id);if(mounted.current)await inspect(task.task_id,base);
  });
  return <aside role="dialog" aria-label="Image to Location" className="fixed inset-y-16 right-3 z-50 w-[420px] max-w-[95vw] overflow-y-auto rounded-lg border border-white/20 bg-neutral-900 p-4 text-sm text-white shadow-2xl">
    <h2 className="text-lg">Image to Location · 机位出图</h2><p className="my-2 text-xs text-white/60">同一3D场景可反复切机位分别出图。先生成待审候选，不自动替换场次分镜图；Shot仅关联来源。</p>
    <button type="button" disabled={working} onClick={close}>关闭</button>
    <fieldset disabled={working||disabled} className="space-y-3 mt-3">
      <label className="block">项目<select aria-label="出图项目" className="w-full bg-neutral-800" value={project} onChange={e=>{setProject(e.target.value);setReview("")}}><option value="">请选择</option>{projects.map(p=><option key={p.id} value={p.id}>{p.label}</option>)}</select></label>
      <label className="block">场次<select aria-label="出图场次" className="w-full bg-neutral-800" value={scene} onChange={e=>{setScene(e.target.value);setReview("")}}><option value="">请选择</option>{scenes.map(s=><option key={s.id} value={s.id}>{s.label}</option>)}</select></label>
      <label className="block">关联 Shot（可选）<select aria-label="出图镜头" className="w-full bg-neutral-800" value={shot} onChange={e=>{setShot(e.target.value);setReview("")}}><option value="">仅场次</option>{shots.map(s=><option key={s.id} value={s.id}>#{s.id} {s.title||s.description||s.action||''}</option>)}</select></label>
      <label className="block">图片编辑模板<select aria-label="出图模板" className="w-full bg-neutral-800" value={code} onChange={e=>{setCode(e.target.value);setReview("")}}>{templates.map(t=><option key={t.code} value={t.code}>{t.name}（最多{t.max_refs}张）</option>)}</select></label>
      <button type="button" disabled={!project||!scene} onClick={()=>void action(async()=>{setSnapshot(await capture());setReview("");setMessage("已捕获干净机位。后续移动相机不会改变这张取景；要用新机位请重新捕获。");})}>捕获当前干净机位</button>
      {preview&&<img alt="待审机位快照（第一张参考图）" src={preview} className="w-full"/>}
      <label className="block">额外参考图（按选择顺序，取景图固定第一张）<input aria-label="出图参考图" type="file" accept="image/png,image/jpeg,image/webp" multiple onChange={e=>{setReferences(Array.from(e.target.files||[]));setReview("")}}/></label>
      <ol>{references.map((f,i)=><li key={i}>{i+2}. {f.name}</li>)}</ol>
      <p className="text-xs">引用语法：{template?.reference_syntax||'无可用模板'}；编号1是取景图。不要臆测参考图未呈现的细节。</p>
      <label className="block">人工提示词<textarea aria-label="出图提示词" rows={5} className="w-full bg-neutral-800" value={prompt} onChange={e=>{setPrompt(e.target.value);setReview("")}}/></label>
      <label className="block"><input type="checkbox" aria-label="确认出图审阅" checked={reviewed} disabled={!snapshot} onChange={e=>setReview(e.target.checked?key:"")}/>已核对目标场次、Shot、模板、机位、参考图顺序和提示词</label>
      <button type="button" disabled={!!blocker} onClick={()=>void generate()}>生成待审分镜候选</button>{blocker&&<p className="text-xs text-amber-200">{blocker}</p>}
    </fieldset>
    {working&&<p role="status">正在处理…</p>}{error&&<p role="alert" className="text-red-300">{error}</p>}{message&&<p role="status" className="my-2">{message}</p>}
    {result?.result_url&&<div><img alt="生成候选预览" src={result.result_url} className="w-full"/><p>候选 #{result.candidate.id} · {result.candidate.is_current?'当前':'待人工采纳'}</p><button type="button" disabled={working||result.candidate.is_current} onClick={()=>{if(window.confirm('采纳这张候选为该场次当前分镜图？将按既有规则使下游视频/衔接状态失效。'))void action(async()=>{await locationJSON(`/api/projects/${result.project}/candidates/${result.candidate.id}/select`,{method:'POST'});await inspect(result.task_id,result.base);setMessage('已按人工确认采纳为当前分镜图')})}}>手动采纳为当前分镜图</button></div>}
    {history.length>0&&<section><h3 className="mt-3">本场次最近50次取景任务</h3>{history.map(t=><div key={t.task_id}><button type="button" disabled={working} onClick={()=>void action(()=>inspect(t.task_id))}>{t.task_id} · {t.status}{t.shot_id?` · Shot #${t.shot_id}`:''} · 查看/恢复</button></div>)}</section>}
  </aside>;
}
