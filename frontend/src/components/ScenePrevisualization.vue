<template>
  <section class="previs-panel">
    <h3>轻量 3D 站位 / 机位预演</h3>
    <p class="field-hint">单位：米；Y向上、0°面向+Z。拖人物/道具移动，拖空白旋转机位，滚轮缩放。这里只做预演，不生成图片或视频，不自动修改提示词。</p>
    <p v-if="error" role="alert" class="previs-error">{{ error }}</p>
    <p v-if="stale" role="status">Scene正文已变化，旧预演保留供参考；确认核对后才能以当前正文版本保存。</p>
    <div ref="viewport" :style="assetBusy?{pointerEvents:'none'}:{}" class="previs-viewport" aria-label="3D站位预演画布"></div>
    <div v-if="loading">加载草稿…</div>
    <fieldset v-else :disabled="assetBusy" class="previs-editor">
      <PrevisTimeline ref="timeline" :document="doc" :selected-id="selectedID" :project-id="projectId" :scene-id="sceneId" @update-document="doc=$event" @sample="setSample" @pick="pickPoint=$event;sampled=null;status='请在地面点击标记 '+$event.toUpperCase()" />
      <div class="previs-toolbar"><button type="button" class="btn btn-sm btn-secondary" @click="addObject('character')" :disabled="doc.objects.length>=32">＋人物</button><button type="button" class="btn btn-sm btn-secondary" @click="addObject('prop')" :disabled="doc.objects.length>=32">＋道具</button><button type="button" class="btn btn-sm btn-secondary" @click="addObject('wall')" :disabled="doc.objects.length>=32">＋墙/障碍</button><button v-for="kind in ['building','tree','plant']" :key="kind" type="button" class="btn btn-sm btn-secondary" :disabled="doc.objects.length>=32" @click="addObject(kind)">＋{{{building:'建筑占位',tree:'树木占位',plant:'花木占位'}[kind]}}</button><button type="button" class="btn btn-sm btn-secondary" @click="topView">俯视</button><button type="button" class="btn btn-sm btn-secondary" :disabled="!rendererReady" @click="downloadPNG">下载预演 PNG</button></div>
      <PrevisEnvironmentPanel :document="doc" :selected="selected" :information="modelInformation[selectedID]" :busy="assetBusy" :ready="rendererReady" @update-document="updateEnvironment" @import-model="importModel" @export="exportFrames" />
      <div class="previs-grid"><div><label>对象<select v-model="selectedID" class="input"><option value="">选择对象</option><option v-for="o in doc.objects" :key="o.id" :value="o.id">{{ o.name }}</option></select></label>
        <template v-if="selected"><label>名称<input v-model="selected.name" maxlength="80" class="input"></label><div class="previs-fields"><label v-for="axis in axes" :key="axis">{{axis.toUpperCase()}}<input v-model.number="selected.position[axis]" type="number" min="-100" max="100" step="0.1" class="input"></label><label>朝向°<input v-model.number="selected.yaw" type="number" min="-360" max="360" class="input"></label><label v-for="(caption,key) in dimensions" :key="key">{{caption}}<input v-model.number="selected[key]" type="number" min="0.05" max="30" step="0.1" class="input"></label></div><button type="button" class="btn btn-sm btn-danger" @click="removeObject">移除选中对象</button></template>
      </div><div><label>机位位置</label><div class="previs-fields"><label v-for="axis in axes" :key="axis">{{axis.toUpperCase()}}<input v-model.number="doc.camera.position[axis]" type="number" min="-100" max="100" step="0.1" class="input"></label></div><label>机位看向</label><div class="previs-fields"><label v-for="axis in axes" :key="axis">{{axis.toUpperCase()}}<input v-model.number="doc.camera.target[axis]" type="number" min="-100" max="100" step="0.1" class="input"></label><label>视角°<input v-model.number="doc.camera.fov" type="number" min="15" max="100" class="input"></label></div>
        <div class="previs-toolbar"><input v-model="viewName" maxlength="80" class="input" placeholder="命名当前机位"><button type="button" class="btn btn-sm btn-secondary" @click="saveView" :disabled="doc.views.length>=12">记住机位</button></div><div v-for="(v,i) in doc.views" :key="v.name" class="previs-toolbar"><button type="button" class="btn btn-xs btn-secondary" @click="doc.camera=clonePrevis(v.camera)">{{v.name}}</button><button type="button" class="btn btn-xs btn-ghost" @click="doc.views.splice(i,1)">删除</button></div>
      </div></div>
      <div class="previs-toolbar"><button type="button" class="btn" :disabled="promptBusy || !!validatePrevis(doc) || stale || !sourceHash" @click="previewPrompt('image')">生成分镜图提示词草稿</button><button type="button" class="btn" :disabled="promptBusy || !!validatePrevis(doc) || stale || !sourceHash" @click="previewPrompt('video')">生成视频提示词草稿</button><span class="field-hint">无需先保存站位；在下方审阅编辑，确认采用时同时保存站位和对应提示词。</span></div>
      <details><summary>审阅站位建议（仅写入可编辑表单）</summary><pre class="previs-suggestion">{{suggestion.suggestions.join('\n')}}\n{{suggestion.camera}}</pre></details>
      <div class="previs-toolbar"><label v-if="stale"><input type="checkbox" v-model="staleReviewed">我已核对当前正文</label><button type="button" class="btn btn-secondary" :disabled="saving || (stale&&!staleReviewed)" @click="save">{{saving?'保存中…':'保存预演草稿'}}</button><button type="button" class="btn" :disabled="!!validatePrevis(doc) || stale" @click="adopt">采用为可编辑站位建议</button><button type="button" class="btn btn-ghost" :disabled="saving" @click="close">关闭预演</button><span role="status">{{status}}</span></div>
    </fieldset>
    <p v-if="assetBusy" role="status">{{assetStatus}} <button v-if="exporting" type="button" @click="cancelExport=true">取消导出</button></p>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api } from '../api'
