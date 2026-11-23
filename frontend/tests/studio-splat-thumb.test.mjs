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
  // 摆姿红球不能扩大自动取景边界，必须以干净的骨架克隆计算网格包围盒。
  assert.match(body, /box\.union\(new THREE\.Box3\(\)\.setFromObject\(clonePosedContent\(entry\.object\)\)\)/)
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

test('缩略图缩到最长边 512 并编成 JPEG，不再整张 4K PNG 传上去', () => {
  assert.match(stage, /const THUMBNAIL_MAX_EDGE = 512;/)
  const fn = stage.match(/async function shrinkToThumbBlob\(([\s\S]*?)\n\}/)
  assert.ok(fn, '缺 shrinkToThumbBlob')
  const body = fn[1]
  // 只缩不放
  assert.match(body, /Math\.min\(1, maxEdge \/ Math\.max\(canvas\.width, canvas\.height\)\)/)
  // 像素要同步拷到离屏画布，再让 toBlob 慢慢编码
  assert.match(body, /ctx\.drawImage\(canvas, 0, 0, out\.width, out\.height\);/)
  assert.ok(
    body.indexOf('drawImage') < body.indexOf('toBlob'),
    'drawImage 必须在 toBlob 之前（WebGL 缓冲可能已被清）',
  )
  // 3D 预览不需要透明，PNG 同尺寸大一个数量级，改 JPEG
  assert.match(body, /out\.toBlob\(\(blob\) => resolve\(blob\), "image\/jpeg", 0\.85\)/)
  assert.doesNotMatch(body, /"image\/png"/, '缩略图不该再编成 PNG')
  // captureThumbnail 要真的用它，且留了整套原图的退路
  assert.match(stage, /const shrunk = await shrinkToThumbBlob\(canvas\);/)
  assert.match(stage, /if \(shrunk\) return shrunk;/)
})

test('两条入素材库的路径都用 captureThumbnail，不再直取 canvas', () => {
  // 保存 GLB 与保存泼溅各一次
  assert.equal(
    (stage.match(/await captureThumbnail\(\)/g) || []).length,
    2,
    'GLB 存库与泼溅存库都要走助手',
  )
  // 截图和身份迁移改用干净取景，禁止绕过隐藏辅助物的捕获路径。
  const direct = stage.match(/domElement\.toDataURL/g) || []
  assert.equal(direct.length, 0, `直取 canvas 还剩 ${direct.length} 处，可能漏过了干净取景`)
  assert.match(stage, /const snapshotBlob = await captureCurrentView\(\)/)
  assert.match(stage, /URL\.createObjectURL\(await captureCurrentView\(\)\)/)
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