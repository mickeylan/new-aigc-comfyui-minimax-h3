import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const read = (p) => readFileSync(new URL(p, import.meta.url), 'utf8')
const stage3d = read('../src/studio/scene3d/Scene3DStage.tsx')
const comp = read('../src/studio/scene3d/compositing.ts')

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
  assert.match(stage3d, /alphaTest/, '透明贴图要裁切，避免半透明排序把模型画到平面后面')
  assert.match(stage3d, /平面不透明度/, '缺少不透明度滑杆')
  assert.match(stage3d, /双面可见/, '缺少单双面开关')
})

test('天空盒用等距柱状全景图当背景与环境光', () => {
  assert.match(stage3d, /applySkybox/, '缺少天空盒入口')
  assert.match(stage3d, /EquirectangularReflectionMapping/, '全景图要按等距柱状采样')
  assert.match(stage3d, /scene\.background = texture/, '全景图应设为背景')
  assert.match(stage3d, /scene\.environment = texture/, '可选当作环境光')
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
  assert.match(stage3d, /不参与 GLB 导出|不进 GLB 与场景存档/, '要明确告诉用户视频不进导出')
})

test('视频平面的解码与纹理在删除和卸载时都被释放', () => {
  assert.match(stage3d, /entry\.video\.element\.pause\(\)/, '删除时要暂停视频')
  assert.match(stage3d, /entry\.video\.texture\.dispose\(\)/, '删除时要释放视频纹理')
  assert.match(stage3d, /URL\.revokeObjectURL\(src\)/, '本地导入的 blob URL 要回收')
  assert.match(stage3d, /skyboxTextureRef\.current\?\.dispose\(\)/, '卸载时要释放全景纹理')
})