import { clonePrevis, createPrevis, previsSuggestion, validatePrevis } from '../utils/scenePrevisualization.js'
import PrevisTimeline from './PrevisTimeline.vue'
import { upgradeTimeline } from '../utils/previsTimeline.js'
import PrevisEnvironmentPanel from './PrevisEnvironmentPanel.vue'
import { defaultWeather, inspectGLB, localModelStore } from '../utils/previsEnvironment.js'
import { exportFrameTimes, frameArchive } from '../utils/previsFrameArchive.js'
import { sampleTimeline } from '../utils/previsTimeline.js'
import { requestErrorMessage } from '../utils/requestError.js'
const props=defineProps({projectId:{type:[String,Number],required:true},sceneId:{type:Number,required:true},names:{type:Array,default:()=>[]},promptBusy:{type:Boolean,default:false}})
const emit=defineEmits(['adopt','close','preview-prompt','invalidate-prompt'])
const axes=['x','y','z'],dimensions={width:'宽',height:'高',depth:'深'}
const sampled=ref(null),pickPoint=ref('')
const doc=ref(upgradeTimeline(createPrevis(props.names))),viewport=ref(null),selectedID=ref(''),viewName=ref(''),loading=ref(true),saving=ref(false),stale=ref(false),staleReviewed=ref(false),error=ref(''),status=ref('')
const selected=computed(()=>doc.value.objects.find(o=>o.id===selectedID.value)),suggestion=computed(()=>previsSuggestion(doc.value))
const rendererReady=ref(false),timeline=ref(null),modelInformation=ref({}),assetBusy=ref(false),exporting=ref(false),assetStatus=ref('')
let assets,exportPrevisFrame,cancelExport=false,sampleTime=0
let alive=true,revision=0,sourceHash='',baseline='',THREE,renderer,scene,camera,group,resizeObserver,drag=null,epoch=0
function disposeGroup(){if(!group)return;group.traverse(o=>{o.geometry?.dispose();if(Array.isArray(o.material))o.material.forEach(m=>m.dispose());else {o.material?.map?.dispose();o.material?.dispose()}});scene.remove(group)}
let geometrySignature='',meshes=new Map()
function setSample(value,time=0){sampled.value=value;sampleTime=value?time:0;render()}
function render(){
 if(!renderer||!alive||validatePrevis(doc.value))return
 const display=sampled.value||doc.value
 const signature=JSON.stringify([doc.value.objects.map(({id,kind,width,height,depth})=>({id,kind,width,height,depth})),selectedID.value,doc.value.routes])
 if(!group||signature!==geometrySignature){
 geometrySignature=signature;disposeGroup();meshes.clear();group=new THREE.Group();scene.add(group)
 const grid=new THREE.GridHelper(40,40,0x718096,0x364152);group.add(grid)
 for(const r of doc.value.routes){
  const points=[r.a,r.b].map(p=>new THREE.Vector3(p.x,p.y+0.06,p.z));group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(points),new THREE.LineBasicMaterial({color:0xffcf70})))
  for(const [index,p] of points.entries()){
   const color=index===0?0x46d293:0xffa04b,marker=new THREE.Mesh(new THREE.SphereGeometry(0.18,10,8),new THREE.MeshBasicMaterial({color}));marker.position.copy(p);group.add(marker)
   const canvas=document.createElement('canvas');canvas.width=64;canvas.height=64;const ctx=canvas.getContext('2d');ctx.fillStyle=index===0?'#46d293':'#ffa04b';ctx.font='bold 48px sans-serif';ctx.textAlign='center';ctx.fillText(index===0?'A':'B',32,50);const label=new THREE.Sprite(new THREE.SpriteMaterial({map:new THREE.CanvasTexture(canvas),depthTest:false}));label.position.copy(p).add(new THREE.Vector3(0,0.7,0));label.scale.set(0.7,0.7,1);group.add(label)
  }
 }
 doc.value.objects.forEach((o,i)=>{
  const geometry=o.kind==='character'?new THREE.CylinderGeometry(o.width/2,o.width/2,o.height,12):new THREE.BoxGeometry(o.width,o.height,o.depth)
  const mesh=new THREE.Mesh(geometry,new THREE.MeshStandardMaterial({color:o.id===selectedID.value?0xffc857:o.kind==='wall'?0x718096:o.kind==='prop'?0x77c9a6:[0x5fa8ff,0xe78ab4,0xf2b86d][i%3]}))
  mesh.position.set(o.position.x,o.position.y+o.height/2,o.position.z);mesh.rotation.y=o.yaw*Math.PI/180;mesh.userData.objectID=o.id;group.add(mesh)
  const arrow=new THREE.ArrowHelper(new THREE.Vector3(Math.sin(o.yaw*Math.PI/180),0,Math.cos(o.yaw*Math.PI/180)),new THREE.Vector3(o.position.x,o.position.y+o.height,o.position.z),1,0xffffff);group.add(arrow);meshes.set(o.id,{mesh,arrow})
 })
 }
 for(const o of display.objects){const pair=meshes.get(o.id);if(!pair)continue;pair.mesh.position.set(o.position.x,o.position.y+o.height/2,o.position.z);pair.mesh.rotation.y=o.yaw*Math.PI/180;pair.arrow.position.set(o.position.x,o.position.y+o.height,o.position.z);pair.arrow.setDirection(new THREE.Vector3(Math.sin(o.yaw*Math.PI/180),0,Math.cos(o.yaw*Math.PI/180)))}
 if(assets){assets.update(display,sampleTime);for(const o of display.objects){const pair=meshes.get(o.id),item=assets.models.get(o.id),binding=display.models?.find(b=>b.object_id===o.id);if(pair)pair.mesh.visible=!(binding&&item?.hash===binding.asset_hash)}assets.ensure(doc.value.models||[],(id,item,e)=>{if(!alive)return;const binding=doc.value.models?.find(b=>b.object_id===id);if(item&&binding?.asset_hash!==item.hash)return;modelInformation.value[id]=e?{message:e.message}:modelInfo(item);render()})}
 const p=display.camera.position,t=display.camera.target
 camera.position.set(p.x,p.y,p.z);camera.lookAt(t.x,t.y,t.z);camera.fov=display.camera.fov
 const width=Math.max(viewport.value.clientWidth,1),height=360
 renderer.setSize(width,height,false);camera.aspect=width/height;camera.updateProjectionMatrix();renderer.render(scene,camera)
}
function ray(event){const r=renderer.domElement.getBoundingClientRect();const cast=new THREE.Raycaster();cast.setFromCamera(new THREE.Vector2((event.clientX-r.left)/r.width*2-1,-(event.clientY-r.top)/r.height*2+1),camera);return cast}
function pointerDown(e){if(!renderer||validatePrevis(doc.value)||sampled.value)return;if(pickPoint.value){const o=doc.value.objects.find(v=>v.id===selectedID.value);if(!o)return;const p=new THREE.Vector3();if(!ray(e).ray.intersectPlane(new THREE.Plane(new THREE.Vector3(0,1,0),0),p))return;if(Math.abs(p.x)>100||Math.abs(p.z)>100){error.value='标记超出100米边界';return}let r=doc.value.routes.find(v=>v.object_id===o.id);if(!r){if(doc.value.routes.length>=32){error.value='路线数量已达上限';return}r={id:`route-${o.id}`,object_id:o.id,a:clonePrevis(o.position),b:clonePrevis(o.position)};doc.value.routes.push(r)}r[pickPoint.value]={x:Math.round(p.x*10)/10,y:0,z:Math.round(p.z*10)/10};status.value=`已标记 ${pickPoint.value.toUpperCase()}`;pickPoint.value='';return} const hit=ray(e).intersectObjects([...group.children,...(assets?.root.children||[])],true).find(h=>h.object.userData.objectID);drag={x:e.clientX,y:e.clientY,id:hit?.object.userData.objectID};if(drag.id)selectedID.value=drag.id;renderer.domElement.setPointerCapture(e.pointerId)}
function pointerMove(e){if(!drag)return
 if(drag.id){const o=doc.value.objects.find(o=>o.id===drag.id);if(!o)return;const point=new THREE.Vector3();if(!ray(e).ray.intersectPlane(new THREE.Plane(new THREE.Vector3(0,1,0),-o.position.y),point))return;o.position.x=Math.max(-100,Math.min(100,Math.round(point.x*10)/10));o.position.z=Math.max(-100,Math.min(100,Math.round(point.z*10)/10))}
 else {const p=doc.value.camera.position,t=doc.value.camera.target;const v=new THREE.Vector3(p.x-t.x,p.y-t.y,p.z-t.z);const sphere=new THREE.Spherical().setFromVector3(v);sphere.theta-=(e.clientX-drag.x)*0.008;sphere.phi=Math.max(0.03,Math.min(Math.PI-0.03,sphere.phi+(e.clientY-drag.y)*0.008));v.setFromSpherical(sphere);doc.value.camera.position={x:t.x+v.x,y:t.y+v.y,z:t.z+v.z}}
 drag.x=e.clientX;drag.y=e.clientY
}
function pointerUp(){drag=null}
function wheel(e){e.preventDefault();const p=doc.value.camera.position,t=doc.value.camera.target;const scale=e.deltaY>0?1.1:0.9;const next={x:t.x+(p.x-t.x)*scale,y:t.y+(p.y-t.y)*scale,z:t.z+(p.z-t.z)*scale};if(axes.every(k=>Math.abs(next[k])<=100)&&Math.hypot(next.x-t.x,next.y-t.y,next.z-t.z)>0.2)doc.value.camera.position=next}
async function initialize(){try{
 THREE=await import('three');const runtime=await import('../utils/previsRenderAssets.js');if(!alive)return;exportPrevisFrame=runtime.exportPrevisFrame
 renderer=new THREE.WebGLRenderer({antialias:true,preserveDrawingBuffer:true});renderer.setPixelRatio(Math.min(window.devicePixelRatio||1,2));scene=new THREE.Scene();scene.background=new THREE.Color(0x18212f);camera=new THREE.PerspectiveCamera(45,1,0.05,500);scene.add(new THREE.HemisphereLight(0xffffff,0x354052,3));assets=runtime.createAssetRuntime(THREE,scene);viewport.value.appendChild(renderer.domElement)
 renderer.domElement.style.touchAction='none';renderer.domElement.addEventListener('pointerdown',pointerDown);renderer.domElement.addEventListener('pointermove',pointerMove);renderer.domElement.addEventListener('pointerup',pointerUp);renderer.domElement.addEventListener('pointercancel',pointerUp);renderer.domElement.addEventListener('wheel',wheel,{passive:false});resizeObserver=new ResizeObserver(render);resizeObserver.observe(viewport.value);render();rendererReady.value=true
 }catch(e){if(alive)error.value=`3D画布不可用，仍可编辑和保存参数：${e.message}`}}
