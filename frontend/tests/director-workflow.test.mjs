import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { compatibleSkills, projectSkillConfig, promptHistoryLabel } from '../src/utils/directorWorkflow.js'

test('项目 Skill 按阶段和 operation 精确隔离', () => {
  const skills = [
    { id: 1, stage: 'video_prompt', operation: 'director-shot-packet', enabled: true },
    { id: 2, stage: 'video_prompt', operation: 'reference-shot-state-prompt', enabled: true },
    { id: 3, stage: 'video_prompt', operation: 'director-shot-packet', enabled: false }
  ]
  assert.deepEqual(compatibleSkills(skills, 'video_prompt', 'director-shot-packet').map(v => v.id), [1])
  const configs = [{ stage: 'video_prompt', operation: 'director-shot-packet', skill_id: 1 }, { stage: 'video_prompt', operation: 'reference-shot-state-prompt', skill_id: 2 }]
  assert.equal(projectSkillConfig(configs, 'video_prompt', 'reference-shot-state-prompt').skill_id, 2)
})

test('提示词历史明确区分草稿和已应用且保留动作来源', () => {
  assert.equal(promptHistoryLabel({ action: 'optimize', state: 'draft' }), '优化 · 草稿')
  assert.equal(promptHistoryLabel({ action: 'rollback', state: 'applied' }), '回滚草稿 · 已应用')
})

test('视频提示词首尾帧支持连续性帧、当前分镜和自定义上传', () => {
  const source = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
  assert.match(source, /连续性设置：上一镜确认尾帧/)
  assert.match(source, /上传其他首帧/)
  assert.match(source, /上传其他尾帧/)
  assert.match(source, /api\.uploadSceneVideoFrame/)
})

test('剪辑台保留三个高级导演审查入口和视觉节拍应用入口', () => {
  const source = readFileSync(new URL('../src/views/ProjectEditor.vue', import.meta.url), 'utf8')
  for (const handler of ['runVisualBeats', 'runAssetContinuityReview', 'runCoverageReview', 'applyVisualBeats']) {
    assert.match(source, new RegExp(`@click=["']${handler}["']`), `${handler} 缺少可访问的 UI 入口`)
  }
})
