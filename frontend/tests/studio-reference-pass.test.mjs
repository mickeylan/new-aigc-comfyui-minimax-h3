import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')

// 白模/法线/深度是给视频模型当控制图的：要能按当前机位渲一张干净的几何参考，
// 不能带网格线、gizmo、泼溅那些「编辑器痕迹」，也不能渲完把编辑器留在参考材质上。
test('三种参考图 pass 齐备，各自用对的材质', () => {
  const type = stage.match(/type ReferencePass = ([^;]+);/)
  assert.ok(type, '缺 ReferencePass 类型')
  for (const pass of ['clay', 'normal', 'depth']) {
    assert.match(type[1], new RegExp(`"${pass}"`), `缺 pass ${pass}`)
    assert.match(stage, new RegExp(`\\b${pass}: "`), `缺 ${pass} 的中文标签`)
  }
  assert.match(stage, /const REFERENCE_PASS_LABELS: Record<ReferencePass, string> = \{/)

  const factory = stage.match(/function createReferenceMaterial\(\s*pass: ReferencePass,\s*depthRange: \{ near: number; far: number \},\s*\) \{([\s\S]*?)\n\}/)
  assert.ok(factory, '缺 createReferenceMaterial')
  // 白模：中性白标准材质，靠场景灯光出体积；双面，单面片模型从背后看才不会被剪掉
  assert.match(factory[1], /color: 0xffffff,/)
  assert.match(factory[1], /side: THREE\.DoubleSide,/)
})

test('深度用线性深度 shader，近黑远白，不是硬关 depthWrite', () => {
  // 曾经试过用 depthWrite 让泼溅挡网格，结果是硬边锯齿 + 白斑黑洞，已回退；
  // 深度参考图必须单独算，不能靠渲染开关。
  assert.doesNotMatch(stage, /depthWrite: true/, 'depthWrite 那条路已证伪，不能再回来')

  const shader = stage.match(/return new THREE\.ShaderMaterial\(\{([\s\S]*?)\n  \}\);/)
  assert.ok(shader, '缺深度 ShaderMaterial')
  assert.match(shader[1], /uNear: \{ value: depthRange\.near \}/)
  assert.match(shader[1], /uFar: \{ value: depthRange\.far \}/)
  // 视图空间深度取正，再按 near/far 归一化到 0..1
  assert.match(shader[1], /vViewDepth = -viewPosition\.z;/)
  assert.match(shader[1], /clamp\(\(vViewDepth - uNear\) \/ \(uFar - uNear\), 0\.0, 1\.0\)/)
  assert.match(shader[1], /gl_FragColor = vec4\(vec3\(linearDepth\), 1\.0\);/)
})

test('深度范围按模型包围盒算，没法线的几何体先补法线', () => {
  // 直接用 camera.near/far（0.1–1000）：几米内的模型只占 1% 灰度，看着就是黑图。
  assert.match(stage, /const bounds = new THREE\.Box3\(\);/)
  // 骨骼红球不属于内容，不能影响深度标定范围；泼溅仍保留独立边界路径。
  assert.match(stage, /objectsRef\.current\.filter\(entry => entry\.object\.visible\)\.forEach\(\(entry\) => bounds\.expandByObject\(entry\.splat \? entry\.object : clonePosedContent\(entry\.object\)\)\);/)
  assert.match(stage, /depthRange\.near = Math\.max\(0\.01, distance - radius\);/)
  assert.match(stage, /depthRange\.far = Math\.max\(depthRange\.near \+ 0\.5, distance \+ radius\);/)

  // GLB 常常不存法线：换成 Standard / Normal 材质后光照拿不到法线，整块渲成纯黑。
  assert.match(stage, /if \(geometry && !geometry\.getAttribute\("normal"\)\) \{/)
  assert.match(stage, /geometry\.computeVertexNormals\(\);/)
})

test('渲一帧前藏掉编辑器痕迹与泼溅，渲完 finally 无条件还原', () => {
  const fn = stage.match(/const captureReference = useCallback\(async \(pass: ReferencePass\) => \{([\s\S]*?)\n  \}, \[\]\);/)
  assert.ok(fn, '缺 captureReference')
  const body = fn[1]

  // 网格线/地面/gizmo/骨架线不是要被约束的几何
  assert.match(body, /hide\(gridRef\.current\);/)
  assert.match(body, /hide\(groundRef\.current\);/)
  assert.match(body, /hide\(transformRef\.current\?\.getHelper\?\.\(\) \?\? null\);/)
  assert.match(body, /if \(child instanceof THREE\.SkeletonHelper\) hide\(child\);/)
  // 泼溅走 Spark 自己的光栅化，overrideMaterial 管不到，混进灰度图会变彩色噪点
  assert.match(body, /hide\(sparkRef\.current\);/)

  // 覆盖材质 + 黑底
  assert.match(body, /objectsRef\.current\.forEach\(\(entry\) => paint\(entry\.object\)\);/)
  assert.match(body, /scene\.background = new THREE\.Color\(0x000000\);/)
  // render 与取图之间不能隔帧
  assert.match(body, /renderer\.clear\(\);\s*\n\s*renderer\.render\(scene, camera\);/)
  assert.match(body, /context\.drawImage\(renderer\.domElement, 0, 0, width, height\);/)
  assert.match(body, /snapshot\.toBlob\(/)

  // 还原是硬要求：否则编辑器一直停在白模材质上
  const restore = body.match(/finally \{([\s\S]*?)\n    \}/)
  assert.ok(restore, '缺 finally 还原')
  assert.match(restore[1], /scene\.overrideMaterial = previousOverride;/)
  assert.match(restore[1], /scene\.background = previousBackground;/)
  assert.match(restore[1], /item\.mesh\.material = item\.material;/)
  assert.match(restore[1], /object\.visible = true;/)
  assert.match(restore[1], /material\.dispose\(\);/)
})

test('序列指定宽度使用物理像素并在finally恢复屏幕DPI', () => {
  const body = stage.slice(stage.indexOf('const exportSequence = useCallback('), stage.indexOf('const saveSequenceToLibrary = useCallback('));
  assert.match(body, /const savedPixelRatio = renderer\?\.getPixelRatio\(\) \?\? 1;/);
  assert.match(body, /renderer\.setPixelRatio\(1\);[\s\S]*?renderer\.setSize\(sequenceWidth, height, false\);/);
  assert.match(body, /finally \{[\s\S]*?renderer\.setPixelRatio\(savedPixelRatio\);\s*renderer\.setSize\(savedSize\.x, savedSize\.y, false\);/);
});

test('右栏能按三种 pass 导出，出图可预览可下载', () => {
  // 三个按钮由标签表驱动，避免 UI 与类型漂移
  assert.match(stage, /\(Object\.keys\(REFERENCE_PASS_LABELS\) as ReferencePass\[\]\)\.map\(\(pass\) => \(/)
  assert.match(stage, /onClick=\{\(\) => void exportReference\(pass\)\}/)
  // 预览 + 下载 + 尺寸/体积提示
  assert.match(stage, /src=\{referenceShot\.url\}/)
  assert.match(stage, /download=\{`\$\{referenceShot\.pass\}-reference\.png`\}/)
  assert.match(stage, /setReferenceShot\(\{ pass, url, bytes: shot\.blob\.size, width: shot\.width, height: shot\.height \}\)/)
  assert.match(stage, /已生成\$\{REFERENCE_PASS_LABELS\[pass\]\}参考图 \$\{shot\.width\}×\$\{shot\.height\}/)
  // 换图时释放旧 blob URL
  assert.match(stage, /if \(referenceUrlRef\.current\) URL\.revokeObjectURL\(referenceUrlRef\.current\);/)
  // 未出图时给一句说明，别让面板空着
  assert.match(stage, /白模看结构、法线看朝向、深度看前后关系（近黑远白）。/)
})