onMounted(async()=>{const token=++epoch
 try{const {data}=await api.getScenePrevisualization(props.projectId,props.sceneId);if(!alive||token!==epoch)return;sourceHash=data.source_content_hash;if(data.item){const saved=JSON.parse(data.item.document_json);if(validatePrevis(saved))throw new Error('保存的预演不符合当前版本，请保留历史记录');doc.value=upgradeTimeline(saved);revision=data.item.revision;stale.value=!!data.stale}baseline=JSON.stringify(doc.value)}catch(e){if(alive)error.value=requestErrorMessage(e,'读取预演失败，暂不可保存')}
 finally{if(alive){loading.value=false;initialize()}}
})
watch([doc,selectedID],render,{deep:true})
watch(doc,()=>{sampled.value=null;emit('invalidate-prompt')}, {deep:true,flush:'sync'})
function previewPrompt(kind){if(validatePrevis(doc.value)||stale.value||!sourceHash)return;emit('preview-prompt',{kind,suggestion:clonePrevis(suggestion.value)})}
function addObject(kind){const id=globalThis.crypto?.randomUUID?.()||`${Date.now()}-${Math.random()}`;doc.value.objects.push({id,name:kind==='character'?'新人物':kind==='wall'?'墙/障碍':kind==='building'?'建筑占位':kind==='tree'?'树木占位':kind==='plant'?'花木占位':'新道具',kind,position:{x:0,y:0,z:0},yaw:0,width:kind==='building'?6:kind==='wall'?4:kind==='tree'?2:0.6,height:kind==='building'?5:kind==='tree'?4:kind==='character'?1.7:kind==='wall'?2.5:1,depth:kind==='building'?5:kind==='tree'?2:0.4});selectedID.value=id}
function removeObject(){const id=selectedID.value;doc.value={...doc.value,models:(doc.value.models||[]).filter(b=>b.object_id!==id),objects:doc.value.objects.filter(o=>o.id!==id),routes:doc.value.routes.filter(r=>r.object_id!==id),keyframes:doc.value.keyframes.filter(k=>k.object_id!==id)};selectedID.value=''}
function topView(){const t=doc.value.camera.target;doc.value.camera.position={x:t.x,y:Math.min(100,t.y+15),z:t.z+0.01}}
function saveView(){const name=viewName.value.trim();if(!name||doc.value.views.some(v=>v.name===name)){error.value='请输入不重复的机位名称';return}doc.value.views.push({name,camera:clonePrevis(doc.value.camera)});viewName.value=''}
async function save(){error.value=validatePrevis(doc.value);if(error.value)return;if(!sourceHash){error.value='未成功读取正文版本，不能保存';return}const token=epoch,payload=clonePrevis(doc.value);saving.value=true
 try{const {data}=await api.saveScenePrevisualization(props.projectId,props.sceneId,{revision,source_content_hash:sourceHash,document:payload});if(!alive||token!==epoch)return;revision=data.revision;baseline=JSON.stringify(payload);stale.value=false;status.value='预演已保存；未修改人物站位或生成提示词'}catch(e){if(alive)error.value=requestErrorMessage(e,'保存预演失败，本地草稿保留')}finally{if(alive)saving.value=false}}
