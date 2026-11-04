import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')
const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')

test('manual story dialogue repair uses the dedicated saved-dialogue endpoint', () => {
  assert.match(api, /syncEpisodeStoryDialogues:\s*\(id, number\) => http\.post\(`\/projects\/\$\{id\}\/episodes\/\$\{number\}\/story-dialogues\/sync`\)/)
  assert.match(detail, /补回本集对白到故事内容/)
  assert.match(detail, /不会删除原文、不会修改说话人或媒体，也不会发起 AI\/GPU/)
  assert.match(detail, /const projectID = id\(\)[\s\S]*const episodeN = activeEpN\.value[\s\S]*api\.syncEpisodeStoryDialogues\(projectID, episodeN\)/)
})
