import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const director = readFileSync(new URL('../src/components/ShotDirectorEditor.vue', import.meta.url), 'utf8')

test('Scene导演流程在项目详情内连续完成而不跳转剪辑台', () => {
  assert.match(detail, /SCENE DIRECTOR WORKSPACE/)
  assert.match(detail, /1\. 场景扩写/)
  assert.match(detail, /2\. AI导演拆镜/)
  assert.match(detail, /3\. 参考图与生成/)
  assert.match(detail, /<ShotDirectorEditor/)
  assert.match(detail, /保存并继续AI导演拆镜/)
  assert.match(detail, /准备并审核视频提示词/)
  assert.doesNotMatch(detail, /applyAndOpenSceneDirector[\s\S]{0,300}router\.push/)
})

test('Scene工作台将分页、选中Scene和步骤写入URL并可刷新恢复', () => {
  assert.match(detail, /scene_page: String\(scenePage\.value\)/)
  assert.match(detail, /panel: 'director'/)
  assert.match(detail, /step: sceneWorkspaceStep\.value/)
  assert.match(detail, /route\.query\.panel === 'director'/)
  assert.match(detail, /requestedSceneID/)
  assert.match(detail, /openEditScene\(scene, String\(route\.query\.step \|\| 'scene'\)\)/)
  assert.match(detail, /const initialScenePage =/)
})

test('保存AI扩写后同一Scene的导演概况同步新正文且不覆盖人工草稿', () => {
  assert.match(director, /watch\(\(\) => props\.sceneContent/)
  assert.match(director, /current === previous/)
  assert.match(director, /!directorDraft\.value/)
  assert.match(director, /directorBrief\.value = String\(nextContent \|\| ''\)/)
  assert.doesNotMatch(detail.slice(detail.indexOf('async function saveSceneAndContinueDirector'), detail.indexOf('async function workspaceGenerateImage')), /router\.push/)
})

test('Scene卡片显示生产进度并支持同工作台处理下一Scene', () => {
  assert.match(detail, /sceneWorkflowStages\(sc\)/)
  assert.match(detail, /导演Shots/)
  assert.match(detail, /视频提示词/)
  assert.match(detail, /openNextSceneWorkspace/)
  assert.match(detail, /goToScenePage\(Math\.floor\(\(index \+ 1\) \/ scenePageSize\) \+ 1\)/)
  assert.match(detail, /返回第 \{\{ scenePage \}\} 页/)
})
