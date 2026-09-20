import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
const view=readFileSync(new URL('../src/views/AdaptationPlan.vue',import.meta.url),'utf8')
const api=readFileSync(new URL('../src/api/index.js',import.meta.url),'utf8')
test('长篇改编提供故事弧和5至20集滚动批次',()=>{assert.match(view,/长篇滚动改编规划/);assert.match(view,/故事弧 \/ 分卷/);assert.match(view,/5–20/);assert.match(view,/createPlanningBatch/);assert.match(view,/generatePlanningBatch/);assert.match(view,/reviewPlanningBatch/)})
test('批次审核通过后接入Episode剧本与结构化编辑器',()=>{assert.match(view,/审核通过并应用Episode/);assert.match(view,/generateAdaptationScript/);assert.match(view,/episodes\/\$\{ep\.episode_n\}\/screenplay/);assert.match(api,/planningBatches:/)})
