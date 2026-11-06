import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')

test('Scene导演工作台和视频提示词使用显式关闭模态窗口', () => {
  assert.match(detail, /aria-label="Scene导演工作台"/)
  assert.match(detail, /aria-label="编辑视频提示词"/)
  assert.doesNotMatch(detail, /scene-director-mask" @click\.self="closeSceneWorkspace"/)
  assert.doesNotMatch(detail, /v-if="videoTaskDetail" class="modal-mask" @click\.self="closeVideoPrompt"/)
})

test('编辑模态窗口关闭前保护未保存内容', () => {
  assert.match(detail, /sceneWorkspaceDirty/)
  assert.match(detail, /Scene导演工作台可能有未保存内容/)
  assert.match(detail, /videoPromptDirty/)
  assert.match(detail, /视频提示词有未保存修改/)
  assert.match(detail, /closeSceneWorkspace\(true\)/)
  assert.match(detail, /videoPromptBaseline\.value = videoPromptState\(\)/)
})
