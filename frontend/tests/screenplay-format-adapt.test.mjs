import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('AI format adaptation is an optional step before the regular import confirm', () => {
  assert.match(detail, /@click="importScreenplay\(true\)">AI 整理格式后导入</)
  assert.match(detail, /@click="importScreenplay\(false\)">/)
  assert.match(api, /adaptScreenplayImport: \(id, number, file\) => \{/)
  assert.match(api, /`\/projects\/\$\{id\}\/episodes\/\$\{number\}\/screenplay\/adapt`/)
  const start = detail.indexOf('async function importScreenplay(adapt = false)')
  const body = detail.slice(start, detail.indexOf('function policyScope', start))
  assert.ok(start > 0)
  // The adapted preview still needs the user's confirmation before it replaces the episode.
  assert.ok(body.indexOf('window.confirm') < body.indexOf('api.applyScreenplayImport'))
  assert.match(body, /preview\.conflicts/)
  assert.match(body, /e\.response\?\.data\?\.issues/)
})

test('imported scenes are handed to the plot-based director split, which asks first', () => {
  const start = detail.indexOf('async function importScreenplay(adapt = false)')
  const body = detail.slice(start, detail.indexOf('function policyScope', start))
  assert.match(body, /if \(imported\) await splitEpisodeByPlot\(\)/)
  const split = detail.slice(detail.indexOf('async function splitEpisodeByPlot'), detail.indexOf('async function controlShotSplit'))
  assert.match(split, /confirm\(/)
})
