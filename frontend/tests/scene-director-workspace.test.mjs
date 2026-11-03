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

test('保存AI扩写后显式同步权威正文且旧本地草稿不能覆盖', () => {
  assert.match(detail, /ref="sceneShotDirector"/)
  assert.match(detail, /sceneShotDirector\.value\?\.syncAuthoritativeSceneContent\(payload\.content\)/)
  assert.match(detail, /applyAndOpenSceneDirector\(\)[\s\S]{0,200}saveSceneAndContinueDirector\(\)/)
  assert.match(director, /defineExpose\(\{ syncAuthoritativeSceneContent \}\)/)
  assert.match(director, /sceneContent: props\.sceneContent \|\| ''/)
  assert.match(director, /const sameSceneContent = String\(draft\.sceneContent \|\| ''\) === String\(props\.sceneContent \|\| ''\)/)
  assert.match(director, /directorDraft\.value = sameSceneContent/)
  assert.doesNotMatch(detail.slice(detail.indexOf('async function saveSceneAndContinueDirector'), detail.indexOf('async function workspaceGenerateImage')), /router\.push/)
})

test('修改故事或画面提示词保留现有分镜图并由用户决定是否重生成', () => {
  assert.match(detail, /现有分镜图已保留，请自行决定是否重新生成/)
  assert.doesNotMatch(detail, /画面提示词已修改，场景已重置，请重新生成画面/)
})

test('Scene卡片显示生产进度并支持同工作台处理下一Scene', () => {
  assert.match(detail, /sceneWorkflowStages\(sc\)/)
  assert.match(detail, /导演Shots/)
  assert.match(detail, /视频提示词/)
  assert.match(detail, /openNextSceneWorkspace/)
  assert.match(detail, /goToScenePage\(Math\.floor\(\(index \+ 1\) \/ scenePageSize\) \+ 1\)/)
  assert.match(detail, /返回第 \{\{ scenePage \}\} 页/)
})

test('仅正文保存不会更新参考图或套装，媒体选择实际变化才条件PUT', () => {
  const saveScene = detail.slice(detail.indexOf('async function saveScene('), detail.indexOf('function aspectLabel'))
  assert.match(saveScene, /referencesBaseline !== null && referencesSnapshot !== referencesBaseline[\s\S]*api\.updateSceneReferences/)
  assert.match(saveScene, /outfitsBaseline !== null && outfitsSnapshot !== outfitsBaseline[\s\S]*api\.updateSceneOutfits/)
  assert.doesNotMatch(saveScene, /await api\.updateScene[^\n]*\n\s*await api\.updateSceneReferences/)
  assert.match(detail, /sceneReferencesBaseline\.value = canonicalSceneReferences\(\[\.\.\.selectedSceneReferences\.value, \.\.\.invalidSceneReferences\.value\]\)/)
  assert.match(detail, /sceneOutfitsBaseline\.value = canonicalSceneOutfits\(selectedSceneOutfits\.value\)/)
})

test('媒体dirty比较规范化对象并保留加载失败和AI预览语义', () => {
  assert.match(detail, /function canonicalSceneReferences\(references\)/)
  assert.match(detail, /\.sort\(\(a, b\) => a\.character_id - b\.character_id \|\| a\.outfit_id - b\.outfit_id\)/)
  assert.match(detail, /sceneReferencesBaseline\.value = null[\s\S]*sceneOutfitsBaseline\.value = null[\s\S]*try \{/)
  assert.match(detail, /仅把当前选择作为 AI 预览上下文，不改动保存时用于判断用户修改的基线/)
  assert.match(detail, /if \(referencesBaseline !== null\) sceneReferencesBaseline\.value = referencesSnapshot/)
  assert.match(detail, /if \(outfitsBaseline !== null\) sceneOutfitsBaseline\.value = outfitsSnapshot/)
})
