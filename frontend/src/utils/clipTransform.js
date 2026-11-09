export const defaultTransform = () => ({fit:'contain',zoom:1,x:0,y:0,rotation:0,crop_left:0,crop_right:0,crop_top:0,crop_bottom:0,keyframes:[]})
const poseKeys=['zoom','x','y','rotation']
function number(value,min,max,name){if(value==null||value===''||typeof value==='boolean'||!Number.isFinite(Number(value))||Number(value)<min||Number(value)>max)throw new RangeError(`${name}参数无效`);return Number(value)}
function pose(value){return {zoom:number(value.zoom,.25,4,'缩放'),x:number(value.x,-1,1,'X'),y:number(value.y,-1,1,'Y'),rotation:number(value.rotation,-360,360,'旋转')}}
export function normalizeTransform(value,duration){
 if(value==null)return null
 const tr={...defaultTransform(),...value}
 if(!['contain','cover'].includes(tr.fit))throw new RangeError('画幅适配无效')
 Object.assign(tr,pose(tr))
 for(const key of ['crop_left','crop_right','crop_top','crop_bottom'])tr[key]=number(tr[key],0,.95,'裁切')
 if(tr.crop_left+tr.crop_right>.95||tr.crop_top+tr.crop_bottom>.95)throw new RangeError('裁切必须至少保留5%画面')
 if(!Array.isArray(tr.keyframes)||tr.keyframes.length>32)throw new RangeError('最多32个关键帧')
 let last=-1
 tr.keyframes=tr.keyframes.map(k=>{const time=number(k.time,0,duration,'关键帧时间');if(time<=last||(last>=0&&time-last<.001-1e-9))throw new RangeError('关键帧须按时间递增且间隔至少1毫秒');last=time;return {...pose(k),time}})
 return tr
}
export function transformAt(tr,time){
 if(!tr)return defaultTransform()
 const keys=tr.keyframes||[]
 if(!keys.length)return {...tr}
 if(time<=keys[0].time)return {...keys[0]}
 const right=keys.findIndex(k=>k.time>=time)
 if(right<0)return {...keys.at(-1)}
 const a=keys[right-1],b=keys[right],ratio=(time-a.time)/(b.time-a.time)
 return Object.fromEntries(poseKeys.map(k=>[k,a[k]+(b[k]-a[k])*ratio]))
}
export function splitTransform(tr,at,total){
 if(!tr)return [null,null]
 if(!tr.keyframes?.length)return [structuredClone(tr),structuredClone(tr)]
 const keys=tr.keyframes
 return [
  {...tr,keyframes:[...keys.filter(k=>k.time<at),{...transformAt(tr,at),time:at}]},
  {...tr,keyframes:[{...transformAt(tr,at),time:0},...keys.filter(k=>k.time>at&&k.time<=total).map(k=>({...k,time:k.time-at}))]},
 ]
}
