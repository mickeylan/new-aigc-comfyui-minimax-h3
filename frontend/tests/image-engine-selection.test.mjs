import test from 'node:test'
import assert from 'node:assert/strict'
import{readFileSync}from'node:fs'
const detail=readFileSync(new URL('../src/views/ProjectDetail.vue',import.meta.url),'utf8')
test('资产默认Krea2且可读文字优先Qwen模板',()=>{assert.match(detail,/image_engine: 'krea2'/);assert.match(detail,/画面内可读文字/);assert.match(detail,/需要清晰中文时推荐 Qwen-Image-2.1/);assert.match(detail,/assetForm\.image_engine = 'qwen_image_2_1'/);assert.match(detail,/qwenAssetTemplateReady/)})
test('剧情场景图由用户选择MiniMax H3或Qwen且模板缺失时禁用',()=>{assert.match(detail,/场景图生成模板/);assert.match(detail,/MiniMax H3 SelfLift（当前默认）/);assert.match(detail,/qwenSceneTemplateReady/);assert.match(detail,/image_engine: sceneForm\.image_engine/);assert.match(detail,/所选 Qwen-Image-2.1 工作流模板未安装/)})
