import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const read = (p) => readFileSync(new URL(p, import.meta.url), 'utf8')
const stage3d = read('../src/studio/scene3d/Scene3DStage.tsx')
const comp = read('../src/studio/scene3d/compositing.ts')
const plane = read('../src/studio/scene3d/planeDocument.ts')

// 3D 合成：图片/视频平面层当背景板或参考板，天空盒当全景背景，
// 平面按 z 缓冲与模型正确互相遮挡。
test('3D 编辑器能放图片与视频平面层', () => {
  assert.match(stage3d, /addPlane/, '缺少平面层入口')
  assert.match(stage3d, /图片平面/, '缺少图片平面按钮')
  assert.match(stage3d, /视频平面/, '缺少视频平面按钮')
  assert.match(stage3d, /new THREE\.PlaneGeometry\(width, height\)/, '应按素材比例建平面')
  assert.match(stage3d, /new THREE\.VideoTexture\(element\)/, '视频平面要用 VideoTexture')
  assert.match(stage3d, /imagePlaneInputRef/, '缺少图片平面文件输入')
  assert.match(stage3d, /videoPlaneInputRef/, '缺少视频平面文件输入')
})

test('平面层按宽高比定尺寸，不透明度和单双面可调', () => {
  assert.match(stage3d, /planeSize\(aspect\)/, '平面尺寸应按宽高比算')
  assert.match(stage3d, /applyPlaneSettings/, '缺少平面参数调整入口')
  assert.match(stage3d, /THREE\.DoubleSide/, '默认双面可见')
  assert.match(plane, /alphaTest: settings\.alphaTest/, '材质保留每层透明裁切参数')
  assert.match(plane, /transparent: true, depthWrite: false/, '软边混合不能被不透明模式或深度写入截断')
  assert.match(stage3d, /planeMaterial\(texture, normalizedSettings\)/, '创建必须复用统一材质实现')
  assert.match(stage3d, /平面不透明度/, '缺少不透明度滑杆')
  assert.match(stage3d, /双面可见/, '缺少单双面开关')
})

test('天空盒用等距柱状全景图当背景与环境光', () => {
  assert.match(stage3d, /applySkybox/, '缺少天空盒入口')
  assert.match(stage3d, /EquirectangularReflectionMapping/, '全景图要按等距柱状采样')
  assert.match(stage3d, /scene\.background = texture/, '全景图应设为背景')
  assert.match(stage3d, /scene\.environment = skybox\.asEnvironment \? texture : null/, '环境光关闭时必须清掉旧环境贴图')
  assert.match(stage3d, /backgroundIntensity/, '亮度可调')
  assert.match(stage3d, /clearSkybox/, '缺少清除入口')
  assert.match(stage3d, /导入天空盒/, '缺少天空盒按钮')
})

test('设了天空盒后切灯光预设不会把背景顶掉', () => {
  assert.match(
    stage3d,
    /if \(!skyboxTextureRef\.current\) scene\.background = new THREE\.Color\(preset\.background\)/,
    '切灯光预设时若已有天空盒，不应覆盖背景',
  )
  assert.match(stage3d, /scene\.background = new THREE\.Color\(LIGHT_PRESETS\[lightPreset\]\.background\)/, '清除天空盒要回到灯光预设底色')
})

test('视频平面不参与导出，并说明原因', () => {
  assert.match(stage3d, /isExportablePlane/, '导出要按媒体类型过滤')
  assert.match(comp, /isExportablePlane/, '缺少导出判定')
  assert.match(comp, /kind === "image"/, '只有图片平面可导出')
  assert.match(stage3d, /视频平面随场景存档保存；GLB格式不支持视频/, '要明确告诉用户视频不进导出')
})

test('视频平面的解码与纹理在删除和卸载时都被释放', () => {
  assert.match(stage3d, /entry\.video\.element\.pause\(\)/, '删除时要暂停视频')
  assert.match(stage3d, /entry\.video\.texture\.dispose\(\)/, '删除时要释放视频纹理')
  assert.match(stage3d, /URL\.revokeObjectURL\(src\)/, '本地导入的 blob URL 要回收')
  assert.match(stage3d, /skyboxTextureRef\.current\?\.dispose\(\)/, '卸载时要释放全景纹理')
})