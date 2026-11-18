const copy = value => JSON.parse(JSON.stringify(value))
const vector = p => p && ['x','y','z'].every(k=>Number.isFinite(p[k])&&Math.abs(p[k])<=100)
const cameraOK = c => c && vector(c.position)&&vector(c.target)&&Number.isFinite(c.fov)&&c.fov>=15&&c.fov<=100&&['x','y','z'].reduce((n,k)=>n+(c.position[k]-c.target[k])**2,0)>0.0001
export function upgradeTimeline(doc){return {...copy(doc),schema_version:Math.max(2,doc.schema_version),duration_ms:doc.duration_ms??10000,routes:copy(doc.routes||[]),keyframes:copy(doc.keyframes||[]),camera_keyframes:copy(doc.camera_keyframes||[])}}
export function validateTimeline(input){
 const doc={...input,routes:input.routes??[],keyframes:input.keyframes??[],camera_keyframes:input.camera_keyframes??[]}
 if(doc.schema_version===1)return ['duration_ms','routes','keyframes','camera_keyframes'].some(k=>k in input)?'旧版文档不能包含时间轴':''
 if(![2,3].includes(doc.schema_version)||!Number.isInteger(doc.duration_ms)||doc.duration_ms<1000||doc.duration_ms>120000)return '时间轴时长须为1至120秒'
 if(!Array.isArray(doc.routes)||doc.routes.length>32||!Array.isArray(doc.keyframes)||doc.keyframes.length>256||!Array.isArray(doc.camera_keyframes)||doc.camera_keyframes.length>128)return '时间轴条目超过限制或格式错误'
 const objects=new Set(doc.objects.map(o=>o.id)),routes=new Set(),frames=new Set(),cameras=new Set()
 const time=t=>Number.isInteger(t)&&t>=0&&t<=doc.duration_ms
 for(const r of doc.routes){if(typeof r.id!=='string'||!r.id.trim()||r.id.length>80||routes.has(r.id)||!objects.has(r.object_id)||!vector(r.a)||!vector(r.b))return '路线身份或A/B坐标无效';routes.add(r.id)}
 for(const k of doc.keyframes){const id=`${k.object_id}|${k.time_ms}`;if(!objects.has(k.object_id)||!time(k.time_ms)||!vector(k.position)||!Number.isFinite(k.yaw)||Math.abs(k.yaw)>360||frames.has(id))return '对象关键帧重复或无效';frames.add(id)}
 for(const k of doc.camera_keyframes){if(!time(k.time_ms)||!cameraOK(k.camera)||typeof k.cut!=='boolean'||cameras.has(k.time_ms))return '机位关键帧重复或无效';cameras.add(k.time_ms)}
 return ''
}
const lerp=(a,b,t)=>a+(b-a)*t
const mixVector=(a,b,t)=>Object.fromEntries(['x','y','z'].map(k=>[k,lerp(a[k],b[k],t)]))
function interval(keys,time){const sorted=[...keys].sort((a,b)=>a.time_ms-b.time_ms);let previous=null;for(const next of sorted){if(next.time_ms>time)return [previous,next];previous=next}return [previous,null]}
export function sampleTimeline(doc,time){const result=copy(doc);if(![2,3].includes(doc.schema_version))return result;time=Math.max(0,Math.min(doc.duration_ms,time))
 for(const object of result.objects){const [a,b]=interval((doc.keyframes||[]).filter(k=>k.object_id===object.id),time);if(!a)continue;const t=b?(time-a.time_ms)/(b.time_ms-a.time_ms):0;object.position=b?mixVector(a.position,b.position,t):copy(a.position);const delta=b?((b.yaw-a.yaw+540)%360+360)%360-180:0;object.yaw=((a.yaw+delta*t+540)%360)-180}
 const [a,b]=interval(doc.camera_keyframes||[],time);if(a){if(!b||b.cut)result.camera=copy(a.camera);else{const t=(time-a.time_ms)/(b.time_ms-a.time_ms);const c={position:mixVector(a.camera.position,b.camera.position,t),target:mixVector(a.camera.target,b.camera.target,t),fov:lerp(a.camera.fov,b.camera.fov,t)};result.camera=cameraOK(c)?c:copy(a.camera)}}return result
}
export function applyPrevisPlan(doc,plan,selectedIndices){if(plan.base_hash!==JSON.stringify(doc))throw Error('计划来源已变化，请重新准备')
 if(!Array.isArray(plan.operations)||plan.operations.length>128)throw Error('计划操作无效或过多')
 const next=upgradeTimeline(doc),selected=selectedIndices??plan.operations.map((_,i)=>i)
 if(!Array.isArray(selected)||!selected.length||new Set(selected).size!==selected.length||selected.some(i=>!Number.isInteger(i)||i<0||i>=plan.operations.length))throw Error('请选择有效操作')
 for(const i of selected){const o=plan.operations[i];if(o.kind==='route'&&o.route){next.routes=next.routes.filter(r=>r.id!==o.route.id);next.routes.push(copy(o.route))}else if(o.kind==='keyframe'&&o.keyframe){next.keyframes=next.keyframes.filter(k=>k.object_id!==o.keyframe.object_id||k.time_ms!==o.keyframe.time_ms);next.keyframes.push(copy(o.keyframe))}else if(o.kind==='camera_keyframe'&&o.camera_keyframe){next.camera_keyframes=next.camera_keyframes.filter(k=>k.time_ms!==o.camera_keyframe.time_ms);next.camera_keyframes.push(copy(o.camera_keyframe))}else throw Error('不支持的计划操作')}
 const error=validateTimeline(next);if(error)throw Error(error);return next
}
