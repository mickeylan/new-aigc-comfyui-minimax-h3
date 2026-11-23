import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const read = (p) => readFileSync(new URL(p, import.meta.url), 'utf8')
const materials = read('../src/views/Materials.vue')
const stage3d = read('../src/studio/scene3d/Scene3DStage.tsx')
const previsPanel = read('../src/components/PrevisEnvironmentPanel.vue')
const previs = read('../src/components/ScenePrevisualization.vue')

// 3D 拆成两个职责后，资产工厂（3D 编辑器）产出 type=3d 素材，调度预演从素材库取模型。
test('素材库支持 3D 分组：筛选、缩略图与来源标签', () => {
  assert.match(materials, /setType\('3d'\)/, '缺少 3D 筛选')
  assert.match(materials, /viewer\.thumb_path \|\| viewer\.path/, '3D 素材应优先显示缩略图')
  assert.match(materials, /studio_3d: '3D 编辑器'/, '缺少 3D 编辑器来源标签')
  assert.match(materials, /\.\(glb\|gltf\|obj\|fbx\|ply\|splat\|spz\|ksplat\)\$/, '上传时应识别 3D 扩展名')
})

test('3D 编辑器存入素材库带模型与预览图，类型固定 3d', () => {
  assert.match(stage3d, /saveToLibrary/, '缺少存入素材库入口')
  assert.match(stage3d, /form\.append\("type", "3d"\)/, '素材类型必须是 3d')
  assert.match(stage3d, /form\.append\("thumbnail"/, '必须带客户端截的预览图')
  assert.match(stage3d, /await captureThumbnail\(\)/, '预览图必须走等待首帧完成的缩略图助手')
  assert.match(stage3d, /\/api\/materials/, '应提交到素材库接口')
})

test('调度预演可以从素材库选 GLB 绑定到当前对象', () => {
  assert.match(previsPanel, /models3d/, '面板需要素材库 3D 列表')
  assert.match(previsPanel, /importFromLibrary/, '缺少素材库导入动作')
  assert.match(previsPanel, /emit\('import-model'/, '应复用同一条 GLB 导入流程')
  assert.match(previs, /api\.materials\(\{type:'3d'\}\)/, '预演应拉取 3D 素材列表')
  assert.match(previs, /:models-3d="libraryModels"/, '应把列表传给面板')
})

test('素材库里的高斯泼溅拖进场景走 Spark 解码，不走 GLTFLoader', () => {
  assert.match(stage3d, /if \(kind === "splat"\) \{/, '应单独分派 splat 分支')
  assert.match(stage3d, /await loadSplatUrl\(url, asset\.name, dropPoint\)/, '.spz 应交给 Spark 加载')
  assert.match(stage3d, /\[addObject, loadSplatUrl\]/, '回调依赖里要带上 loadSplatUrl')
})

// 没有缩略图的素材（或缩略图加载失败）不能留一个空白 <img>
test('素材库面板给缺缩略图的素材一个占位块', () => {
  const panel = read('../src/studio/scene3d/AssetLibraryPanel.tsx')
  assert.match(panel, /function AssetThumbnail\(/, '缺少缩略图组件')
  assert.match(panel, /useState\(!asset\.thumb_path\)/, '没缩略图时应直接进占位状态')
  assert.match(panel, /onError=\{\(\) => setFailed\(true\)\}/, '加载失败也要退回占位块')
  assert.match(panel, /<AssetThumbnail asset=\{asset\} \/>/, '列表行应使用缩略图组件')
})