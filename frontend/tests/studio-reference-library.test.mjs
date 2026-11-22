import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const {
  SEQUENCE_LIBRARY_MAX_FRAMES,
  fileNameStamp,
  referenceFileName,
  sequenceFrameFileName,
  sequenceLibraryBlocker,
} = await import('../src/studio/scene3d/reference-library.ts')

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')

test('参考图入库命名：单帧带 pass 与时间戳，序列帧号三位且和 zip 内一致', () => {
  const stamp = fileNameStamp(new Date(2026, 10, 22, 17, 12, 34))
  assert.equal(stamp, '20261122-171234')
  assert.equal(referenceFileName('clay', stamp), 'clay-20261122-171234.png')
  assert.equal(referenceFileName('depth', stamp), 'depth-20261122-171234.png')
  assert.equal(referenceFileName('', stamp), 'ref-20261122-171234.png')
  assert.equal(sequenceFrameFileName('depth', stamp, 0), 'depth-seq-20261122-171234-000.png')
  assert.equal(sequenceFrameFileName('depth', stamp, 7), 'depth-seq-20261122-171234-007.png')
  assert.equal(sequenceFrameFileName('depth', stamp, 120), 'depth-seq-20261122-171234-120.png')
})

test('序列入库限额：空序列/超上限都给明确原因', () => {
  assert.equal(sequenceLibraryBlocker(0), '序列还没有帧，先导出一次')
  assert.equal(sequenceLibraryBlocker(13), null)
  assert.equal(sequenceLibraryBlocker(SEQUENCE_LIBRARY_MAX_FRAMES), null)
  const blocked = sequenceLibraryBlocker(SEQUENCE_LIBRARY_MAX_FRAMES + 1)
  assert.ok(blocked && blocked.includes('超过一次存库上限'), String(blocked))
})

test('编辑器接线：单帧/序列都能存进素材库（type=image）', () => {
  // 单帧：把预览 URL 读回 blob，按 pass 命名上传
  assert.match(stage, /const saveReferenceToLibrary = useCallback\(async \(\) => \{/)
  assert.match(stage, /form\.append\("file", blob, referenceFileName\(shot\.pass, fileNameStamp\(new Date\(\)\)\)\);/)
  assert.match(stage, /form\.append\("type", "image"\);/)
  // 序列：导出时把帧留一份，存库时逐帧上传
  assert.match(stage, /const sequenceFramesRef = useRef<\{ pass: ReferencePass; stamp: string; blobs: Blob\[\] \} \| null>\(null\);/)
  assert.match(stage, /sequenceFramesRef\.current = \{/)
  assert.match(stage, /const saveSequenceToLibrary = useCallback\(async \(\) => \{/)
  assert.match(stage, /const blocker = sequenceLibraryBlocker\(cache\.blobs\.length\);/)
  assert.match(stage, /form\.append\("file", cache\.blobs\[index\], sequenceFrameFileName\(cache\.pass, cache\.stamp, index\)\);/)
  // UI 两个入口
  assert.match(stage, /把这张参考图（白模\/法线\/深度）存进素材库/)
  assert.match(stage, /把最近一次导出的序列逐帧存进素材库（一次最多 60 帧）/)
})