import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const read = (p) => readFileSync(new URL(p, import.meta.url), 'utf8')
const stage3d = read('../src/studio/scene3d/Scene3DStage.tsx')
const panel = read('../src/studio/scene3d/AssetLibraryPanel.tsx')
const lib = read('../src/studio/scene3d/assetLibrary.ts')

// Kitbashing：资产库里的模型要能拖进 3D 场景，落在鼠标指到的地面位置，
// 而不是只能从本地文件选择器一个个导入。
test('3D 编辑器内嵌资产库面板，读素材库的 3d 分组', () => {
  assert.match(stage3d, /AssetLibraryPanel/, '缺少资产库面板')
  assert.match(stage3d, /资产库（拖进画面摆位）/, '缺少资产库入口文案')
  assert.match(panel, /\/api\/materials\?type=3d/, '应只拉 3D 素材')
  assert.match(panel, /max-h-56[\s\S]{0,80}overflow-y-auto/, '资产多时可滚动')
})

test('拖放用编辑器私有 MIME，不抢占浏览器拖文件', () => {
  assert.match(lib, /application\/x-studio-3d-asset/)
  assert.match(panel, /setData\(ASSET_DRAG_MIME/, '拖起时写入资产数据')
  assert.match(stage3d, /dataTransfer\.types\.includes\(ASSET_DRAG_MIME\)/, '只接管本面板的拖放')
  assert.match(stage3d, /getData\(ASSET_DRAG_MIME\)/, '落下时读资产数据')
})

test('落点投到地面 y=0，资产摆在鼠标位置而不是永远堆原点', () => {
  assert.match(stage3d, /dropPointFromEvent/, '缺少落点计算')
  assert.match(stage3d, /intersectPlane/, '应把屏幕点投到地面平面')
  assert.match(stage3d, /new THREE\.Plane\(new THREE\.Vector3\(0, 1, 0\), 0\)/, '地面平面是 y=0')
  assert.match(stage3d, /group\.position\.set\(dropPoint\.x, 0, dropPoint\.z\)/, '应按落点移动对象')
  assert.match(stage3d, /已放入 \$\{asset\.name\}（原点）/, '点按钮放入时要说明落在原点')
})

test('素材库资产按格式走对应加载器，不支持的格式显式提示', () => {
  assert.match(stage3d, /loadLibraryAsset/, '缺少资产加载入口')
  assert.match(stage3d, /assetKind\(asset\.path \|\| asset\.name\)/, '按扩展名判格式')
  assert.match(stage3d, /new OBJLoader\(\)\.parse/, 'obj 走 OBJLoader')
  assert.match(stage3d, /new GLTFLoader\(\)\.loadAsync\(url\)/, 'glb/gltf 走 GLTFLoader')
  assert.match(stage3d, /暂不支持这个格式，请先转成 GLB/, '不支持的格式要说明原因')
})

test('资产库面板对不支持的格式禁用拖拽并标出需转 GLB', () => {
  assert.match(panel, /isSupportedAsset/, '面板要按格式区分可拖性')
  assert.match(panel, /draggable=\{supported\}/, '不支持的格式不能拖')
  assert.match(panel, /需转 GLB/, '要给出转换提示')
  assert.match(panel, /缩略图|thumbnailUrl/, '列表应显示缩略图')
  assert.match(panel, /刷新/, '缺少刷新入口')
  assert.match(panel, /素材库里还没有 3D 模型/, '空态要说明怎么办')
})