import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('分镜图视频用途仅由独立弹窗显式保存', () => {
  assert.match(api, /updateSceneStoryboardVideoRole:.*storyboard-video-role/)
  assert.match(detail, /分镜图用于视频/)
  for (const text of ['起始帧', '关键帧', '结束帧', '不用于视频', '显式保存']) assert.match(detail, new RegExp(text))
  assert.match(detail, /storyboard_video_role \|\| 'keyframe'/)
  assert.match(detail, /framePurposeForm\.time !== '' \? Number/)
  assert.match(detail, /storyboard_video_time:.*: null/)
})

test('取消、非法时间和零秒处理明确', () => {
  assert.match(detail, /@click="closeFramePurpose">取消/)
  assert.match(detail, /Number\.isFinite\(n\) && n >= 0/)
  assert.match(detail, /framePurposeForm\.time === ''/)
  assert.match(detail, /:disabled="framePurposeSaving \|\| !framePurposeValid"/)
})

test('异步保存绑定scene、project与epoch且仅合并安全字段', () => {
  assert.match(detail, /const sceneId = framePurposeScene\.value\.id, projectId = id\(\), epoch = \+\+framePurposeEpoch/)
  assert.match(detail, /epoch !== framePurposeEpoch \|\| id\(\) !== projectId \|\| framePurposeScene\.value\?\.id !== sceneId/)
  assert.match(detail, /\['storyboard_video_role', 'storyboard_video_time', 'updated_at', 'prompt_stale'\]/)
  const saveBody = detail.slice(detail.indexOf('async function saveFramePurpose'), detail.indexOf('\n}', detail.indexOf('async function saveFramePurpose')) + 2)
  assert.doesNotMatch(saveBody, /local\.(image_file|video_file|image_prompt|video_prompt)\s*=/)
  assert.match(detail, /不会自动生成/)
})

test('用途说明覆盖Ref2VA、严格首尾和连续性优先级', () => {
  assert.match(detail, /Ref2VA 中这是软约束而非硬帧/)
  assert.match(detail, /严格首尾需选择对应首尾帧模式/)
  assert.match(detail, /连续性尾帧会覆盖当前显式设置，手工选择的帧优先级更高/)
})
