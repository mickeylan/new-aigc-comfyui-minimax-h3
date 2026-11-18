import test from 'node:test'
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import {createPrevis,previsSuggestion,mergePrevisBlocking} from '../src/utils/scenePrevisualization.js'

test('3D确定性朝向区别互动对象、身体朝向、镜头正侧背面，不推测视线',()=>{
 const d=createPrevis(['甲','乙']);d.objects[0].position={x:0,y:0,z:0};d.objects[1].position={x:0,y:0,z:3};d.camera.position={x:0,y:2,z:8}
 let s=previsSuggestion(d)
 assert.match(s.characters[0].facing,/朝向乙所在方向/);assert.match(s.characters[0].facing,/相对镜头正面/);assert.equal(s.characters[0].gaze,'')
 d.objects[0].yaw=180;assert.match(previsSuggestion(d).characters[0].facing,/相对镜头背面/)
 d.objects[0].yaw=90;assert.match(previsSuggestion(d).characters[0].facing,/相对镜头侧面/)
 d.camera.position={x:0,y:10,z:0};assert.match(previsSuggestion(d).characters[0].facing,/不能判定正背面/)
})
test('3D合并只改同名人物几何位置及朝向，保留事实、视线、关系与起止，不改输入',()=>{
 const original={facts:['原文事实'],suggestions:['人工建议'],characters:[{name:'甲',position:'旧',facing:'旧',gaze:'看乙',visibility:'无遮挡'},{name:'不在预演的人',position:'门边'}],relations:[{kind:'motion',trajectory:'原文路径'}],start:'起始',end:'结束',summary:'原摘要'}
 const before=JSON.stringify(original),suggestion=previsSuggestion(createPrevis(['甲','乙']))
 const result=mergePrevisBlocking(original,suggestion)
 assert.equal(JSON.stringify(original),before)
 assert.deepEqual(result.facts,original.facts);assert.deepEqual(result.relations,original.relations)
 assert.equal(result.characters[0].gaze,'看乙');assert.equal(result.characters[0].visibility,'无遮挡');assert.equal(result.characters[1].position,'门边')
 assert.equal(result.start,'起始');assert.equal(result.end,'结束');assert.equal(result.summary,'原摘要');assert.equal(result.characters[2].name,'乙')
 assert.deepEqual(mergePrevisBlocking(result,suggestion).suggestions,result.suggestions)
})
test('3D直达草稿入口懒加载、来源绑定、显式确认，过期响应与编辑失效受保护',()=>{
 const panel=readFileSync(new URL('../src/components/ScenePrevisualization.vue',import.meta.url),'utf8')
 const review=readFileSync(new URL('../src/components/SceneBlockingPromptDraft.vue',import.meta.url),'utf8')
 const page=readFileSync(new URL('../src/views/ProjectDetail.vue',import.meta.url),'utf8')
 assert.match(panel,/生成分镜图提示词草稿/);assert.match(panel,/生成视频提示词草稿/)
 assert.match(panel,/watch\(doc,.*invalidate-prompt/)
 assert.match(page,/mergePrevisBlocking\(blockingPayload\(\),suggestion\)/)
 assert.match(page,/watch\(blockingDraft,.*previsPromptRequest.value=null/)
 assert.match(review,/source_version:preview.source_version/);assert.match(review,/blocking_hash:preview.blocking_hash/)
 assert.match(review,/window.confirm/);assert.match(review,/confirm_replace:true/)
 assert.match(review,/if\(!alive\|\|token!==epoch\)return/)
 assert.match(review,/status===409\)ready.value=false/)
 assert.match(review,/onBeforeUnmount\(\(\)=>\{alive=false;epoch\+\+\}\)/)
})
