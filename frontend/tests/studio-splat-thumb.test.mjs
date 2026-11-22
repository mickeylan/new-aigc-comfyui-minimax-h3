import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')

// 实测过：泼溅刚放进场景就点「存库」，截到的是一张空视口图（素材库里一格空地网格）。
test('缩略图先强制重画一帧，再采样确认不是空视口', () => {
  const fn = stage.match(/const captureThumbnail = useCallback\(([\s\S]*?)\n  \}, \[\]\);/)
  assert.ok(fn, '缺 captureThumbnail')
  const body = fn[1]
  assert.match(body, /renderer\.render\(scene, camera\)/, '截之前必须强制渲染一帧')
  assert.match(body, /looksBlank\(\)/, '必须有空视口判定')
  assert.match(body, /splatLodPendingRef\.current/, '要等泼溅细节层建完')
  assert.match(body, /const deadline = performance\.now\(\) \+ timeoutMs/, '必须有超时，不能死等')
  assert.match(body, /canvas\.toDataURL\("image\/png"\)/, '最终仍取 PNG')
  // 选中箭头是编辑器 chrome，不该烤进缩略图；但必须还原
  assert.match(body, /gizmo\.visible = false/, '截图前应藏 gizmo')
  assert.match(body, /finally \{[\s\S]*gizmo\.visible = gizmoVisible/, '截完必须还原 gizmo')
})

test('缩略图自动取景：临时推相机框住全部对象，截完还原视角', () => {
  const fn = stage.match(/const captureThumbnail = useCallback\(([\s\S]*?)\n  \}, \[\]\);/)
  const body = fn[1]
  // 泼溅用自己的包围盒，网格用 setFromObject
  assert.match(body, /box\.union\(entry\.splat\.getBoundingBox\(false\)/)
  assert.match(body, /box\.union\(new THREE\.Box3\(\)\.setFromObject\(entry\.object\)\)/)
  // 距离按 fov 算，带余量
  // getBoundingBox(false)：连 splat 自身尺度一起算，默认只取中心点会低估
  assert.match(body, /radius \/ Math\.sin\(fov \/ 2\)\) \* 1\.35/)
  // 用户视角必须还原，否则点一次「存库」相机就跑了
  assert.match(body, /savedPosition = camera\?\.position\.clone\(\)/)
  assert.match(body, /finally \{[\s\S]*camera\.position\.copy\(savedPosition\)/)
  assert.match(body, /controls\.target\.copy\(savedTarget\)/)
  // 取景后要立刻渲染再取 PNG，中间不能插 await
  const grab = body.slice(body.indexOf('renderer.render(scene, camera);\n      const dataUrl'))
  assert.ok(grab, '取景后应紧接一次渲染再取图')
  const slice = body.slice(body.indexOf('camera.position.copy(center)'), body.indexOf('toDataURL'))
  assert.ok(!/\n\s*await /.test(slice), '取景与取图之间不能有 await（注释里提到的不算）')
})

test('两条入素材库的路径都用 captureThumbnail，不再直取 canvas', () => {
  // 保存 GLB 与保存泼溅各一次
  assert.equal(
    (stage.match(/await captureThumbnail\(\)/g) || []).length,
    2,
    'GLB 存库与泼溅存库都要走助手',
  )
  // 直取 canvas 只应留在「截图」这种"看到什么存什么"的地方
  const direct = stage.match(/domElement\.toDataURL/g) || []
  assert.equal(direct.length, 2, `直取 canvas 还剩 ${direct.length} 处，多出来的可能漏改`)
})

test('缩略图上传用助手返回的 Blob，不再二次 fetch dataURL', () => {
  assert.equal(
    (stage.match(/form\.append\("thumbnail", (thumb|thumbnail), "thumb\.png"\)/g) || []).length,
    2,
  )
  assert.equal(
    (stage.match(/await fetch\(thumb(String|nail)?\)\)\.blob\(\)/g) || []).length,
    0,
    '不应再从 dataURL 二次取 Blob',
  )
})