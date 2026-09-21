import test from 'node:test'
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
const view=readFileSync(new URL('../src/views/QwenImagePrompts.vue',import.meta.url),'utf8')
const api=readFileSync(new URL('../src/api/index.js',import.meta.url),'utf8')
const router=readFileSync(new URL('../src/router/index.js',import.meta.url),'utf8')
const app=readFileSync(new URL('../src/App.vue',import.meta.url),'utf8')
test('Qwen Image 2.1 提示词程序独立于现有生成链路',()=>{assert.match(view,/当前只输出提示词，不会提交任务，也不会改变 Krea2 或 MiniMax H3 流程/);assert.match(api,/qwenImagePromptPrograms/);assert.match(api,/generateQwenImagePrompt/);assert.match(router,/qwen-image-prompts/);assert.match(app,/Qwen 提示词/)})
test('多图编辑提示词保持有序 image 标签并限制十张',()=>{assert.match(view,/&lt;image1&gt;/);assert.match(view,/form\.references\.length>=10/);assert.match(view,/顺序将固定映射/);assert.match(view,/ratio_follow/)})
