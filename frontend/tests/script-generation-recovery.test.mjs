import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const screenplay = readFileSync(new URL('../src/views/EpisodeScreenplay.vue', import.meta.url), 'utf8')
const adaptation = readFileSync(new URL('../src/views/AdaptationPlan.vue', import.meta.url), 'utf8')

test('剧本生成按钮只受本次剧本请求约束', () => {
  // 其他后台操作（配音、资产补全、同步）或缺少 synopsis 时也必须保持可点击，
  // 否则一次失败后按钮会永久置灰。
  assert.doesNotMatch(detail, /:disabled="busy \|\| generatingScript \|\| !project\.synopsis"/)
  assert.match(detail, /:disabled="generatingScript" @click="regenerateScript"/)
  assert.doesNotMatch(screenplay, /:disabled="busy \|\| generatingScript \|\| !project\?\.synopsis"/)
  assert.match(screenplay, /:disabled="generatingScript" @click="regenerateEpisodeScript"/)
})

test('剧本生成失败后仍会解锁并给出可重试路径', () => {
  for (const source of [detail, screenplay]) {
    assert.match(source, /SCRIPT_GENERATION_WATCHDOG_MS|SCRIPT_WATCHDOG_MS/)
    assert.match(source, /scriptGenerationWatchdog|scriptWatchdog/)
    assert.match(source, /剧本生成请求超时未返回/)
    assert.match(source, /已有生成任务正在进行/)
    assert.match(source, /resetProjectGeneration/)
  }
  assert.match(detail, /releaseStaleScriptClaim/)
  assert.match(detail, /finally \{\s*endScriptGeneration\(\)/)
  assert.match(screenplay, /releaseStaleScriptClaim/)
})

test('滚动规划页的整集剧本生成同样支持陈旧生成锁恢复', () => {
  assert.match(adaptation, /async function script\(ep\)/)
  assert.match(adaptation, /已有生成任务正在进行/)
  assert.match(adaptation, /resetProjectGeneration/)
  assert.match(adaptation, /finally\{busy\.value=false\}/)
})