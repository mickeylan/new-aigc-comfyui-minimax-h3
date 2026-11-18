import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { clonePrevis, createPrevis, previsSuggestion, validatePrevis } from '../src/utils/scenePrevisualization.js'
import { compareGenerationSnapshots } from '../src/utils/generationSnapshot.js'

test('3D默认草稿去重、限制对象数、JSON往返，建议不写回原稿',()=>{
  const d=createPrevis(['甲','甲','乙'])
  assert.equal(validatePrevis(d),'');assert.equal(d.objects.length,2)
  assert.equal(createPrevis(Array.from({length:40},(_,i)=>String(i))).objects.length,32)
  const baseline=JSON.stringify(d),suggestion=previsSuggestion(d)
  assert.equal(JSON.stringify(d),baseline);assert.equal(suggestion.characters.length,2)
  assert.match(suggestion.suggestions[0],/建议，非原文事实/)
  const copy=clonePrevis(d);copy.objects[0].position.x=20;assert.notEqual(copy.objects[0].position.x,d.objects[0].position.x)
})
test('3D拒绝非有限坐标、非法尺寸、同点机位、重复对象和机位',()=>{
  const cases=[d=>{d.objects[0].position.x=Infinity},d=>{d.objects[0].yaw=NaN},d=>{d.camera.fov=14},d=>{d.camera.position={...d.camera.target}},d=>{d.objects[0].width=0},d=>{d.objects.push({...d.objects[0]})},d=>{d.views=[{name:'A',camera:clonePrevis(d.camera)},{name:'A',camera:clonePrevis(d.camera)}]}]
  for(const change of cases){const d=createPrevis(['甲']);change(d);assert.notEqual(validatePrevis(d),'')}
})
test('快照保留重复位置、报告换序与来源版本，不修改旧快照',()=>{
  const make=refs=>({refs_json:JSON.stringify(refs)})
  const a={role:'ref_images',file:'1/a.png',sha256:'a'},b={role:'ref_images',file:'1/b.png',sha256:'b'}
  assert.deepEqual(compareGenerationSnapshots(make([a,a]),make([a,a])),[])
  assert.equal(compareGenerationSnapshots(make([a]),make([a,a])).filter(v=>v.startsWith('移除参考')).length,1)
  assert.equal(compareGenerationSnapshots(make([b,a]),make([a,b])).filter(v=>v.startsWith('参考图顺序已变')).length,2)
  assert.equal(compareGenerationSnapshots(make([{...a,source_type:'asset',source_id:2}]),make([{...a,source_type:'asset',source_id:1}])).filter(v=>v.startsWith('参考图来源版本已变')).length,1)
})
test('预演独立懒加载、CAS保存、显式采用、PNG仅下载并释放GPU资源',()=>{
  const component=readFileSync(new URL('../src/components/ScenePrevisualization.vue',import.meta.url),'utf8')
  const detail=readFileSync(new URL('../src/views/ProjectDetail.vue',import.meta.url),'utf8')
  assert.match(component,/await import\('three'\)/);assert.doesNotMatch(component,/three\/examples|requestAnimationFrame/)
  assert.match(component,/source_content_hash:sourceHash/);assert.match(component,/revision,/)
  assert.match(component,/emit\('adopt'/);assert.match(component,/toBlob/);assert.match(component,/forceContextLoss/);assert.match(component,/resizeObserver\?\.disconnect/)
  assert.doesNotMatch(component,/generateScene|uploadFile|saveSceneBlocking|VideoFullPrompt|BlockingJSON/)
  assert.match(detail,/defineAsyncComponent\(\(\) => import\('\.\.\/components\/ScenePrevisualization.vue'\)\)/)
  const adopt=detail.slice(detail.indexOf('function adoptScenePrevis'),detail.indexOf('const blockingDraft'))
  assert.match(adopt,/window.confirm/);assert.match(adopt,/blockingDraft\.suggestionsText/);assert.doesNotMatch(adopt,/api\./)
})
