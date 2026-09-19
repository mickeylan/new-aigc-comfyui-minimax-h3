import test from 'node:test'
import assert from 'node:assert/strict'
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
