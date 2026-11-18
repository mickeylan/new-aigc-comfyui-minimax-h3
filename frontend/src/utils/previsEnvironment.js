export const defaultWeather=()=>({preset:'clear',intensity:0.5,seed:1,sun_elevation:45,fog_density:0.025})
export function validateEnvironment(doc){
 const w=doc.weather
 if(w&&(!['clear','overcast','rain','snow','fog'].includes(w.preset)||!Number.isFinite(w.intensity)||w.intensity<0||w.intensity>1||!Number.isInteger(w.seed)||w.seed<0||w.seed>2147483647))return '天气参数无效'
 if(w&&(!Number.isFinite(w.sun_elevation??45)||(w.sun_elevation??45)<0||(w.sun_elevation??45)>90||!Number.isFinite(w.fog_density??0.025)||(w.fog_density??0.025)<0||(w.fog_density??0.025)>0.1))return '太阳高度或雾密度无效'
 const bindings=doc.models??[];if(!Array.isArray(bindings)||bindings.length>32)return '最多32个模型绑定'
 const ids=new Set();for(const b of bindings){if(!doc.objects.some(o=>o.id===b.object_id)||ids.has(b.object_id)||!/^[a-f0-9]{64}$/.test(b.asset_hash)||typeof b.file_name!=='string'||b.file_name.length>160||!b.file_name.trim()||!Number.isInteger(b.clip_index)||b.clip_index< -1||b.clip_index>255||!Number.isInteger(b.start_ms)||!Number.isInteger(b.end_ms)||b.start_ms<0||b.end_ms<=b.start_ms||b.end_ms>doc.duration_ms||typeof b.loop!=='boolean')return '模型或动画片段绑定无效';ids.add(b.object_id)}
 return ''
}
export function weatherParticles(weather,timeMS,count=300){let seed=(weather.seed>>>0)||1;const random=()=>{seed=(Math.imul(seed,1664525)+1013904223)>>>0;return seed/4294967296};const positions=new Float32Array(count*3);const speed=weather.preset==='snow'?1.2:9;for(let i=0;i<count;i++){positions[i*3]=(random()-.5)*40;positions[i*3+1]=((random()*20-timeMS/1000*speed)%20+20)%20;positions[i*3+2]=(random()-.5)*40}return positions}
export function clipTime(binding,timeMS,duration){if(timeMS<binding.start_ms||timeMS>binding.end_ms)return null;const seconds=(timeMS-binding.start_ms)/1000;return binding.loop&&duration>0?seconds%duration:Math.min(seconds,duration)}
export function inspectGLB(buffer){
 if(buffer.byteLength>32*1024*1024||buffer.byteLength<20)throw Error('GLB大小必须介于20字节和32MiB之间')
 const view=new DataView(buffer);if(view.getUint32(0,true)!==0x46546c67||view.getUint32(4,true)!==2||view.getUint32(8,true)!==buffer.byteLength||view.getUint32(16,true)!==0x4e4f534a)throw Error('只支持glTF 2.0二进制GLB')
 const length=view.getUint32(12,true);if(length>buffer.byteLength-20)throw Error('GLB JSON区损坏')
 const json=JSON.parse(new TextDecoder().decode(new Uint8Array(buffer,20,length)))
 for(const resource of [...(json.buffers||[]),...(json.images||[])])if(resource.uri)throw Error('GLB必须内嵌所有纹理和缓冲，不允许外部URL或data URI')
 if(json.extensionsRequired?.length)throw Error('暂不支持需要扩展解码器的GLB，请导出未压缩标准GLB')
 if((json.nodes?.length||0)>2048||(json.animations?.length||0)>256||(json.images?.length||0)>32)throw Error('模型节点、片段或纹理数量超限')
 return json
}
export async function localModelStore(hash,buffer){return new Promise((resolve,reject)=>{const request=indexedDB.open('previs-glb-v1',1);request.onupgradeneeded=()=>request.result.createObjectStore('assets');request.onerror=()=>reject(request.error);request.onsuccess=()=>{const db=request.result,tx=db.transaction('assets',buffer?'readwrite':'readonly'),store=tx.objectStore('assets');let result;const op=buffer?store.put(buffer,hash):store.get(hash);op.onsuccess=()=>result=op.result;tx.oncomplete=()=>{db.close();resolve(result)};tx.onerror=()=>{db.close();reject(tx.error)}}})}
