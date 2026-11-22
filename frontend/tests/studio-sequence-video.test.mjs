import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')
const router = readFileSync(new URL('../../backend/internal/api/router.go', import.meta.url), 'utf8')

test('序列合成视频：编辑器打新接口，带上 pass/stamp/fps', () => {
  assert.match(stage, /const composeSequenceVideo = useCallback\(async \(\) => \{/)
  assert.match(stage, /fetch\("\/api\/materials\/sequence-video", \{/)
  assert.match(
    stage,
    /body: JSON\.stringify\(\{ pass: cache\.pass, stamp: cache\.stamp, fps: sequenceFps \}\)/,
  )
  // 帧数上限沿用存库那套校验，超 60 帧不合成
  assert.match(stage, /const blocker = sequenceLibraryBlocker\(cache\.blobs\.length\);/)
  assert.match(stage, /合成 mp4（后端 ffmpeg）/)
})

test('后端路由：POST /api/materials/sequence-video 已注册', () => {
  assert.match(router, /r\.POST\("\/api\/materials\/sequence-video", svc\.HandleComposeSequenceVideo\)/)
})