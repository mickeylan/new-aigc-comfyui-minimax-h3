import test from 'node:test'
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import {createPrevis,validatePrevis} from '../src/utils/scenePrevisualization.js'
import {upgradeTimeline,validateTimeline,sampleTimeline,applyPrevisPlan} from '../src/utils/previsTimeline.js'
const make=()=>upgradeTimeline(createPrevis(['甲']))
const frame=(t,x,yaw=0)=>({object_id:'character-1',time_ms:t,position:{x,y:0,z:0},yaw})
test('v1兼容、v2空轨道省略往返、拒绝坏引用重复帧超限',()=>{
 assert.equal(validatePrevis(createPrevis(['甲'])),'');const d=make();assert.equal(validatePrevis(d),'');delete d.routes;delete d.keyframes;delete d.camera_keyframes;assert.equal(validatePrevis(d),'')
 for(const change of [d=>d.duration_ms=999,d=>d.duration_ms=1.5,d=>d.keyframes=[frame(0,1),frame(0,2)],d=>d.keyframes=[{...frame(0,1),object_id:'unknown'}],d=>d.keyframes=[frame(10001,0)],d=>d.routes=[{id:'r',object_id:'character-1',a:{x:Infinity,y:0,z:0},b:{x:0,y:0,z:0}}],d=>d.camera_keyframes=[{time_ms:0,camera:d.camera,cut:'yes'}]]){const d=make();change(d);assert.notEqual(validateTimeline(d),'')}
})
test('线性位置、最短弧转向、首帧前静态、末帧保持，不改原稿',()=>{
 const d=make();d.keyframes=[frame(2000,10,-170),frame(1000,0,170)];const before=JSON.stringify(d)
 assert.equal(sampleTimeline(d,0).objects[0].position.x,-2)
 assert.equal(sampleTimeline(d,1500).objects[0].position.x,5);assert.equal(Math.abs(sampleTimeline(d,1500).objects[0].yaw),180)
 assert.equal(sampleTimeline(d,5000).objects[0].position.x,10);assert.equal(JSON.stringify(d),before)
})
test('相机插值和硬切，退化相机回退，不改变基础机位',()=>{
 const d=make(),a=structuredClone(d.camera),b=structuredClone(d.camera);b.position.x=17
 d.camera_keyframes=[{time_ms:0,camera:a,cut:false},{time_ms:1000,camera:b,cut:false}]
 assert.equal(sampleTimeline(d,500).camera.position.x,12)
 d.camera_keyframes[1].cut=true;assert.equal(sampleTimeline(d,999).camera.position.x,7);assert.equal(sampleTimeline(d,1000).camera.position.x,17)
 d.camera_keyframes[1]={time_ms:1000,camera:{position:a.target,target:a.position,fov:45},cut:false};assert.deepEqual(sampleTimeline(d,500).camera,a)
})
test('显式计划只执行所选操作，精确替换、过期与未知操作拒绝，无原地修改',()=>{
 const d=make(),before=JSON.stringify(d),p={base_hash:before,operations:[{kind:'keyframe',keyframe:frame(0,1)},{kind:'keyframe',keyframe:frame(1000,5)}]}
 const n=applyPrevisPlan(d,p,[1]);assert.equal(n.keyframes.length,1);assert.equal(n.keyframes[0].position.x,5);assert.equal(JSON.stringify(d),before)
 assert.throws(()=>applyPrevisPlan(n,p),/来源已变化/)
 assert.throws(()=>applyPrevisPlan(d,{base_hash:before,operations:[{kind:'delete_all'}]}),/不支持/)
 assert.throws(()=>applyPrevisPlan(d,p,[2]),/有效操作/)
 assert.throws(()=>applyPrevisPlan(d,p,[]),/有效操作/)
 const replaced=applyPrevisPlan(n,{base_hash:JSON.stringify(n),operations:[{kind:'keyframe',keyframe:frame(1000,9)}]});assert.equal(replaced.keyframes.length,1);assert.equal(replaced.keyframes[0].position.x,9)
})
test('播放循环按需启动并卸载取消，确认执行和版本失效，逐帧复用几何体',()=>{
 const ui=readFileSync(new URL('../src/components/PrevisTimeline.vue',import.meta.url),'utf8'),parent=readFileSync(new URL('../src/components/ScenePrevisualization.vue',import.meta.url),'utf8')
 assert.match(ui,/requestAnimationFrame\(tick\)/);assert.match(ui,/cancelAnimationFrame\(frame\)/);assert.match(ui,/onBeforeUnmount.*pause\(\)/)
 assert.match(ui,/base!==JSON.stringify\(props.document\)/);assert.match(ui,/window.confirm/);assert.match(ui,/if\(JSON.stringify\(props.document\)!==h.after\)/)
 assert.match(parent,/signature!==geometrySignature/);assert.match(parent,/pair.mesh.position.set/);assert.match(parent,/routes:doc.value.routes.filter/)
 assert.doesNotMatch(ui,/saveSceneBlocking|VideoFullPrompt|generateSceneVideo/)
})
