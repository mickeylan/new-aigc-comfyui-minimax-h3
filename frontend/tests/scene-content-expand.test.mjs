import assert from 'node:assert/strict'
import fs from 'node:fs'

const view = fs.readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const api = fs.readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

assert.match(api, /expandSceneContent:.*content\/expand.*timeout: 300000/)
assert.match(view, /AI导演扩写/)
assert.match(view, /content: sceneForm\.content, requirements: sceneExpandRequirements\.value/)
assert.match(view, /sceneExpandPreview\.value = \{ content: data\.content/)
assert.match(view, /sceneForm\.content = sceneExpandPreview\.value\.content/)
assert.match(view, /尚未保存|未保存正文/)
assert.match(view, /已有结构化 Shots/)
assert.match(view, /e\.response\?\.data\?\.error \|\| 'AI 导演扩写失败'/)
assert.match(view, /expandingSceneContent\.value = false/)
assert.doesNotMatch(view, /expandSceneContent[\s\S]{0,900}updateScene\(/)
assert.match(view, /应用、保存并继续镜头设计/)
assert.match(view, /if \(await saveScene\(false\)\) setSceneWorkspaceStep\('director'\)/)
assert.doesNotMatch(view, /applyAndOpenSceneDirector[\s\S]{0,300}router\.push/)
assert.match(view, /editingScene\.value\?\.id !== sceneId/)
assert.match(view, /sceneForm\.content !== sceneExpandPreview\.value\.sourceContent/)
console.log('scene content expand assertions passed')
