import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
const read = path => readFileSync(new URL(path,import.meta.url),'utf8')
test('delivery timeline integrates with canonical merge sequence and preview',()=>{
 const source=read('../src/views/ProjectEditor.vue')
 assert.match(source,/<FinalCutTimeline[^>]+:clips="mergeSequence"/)
 assert.match(source,/@seek="seekFinalCut"/)
 assert.match(source,/timelineSeek=Number\(clip.source_start/)
 assert.match(source,/if\(timelineSeek!=null\)/)
 assert.match(source,/if\(finalCutDirty.value&&!window.confirm/)
})
test('server draft restoration is explicit and save carries CAS revision',()=>{
 const source=read('../src/views/ProjectEditor.vue'),api=read('../src/api/index.js')
 assert.match(source,/api.saveEditingDraft\(project,episode,revision,clips\)/)
 assert.match(source,/if\(restoreFinalCutDraft\([\s\S]*serverDraftRevisions\[key\]=data.draft.revision/)
 assert.match(source,/保存到服务端/)
 assert.match(api,/editing-draft.*\{revision, edit_sequence\}/)
})
