import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8')
const host = read('../src/studio/pagedraw-host/PageDrawStage.tsx')
const surface = read('../src/studio/pagedraw/PaintSurface.tsx')

test('2D has separate multi-file layer and base image inputs', () => {
  assert.match(host, /添加图片层/)
  assert.match(host, /替换底图/)
  assert.match(host, /ref=\{layerInputRef\}[^\n]*multiple/)
  assert.match(host, /await useSceneStore\.getState\(\)\.createImageFromFile\(0, 0, file\)/)
  assert.match(host, /onChange=\{handleFileChange\}/)
})

test('2D layer order and visibility use the document, not removal/re-addition', () => {
  assert.match(host, /\[store\.drawNodes\]/)
  assert.match(host, /store\[action\]\(\[node\.id\]\)/)
  for (const action of ['sendToBack', 'sendBackward', 'bringForward', 'bringToFront']) assert.ok(host.includes(action))
  assert.match(host, /updateLayer\(node, \{ visible: hidden \}\)/)
  assert.doesNotMatch(host, /hiddenLayers|restoreLayer/)
  assert.match(surface, /<Group[^>]*visible=\{node\.visible !== false\}[^>]*opacity=\{node\.layerOpacity \?\? 1\}/)
})

test('download and writeback share document export and exclude editor overlays', () => {
  assert.equal((host.match(/documentPNG\(stage\)/g) || []).length, 2)
  assert.match(host, /getAspectRatioDimensions\(\)/)
  assert.match(host, /captureDocumentCanvas\(stage, width, height, \[BG_LAYER_ID, DRAW_LAYER_ID\]\)/)
  assert.match(surface, /ref=\{selectionRectRef\}\s+name="editor-only"/)
  assert.match(surface, /name="editor-only"\s+fillPatternImage/)
  assert.match(surface, /name="editor-only"\s+x=\{shapePreview\.x\}/)
  assert.match(surface, /updateNode\(nodeId, \{ \.\.\.commonNodeUpdates \}, false\);\s+\}\s+saveState\(\);/)
})
