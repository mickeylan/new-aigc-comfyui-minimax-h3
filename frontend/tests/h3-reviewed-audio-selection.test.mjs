import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('H3音频模板只允许已审核且哈希一致的生产候选', () => {
  assert.match(detail, /minimax_h3_audio_driven_ref2v_t8_enhanced/)
  assert.match(detail, /minimax_h3_video_then_audio_mux/)
  assert.match(detail, /reviewed_audio\?\.ready/)
  assert.match(detail, /与当前 Dialogue 哈希一致的生产音频候选/)
  assert.match(detail, /仅后期封装音频，不保证口型同步/)
  assert.match(detail, /所选模板需要已审核生产音频/)
  assert.match(detail, /selectedTemplateNeedsReviewedAudio && !reviewedSceneAudioReady/)
  assert.match(detail, /生成Scene连续音轨候选/)
  assert.match(detail, /reviewSceneAudioMix/)
  assert.match(api, /sceneAudioCandidates/)
  assert.match(api, /createSceneAudioCandidate/)
  assert.match(api, /reviewSceneAudioCandidate/)
})
