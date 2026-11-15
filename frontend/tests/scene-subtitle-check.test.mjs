import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')
const panel = readFileSync(new URL('../src/components/SceneSubtitleCheck.vue', import.meta.url), 'utf8')
const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')

test('subtitle check API exposes read, run and manual decision routes', () => {
  assert.match(api, /sceneSubtitleCheck:.*\/subtitle-check`\)/)
  assert.match(api, /runSceneSubtitleCheck:.*http\.post\(`\/projects\/\$\{id\}\/scenes\/\$\{sid\}\/subtitle-check`/)
  assert.match(api, /decideSceneSubtitleCheck:.*\/subtitle-check\/decision`, \{ decision \}/)
})

test('subtitle panel shows three states, timestamp hints and a manual decision only for uncertain', () => {
  assert.match(panel, /clean: '未检出字幕', subtitles: '疑似有字幕', uncertain: '不确定'/)
  assert.match(panel, /v-for="\(h, i\) in check\.hints"[\s\S]*\$emit\('seek', h\.start\)/)
  assert.match(panel, /v-if="check\.status === 'uncertain' && !check\.stale" class="decision"/)
  assert.match(panel, /decide\('has_subtitles'\)[\s\S]*decide\('no_subtitles'\)/)
  assert.match(panel, /不会修改视频/)
})

test('Scene video box mounts the subtitle panel and seeks the player from hints', () => {
  assert.match(detail, /<SceneSubtitleCheck :project-id="project\.id" :scene-id="sc\.id" @seek="t => seekSceneVideo\(sc, t\)"/)
  assert.match(detail, /function seekSceneVideo\(sc, seconds\)/)
})
