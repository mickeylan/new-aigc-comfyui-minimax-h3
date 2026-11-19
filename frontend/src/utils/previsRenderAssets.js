import {inspectGLB,localModelStore,weatherParticles,clipTime} from './previsEnvironment.js'
export function disposeTree(root){const geometries=new Set(),materials=new Set(),textures=new Set();root.traverse(o=>{if(o.geometry)geometries.add(o.geometry);for(const m of (Array.isArray(o.material)?o.material:[o.material]))if(m){materials.add(m);for(const value of Object.values(m))if(value?.isTexture)textures.add(value)}if(o.isSkinnedMesh)o.skeleton?.dispose()});textures.forEach(t=>t.dispose());materials.forEach(m=>m.dispose());geometries.forEach(g=>g.dispose())}
export function createAssetRuntime(T,scene){
 const root=new T.Group();scene.add(root);const models=new Map(),loading=new Map(),attempted=new Map(),versions=new Map();let alive=true
 const weatherGroup=new T.Group();scene.add(weatherGroup)
 const light=new T.DirectionalLight(0xffffff,2);light.position.set(8,15,10);scene.add(light)
 const floor=new T.Mesh(new T.PlaneGeometry(200,200),new T.MeshStandardMaterial({color:0x829080,roughness:1}));floor.rotation.x=-Math.PI/2;floor.position.y=-.02;scene.add(floor)
 const particles=new T.Points(new T.BufferGeometry(),new T.PointsMaterial({color:0xd8e7ff,size:.07,transparent:true,opacity:.8}));weatherGroup.add(particles)
 async function load(binding,buffer){
  const version=(versions.get(binding.object_id)||0)+1;versions.set(binding.object_id,version)
  inspectGLB(buffer)
  const {GLTFLoader}=await import('three/addons/loaders/GLTFLoader.js')
  const manager=new T.LoadingManager();manager.setURLModifier(url=>{if(!url.startsWith('blob:'))throw Error('禁止GLB外部资源请求');return url})
  const gltf=await new GLTFLoader(manager).parseAsync(buffer.slice(0),'')
  if(!alive||versions.get(binding.object_id)!==version){disposeTree(gltf.scene);return null}
  let skin=false,vertices=0,pixels=0;const textures=new Set();gltf.scene.traverse(o=>{if(o.isSkinnedMesh)skin=true;vertices+=o.geometry?.attributes.position?.count||0;for(const m of(Array.isArray(o.material)?o.material:[o.material]))if(m)for(const v of Object.values(m))if(v?.isTexture)textures.add(v)});for(const texture of textures){const image=texture.image;if(image?.width>4096||image?.height>4096)pixels=Infinity;else pixels+=(image?.width||0)*(image?.height||0)}
  if(pixels>32*1024*1024){disposeTree(gltf.scene);throw Error('纹理单边不得超过4096，总像素不得超过32Mi像素')}
  if(vertices>500000){disposeTree(gltf.scene);throw Error('模型超过50万顶点限制')}
  const wrapper=new T.Group(),normalized=new T.Group();normalized.add(gltf.scene);wrapper.add(normalized)
  gltf.scene.updateMatrixWorld(true);const box=new T.Box3().setFromObject(gltf.scene),size=box.getSize(new T.Vector3())
  if(!Number.isFinite(size.y)||size.y<.001){disposeTree(gltf.scene);throw Error('模型尺寸无效')}
  normalized.scale.setScalar(1/size.y);normalized.position.set(-(box.min.x+box.max.x)/2/size.y,-box.min.y/size.y,-(box.min.z+box.max.z)/2/size.y)
  wrapper.traverse(o=>{o.userData.objectID=binding.object_id})
  const old=models.get(binding.object_id);if(old){root.remove(old.wrapper);old.mixer.stopAllAction();old.mixer.uncacheRoot(old.gltf.scene);disposeTree(old.wrapper)}
  const item={hash:binding.asset_hash,wrapper,normalized,gltf,mixer:new T.AnimationMixer(gltf.scene),skin};models.set(binding.object_id,item);root.add(wrapper);return item
 }
 async function ensure(bindings,onResult){for(const b of bindings){if(models.get(b.object_id)?.hash===b.asset_hash||loading.has(b.object_id)||attempted.get(b.object_id)===b.asset_hash)continue;loading.set(b.object_id,b.asset_hash);attempted.set(b.object_id,b.asset_hash);try{const buffer=await localModelStore(b.asset_hash);if(!buffer)throw Error('本浏览器缺少模型，请重新导入同一GLB');const digest=Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',buffer)),v=>v.toString(16).padStart(2,'0')).join('');if(digest!==b.asset_hash)throw Error('本地GLB内容与绑定SHA256不符，请重新导入');const item=await load(b,buffer);if(alive&&item)onResult(b.object_id,item,null)}catch(e){if(alive)onResult(b.object_id,null,e)}finally{loading.delete(b.object_id)}}}
 function update(doc,timeMS){const bound=new Set((doc.models||[]).map(b=>b.object_id));for(const [id,item] of models){if(!bound.has(id)){root.remove(item.wrapper);item.mixer.stopAllAction();item.mixer.uncacheRoot(item.gltf.scene);disposeTree(item.wrapper);models.delete(id)}}
 for(const b of doc.models||[]){const item=models.get(b.object_id),o=doc.objects.find(o=>o.id===b.object_id);if(!item||!o)continue;item.wrapper.visible=item.hash===b.asset_hash;if(!item.wrapper.visible)continue;item.wrapper.position.set(o.position.x,o.position.y,o.position.z);item.wrapper.rotation.y=o.yaw*Math.PI/180;item.wrapper.scale.setScalar(o.height)
 item.mixer.stopAllAction();const clip=item.gltf.animations[b.clip_index],t=clip?clipTime(b,timeMS,clip.duration):null;if(clip&&t!==null){const action=item.mixer.clipAction(clip);action.reset().setLoop(T.LoopOnce,1);action.clampWhenFinished=true;action.play();item.mixer.setTime(t)}else item.mixer.update(0)
 }
 const w=doc.weather||{preset:'clear',intensity:.5,seed:1};scene.background=new T.Color(w.preset==='clear'?0x98b8d0:w.preset==='snow'?0xdce4eb:0x77828e);scene.fog=w.preset==='fog'?new T.FogExp2(scene.background,w.fog_density??0.025):null;const sun=(w.sun_elevation??45)*Math.PI/180;light.position.set(15*Math.cos(sun),15*Math.sin(sun),0);light.intensity=w.preset==='clear'?2:0.6
 particles.visible=['rain','snow'].includes(w.preset);if(particles.visible){const data=weatherParticles(w,timeMS,Math.round(800*w.intensity));const previous=particles.geometry.attributes.position;if(previous&&previous.array.length===data.length){previous.array.set(data);previous.needsUpdate=true}else particles.geometry.setAttribute('position',new T.BufferAttribute(data,3));particles.material.size=w.preset==='snow'?.12:.04;particles.geometry.computeBoundingSphere()}
 }
 function dispose(){alive=false;for(const i of models.values()){i.mixer.stopAllAction();i.mixer.uncacheRoot(i.gltf.scene)}disposeTree(root);disposeTree(weatherGroup);disposeTree(floor);scene.remove(root,weatherGroup,light,floor);models.clear()}
 return{root,weatherGroup,floor,models,load,ensure,update,dispose}
}
export function exportPrevisFrame(T,renderer,scene,camera,geometryGroup,assets,mode,depthRange){return renderExportPass(T,renderer,scene,camera,geometryGroup,assets,mode,depthRange,canvas=>new Promise((resolve,reject)=>canvas.toBlob(b=>b?resolve(b):reject(Error('PNG导出失败')),'image/png')))}
// Renders one clean 1280×720 white/depth pass, lets capture read the canvas, then restores the editor view.
export async function renderExportPass(T,renderer,scene,camera,geometryGroup,assets,mode,depthRange={near:0.1,far:50},capture){
 if(!Number.isFinite(depthRange.near)||!Number.isFinite(depthRange.far)||depthRange.near<0||depthRange.far>500||depthRange.far-depthRange.near<0.1)throw Error('深度范围须满足0≤近<远≤500米，间距至少0.1米')
 const hidden=[],old={background:scene.background,fog:scene.fog,override:scene.overrideMaterial,pixelRatio:renderer.getPixelRatio(),size:renderer.getSize(new T.Vector2()),aspect:camera.aspect}
 const hide=o=>{hidden.push([o,o.visible]);o.visible=false}
 for(const child of geometryGroup.children){if(!child.userData.objectID)hide(child)}
 hide(assets.weatherGroup)
 const material=mode==='depth'?new T.MeshDepthMaterial({depthPacking:T.BasicDepthPacking}):new T.MeshStandardMaterial({color:0xeeeeee,roughness:1,metalness:0})
 if(mode==='depth')material.onBeforeCompile=shader=>{shader.uniforms.exportNear={value:camera.near};shader.uniforms.exportFar={value:camera.far};shader.uniforms.rangeNear={value:depthRange.near};shader.uniforms.rangeFar={value:depthRange.far};shader.fragmentShader='uniform float exportNear; uniform float exportFar; uniform float rangeNear; uniform float rangeFar;\n'+shader.fragmentShader.replace('gl_FragColor = vec4( vec3( 1.0 - fragCoordZ ), opacity );','float distanceToCamera = -perspectiveDepthToViewZ(fragCoordZ, exportNear, exportFar); float gray = 1.0 - clamp((distanceToCamera-rangeNear)/(rangeFar-rangeNear),0.0,1.0); gl_FragColor = vec4(vec3(gray),1.0);')}
 try{scene.fog=null;scene.background=new T.Color(mode==='depth'?0x000000:0xaeb4bc);scene.overrideMaterial=material;renderer.setPixelRatio(1);renderer.setSize(1280,720,false);camera.aspect=1280/720;camera.updateProjectionMatrix();renderer.render(scene,camera);return await capture(renderer.domElement)}
 finally{for(const [o,v]of hidden)o.visible=v;scene.background=old.background;scene.fog=old.fog;scene.overrideMaterial=old.override;renderer.setPixelRatio(old.pixelRatio);renderer.setSize(old.size.x,old.size.y,false);camera.aspect=old.aspect;camera.updateProjectionMatrix();material.dispose();renderer.render(scene,camera)}
}
