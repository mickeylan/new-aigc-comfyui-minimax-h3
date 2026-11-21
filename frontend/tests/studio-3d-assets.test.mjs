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
  assert.match(materials, /\.\(glb\|gltf\|obj\|fbx\|ply\|splat\)\$/, '上传时应识别 3D 扩展名')
})

test('3D 编辑器存入素材库带模型与预览图，类型固定 3d', () => {
  assert.match(stage3d, /saveToLibrary/, '缺少存入素材库入口')
  assert.match(stage3d, /form\.append\("type", "3d"\)/, '素材类型必须是 3d')
  assert.match(stage3d, /form\.append\("thumbnail"/, '必须带客户端截的预览图')
  assert.match(stage3d, /domElement\.toDataURL\("image\/png"\)/, '预览图取当前渲染帧')
  assert.match(stage3d, /\/api\/materials/, '应提交到素材库接口')
})

test('调度预演可以从素材库选 GLB 绑定到当前对象', () => {
  assert.match(previsPanel, /models3d/, '面板需要素材库 3D 列表')
  assert.match(previsPanel, /importFromLibrary/, '缺少素材库导入动作')
  assert.match(previsPanel, /emit\('import-model'/, '应复用同一条 GLB 导入流程')
  assert.match(previs, /api\.materials\(\{type:'3d'\}\)/, '预演应拉取 3D 素材列表')
  assert.match(previs, /:models-3d="libraryModels"/, '应把列表传给面板')
})