import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
const view=readFileSync(new URL('../src/views/AdaptationPlan.vue',import.meta.url),'utf8')
const api=readFileSync(new URL('../src/api/index.js',import.meta.url),'utf8')
test('长篇改编提供故事弧和5至20集滚动批次',()=>{assert.match(view,/长篇滚动改编规划/);assert.match(view,/故事弧 \/ 分卷/);assert.match(view,/5–20/);assert.match(view,/createPlanningBatch/);assert.match(view,/generatePlanningBatch/);assert.match(view,/reviewPlanningBatch/)})
test('批次审核通过后接入Episode剧本与结构化编辑器',()=>{assert.match(view,/审核方案并应用Episode/);assert.match(view,/generateAdaptationScript/);assert.match(view,/episodes\/\$\{ep\.episode_n\}\/screenplay/);assert.match(api,/planningBatches:/);assert.match(view,/episodeScriptHint/);assert.match(view,/连续性复审/);assert.match(view,/AI补全角色\/场景\/道具/);assert.match(api,/generateAdaptationAssets:/);assert.match(view,/:disabled="busy \|\| ep\.status!=='approved'/);assert.match(view,/<button class="btn btn-sm" type="button" @click\.stop="review\(batch,'approve'\)">审核方案并应用Episode<\/button>/);assert.doesNotMatch(view,/:disabled="busy \|\| batch\.status!=='review'/);assert.doesNotMatch(view,/disabled[^>]*>审核方案并应用Episode/)})
test('滚动改编闭环包含状态快照伏笔连续性和故事弧维护',()=>{for(const text of ['批次结束状态快照','角色状态（修为/能力/所在地/道具）','伏笔状态','连续性审核并通过','新增故事弧','拆分','合并选中'])assert.ok(view.includes(text));for(const apiName of ['generateBatchSnapshot:','reviewBatchSnapshot:','storyClues:','createNovelArc:','splitNovelArc:','mergeNovelArcs:','updatePlanningTarget:'])assert.match(api,new RegExp(apiName))})
test('批次方案请求失败时始终显示可复制错误',()=>{assert.match(view,/generation-error-toast/);assert.match(view,/操作失败/);assert.match(view,/复制错误/);assert.match(view,/HTTP \$\{e\.response\.status\}/)})
