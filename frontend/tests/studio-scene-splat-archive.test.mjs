import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')

test('存档按需升到 v7，保留v6动作、泼溅机位合成平面与对象状态', () => {
  assert.match(stage, /interface SplatDocument \{/)
  assert.match(stage, /data: string;/, '泼溅字节要 base64 内嵌')
  assert.match(stage, /version: 1 \| 2 \| 3 \| 4 \| 5 \| 6 \| 7;/)
  assert.match(stage, /splats\?: SplatDocument\[\];/)
  assert.match(stage, /cameraKeys\?: CameraKey\[\];/, 'v3 起机位序列要入库')
  assert.match(stage, /version: recipes\?\.references\.size \? 7 : 6,/, '有配方写v7，无配方仍写v6并携带动作资源')
  assert.match(stage, /recipeIds: new Set\(recipes\?\.references\.keys\(\)\)/, '只有已成功归档的配方对象才从GLB排除')
  assert.match(stage, /recipeModule\.validateRecipeArchive\(doc\.procedural\)/, '载入前验证配方版本与内容')
  assert.match(stage, /recipeModule\.restoreRecipeObject\(doc\.procedural, saved\.procedural!\)/, '每个实例使用指定的配方索引恢复')
  assert.match(stage, /onlyVisible: !options\.flattenTransforms/, '存档必须包含隐藏几何')
  assert.match(stage, /visible: entry\.object\.visible, locked: !!entry\.locked/)
  assert.match(stage, /planes: objectsRef\.current\.filter\(\(entry\) => entry\.mediaKind\)/)
})

test('保存时先把 .spz 字节取回来，取不到就整体失败', () => {
  assert.match(stage, /if \(!entry\.splatUrl\) throw new Error\(/, '没有来源地址要报错')
  assert.match(stage, /const response = await fetch\(entry\.splatUrl\);/)
  assert.match(stage, /data: arrayBufferToBase64\(await response\.arrayBuffer\(\)\)/)
  // 泼溅的变换记在 splats 里，不能混进 GLB 节点名单
  assert.match(stage, /\.filter\(\(entry\) => !entry\.splat && !entry\.mediaKind\)/)
})

test('载入 v1 / v2 / v3 / v4 / v5 / v6 / v7，泼溅按 blob URL 交给 Spark 重建并回填变换', () => {
  assert.match(stage, /if \(\(doc\.version !== 1 && doc\.version !== 2 && doc\.version !== 3 && doc\.version !== 4 && doc\.version !== 5 && doc\.version !== 6 && doc\.version !== 7\)/, 'v1/v2 老存档仍要能读')
  assert.match(stage, /for \(const saved of doc\.splats \?\? \[\]\)/)
  assert.match(stage, /new Blob\(\[base64ToArrayBuffer\(saved\.data\)\]/)
  assert.match(stage, /await loadSplatMesh\(url, \{ placeOnGround: false \}\)/)
  assert.match(stage, /group\.position\.fromArray\(saved\.position\);/)
  assert.match(stage, /splatUrl: url,/)
})

test('泼溅不进 GLB 导出，纯泼溅场景给出明确提示', () => {
  assert.match(stage, /if \(entry\.splat\) return;/, 'exportGeometry 要跳过泼溅')
  assert.match(stage, /没有可导出GLB的对象（视频或泼溅请保存场景存档）/)
  assert.match(stage, /场景里只有泼溅，造不了 GLB 素材；请在左侧对象列表里逐层「存库」，或先存成场景存档/)
})

test('载入新场景时释放旧泼溅的 GPU 缓冲', () => {
  assert.match(stage, /function disposeSceneEntry\(entry: SceneObjectEntry\)/)
  assert.match(stage, /if \(entry\.splat\) \{ disposeSplat\(entry\.splat\)/)
  assert.match(stage, /scene\.remove\(entry\.object\); disposeSceneEntry\(entry\)/)
  assert.match(stage, /if \(!committed\) \{ pending\.forEach\(disposeSceneEntry\); pendingSky\?\.dispose\(\)/, '失败预加载也必须清理')
})