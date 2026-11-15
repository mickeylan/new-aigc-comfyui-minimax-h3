import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')
const studio = readFileSync(new URL('../src/views/EpisodeVoiceStudio.vue', import.meta.url), 'utf8')

test('voice extraction API exposes start, list and explicit confirm routes', () => {
  assert.match(api, /characterVoiceExtractions:.*\/voice\/extractions`\)/)
  assert.match(api, /startCharacterVoiceExtraction:.*\/voice\/extractions`, data/)
  assert.match(api, /confirmCharacterVoiceExtraction:.*\/voice\/extractions\/\$\{eid\}\/confirm`/)
})

test('voice studio previews the separated draft and binds only on confirmation', () => {
  assert.match(studio, /<AudioWaveform v-if="extractScene\(role\)"/)
  assert.match(studio, /<audio v-if="ex\.draft_file"/)
  assert.match(studio, /v-if="ex\.status==='ready'"[^>]*@click="confirmExtraction\(role, ex\)"/)
  assert.match(studio, /不做对白识别/)
  assert.doesNotMatch(studio, /startExtraction[^\n]*confirmCharacterVoiceExtraction/)
})
