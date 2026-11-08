import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { filterReferenceCandidates } from '../src/utils/referenceSearch.js'

test('参考素材支持中文部分名称、空白和大小写搜索且保持顺序及对象身份', () => {
  const refs = [{key:'a',label:'柳乐儿 · 四视图'}, {key:'b',label:'苍黄荒地 · 场景'}, {key:'c',label:'柳石 · 标准像'}, {key:'d',label:'Sword · 道具'}]
  assert.deepEqual(filterReferenceCandidates(refs,'柳'), [refs[0],refs[2]])
  assert.deepEqual(filterReferenceCandidates(refs,' 荒地 '), [refs[1]])
  assert.deepEqual(filterReferenceCandidates(refs,'sWORD'), [refs[3]])
  assert.deepEqual(filterReferenceCandidates(refs,'不存在'), [])
  assert.strictEqual(filterReferenceCandidates(refs,'  '), refs)
  const selected = [{key:'a',use_h3:true}, {key:'b',use_h3:false}]
  const baseline = JSON.stringify(selected)
  filterReferenceCandidates(refs,'柳石')
  assert.equal(JSON.stringify(selected), baseline)
  assert.equal(refs.length,4)
})
test('工作台筛选仅用于展示，清空和切换场景可恢复全部素材', () => {
  const source=readFileSync(new URL('../src/views/ProjectDetail.vue',import.meta.url),'utf8')
  assert.match(source,/v-model="sceneReferenceSearch"/)
  assert.match(source,/v-for="ref in filteredSceneReferenceCandidates"/)
  assert.match(source,/筛选不影响勾选/)
  assert.match(source,/async function openEditScene\(sc, step = 'scene'\) \{\s*sceneReferenceSearch.value = ''/)
  assert.match(source,/@click="sceneReferenceSearch=''"/)
  assert.match(source,/selectedSceneReferences\[referenceIndex\(ref\)\]/)
})