function adopt(){if(validatePrevis(doc.value))return;emit('adopt',clonePrevis(suggestion.value));status.value='已填入站位编辑表单，请核对后另行保存人物站位'}
function close(){if(JSON.stringify(doc.value)!==baseline&&!window.confirm('预演尚有未保存修改，确定关闭？'))return;emit('close')}
function downloadPNG(){render();renderer.domElement.toBlob(blob=>{if(!blob)return;const url=URL.createObjectURL(blob),a=document.createElement('a');a.href=url;a.download=`scene-${props.sceneId}-previs.png`;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)},'image/png')}
function modelInfo(item){return {message:item.skin?'已加载蒙皮骨骼模型':'已加载静态/节点动画模型（无蒙皮骨骼）',clips:item.gltf.animations.map(c=>({name:c.name,duration:c.duration}))}}
function updateEnvironment(next){const e=validatePrevis(next);if(e){error.value=e;return}doc.value=next;error.value=''}
async function importModel({file,object_id}){if(!assets||assetBusy.value)return;timeline.value?.pause();assetBusy.value=true;assetStatus.value='检查并保存本地GLB…';error.value='';try{if(file.size>32*1024*1024)throw Error('GLB超过32MiB');const buffer=await file.arrayBuffer();inspectGLB(buffer);const hash=Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',buffer)),v=>v.toString(16).padStart(2,'0')).join('');if(!alive)return;const binding={object_id,asset_hash:hash,file_name:file.name.slice(0,160),clip_index:-1,start_ms:0,end_ms:doc.value.duration_ms,loop:true};await localModelStore(hash,buffer);const item=await assets.load(binding,buffer);if(!alive)return;if(!doc.value.objects.some(o=>o.id===object_id))throw Error('对象已移除，未绑定模型');modelInformation.value[object_id]=modelInfo(item);doc.value={...doc.value,schema_version:3,weather:doc.value.weather||defaultWeather(),models:[...(doc.value.models||[]).filter(b=>b.object_id!==object_id),binding]};status.value='GLB已导入当前浏览器；请保存预演元数据，选择动画片段后播放'}catch(e){if(alive)error.value=e.message}finally{if(alive){assetBusy.value=false;render()}}}
function downloadBlob(blob,name){const url=URL.createObjectURL(blob),a=document.createElement('a');a.href=url;a.download=name;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}
async function exportFrames(options){if(!renderer||assetBusy.value)return;error.value=validatePrevis(doc.value);if(error.value)return;const base=clonePrevis(doc.value),previousSample=sampled.value,previousTime=sampleTime;try{const bindings=base.models||[];for(const b of bindings){if(assets.models.get(b.object_id)?.hash!==b.asset_hash)throw Error('模型未恢复，禁止悄悄用占位体导出；请重新导入缺失GLB');if(b.clip_index>=assets.models.get(b.object_id).gltf.animations.length)throw Error('绑定的动画片段不存在，请重新选择')}const times=options.fps?exportFrameTimes(options.from_ms,options.to_ms,options.fps,base.duration_ms):[sampleTime];timeline.value?.pause();exporting.value=true;assetBusy.value=true;cancelExport=false;const entries=[];let totalBytes=0;for(let i=0;i<times.length;i++){if(cancelExport||!alive)throw Error('导出已取消');assetStatus.value=`导出 ${i+1}/${times.length}`;sampled.value=sampleTimeline(base,times[i]);sampleTime=times[i];render();const blob=await exportPrevisFrame(THREE,renderer,scene,camera,group,assets,options.mode,options.depth_range);if(cancelExport||!alive)throw Error('导出已取消');totalBytes+=blob.size;if(totalBytes>256*1024*1024)throw Error('序列超过256MiB，请缩小导出范围');if(options.fps)entries.push({name:`${options.mode}-${String(i).padStart(5,'0')}.png`,blob});else downloadBlob(blob,`scene-${props.sceneId}-${options.mode}.png`);await new Promise(r=>setTimeout(r,0))}if(options.fps){entries.push({name:'manifest.json',text:JSON.stringify({schema_version:1,mode:options.mode,width:1280,height:720,fps:options.fps,times_ms:times,files:entries.map(e=>e.name),depth:{near:options.depth_range?.near??0.1,far:options.depth_range?.far??50,camera_near:camera.near,camera_far:camera.far,encoding:'8bit-linear-near-white-far-black'},document:base},null,2)});assetStatus.value='打包ZIP…';const archive=await frameArchive(entries);if(alive&&!cancelExport)downloadBlob(archive,`scene-${props.sceneId}-${options.mode}.zip`)}if(alive)status.value='导出完成，仅下载，未上传或修改生成任务'}catch(e){if(alive)error.value=e.message}finally{if(alive){sampled.value=previousSample;sampleTime=previousTime;assetBusy.value=false;exporting.value=false;render()}}}
function beforeUnload(e){if(JSON.stringify(doc.value)!==baseline){e.preventDefault();e.returnValue=''}}
window.addEventListener('beforeunload',beforeUnload)
onBeforeUnmount(()=>{alive=false;epoch++;window.removeEventListener('beforeunload',beforeUnload);resizeObserver?.disconnect();if(renderer){const el=renderer.domElement;el.removeEventListener('pointerdown',pointerDown);el.removeEventListener('pointermove',pointerMove);el.removeEventListener('pointerup',pointerUp);el.removeEventListener('pointercancel',pointerUp);el.removeEventListener('wheel',wheel);cancelExport=true;assets?.dispose();disposeGroup();renderer.dispose();renderer.forceContextLoss();el.remove()}})
</script>

<style scoped>
.previs-editor{padding:0;margin:0;border:0;min-width:0}.previs-panel{margin:1rem 0;padding:1rem;border:1px solid var(--border);border-radius:8px}.previs-viewport{width:100%;height:360px;overflow:hidden;border-radius:6px;background:#18212f}.previs-toolbar{display:flex;flex-wrap:wrap;gap:.5rem;align-items:center;margin:.7rem 0}.previs-grid{display:grid;grid-template-columns:1fr 1fr;gap:1rem}.previs-fields{display:flex;flex-wrap:wrap;gap:.4rem}.previs-fields label{width:5rem}.previs-error{color:var(--danger,#e45656);white-space:pre-wrap}.previs-suggestion{white-space:pre-wrap;font-size:.85rem}label{display:block}@media(max-width:750px){.previs-grid{grid-template-columns:1fr}}
</style>
