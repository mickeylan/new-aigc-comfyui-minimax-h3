import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { overlayDialogueExclusions, setDialogueItemExcluded, snapshotDialogueExclusions } from '../src/utils/dialogueAnalysis.js'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')

test('exclude and restore mutate the item locally without making an API request', () => {
  const item = { index: 3, text: '原句', excluded: false }
  setDialogueItemExcluded(item, true)
  assert.equal(item.excluded, true)
  setDialogueItemExcluded(item, false)
  assert.equal(item.excluded, false)
  assert.doesNotMatch(setDialogueItemExcluded.toString(), /api\.|fetch|axios/)
})

test('reanalyze overlays both explicit exclude and restore by source occurrence', () => {
  const local = [{ index: 0, text: '甲', excluded: true }, { index: 1, text: '乙', excluded: false }]
  const merged = overlayDialogueExclusions([{ index: 0, text: '甲', excluded: false }, { index: 1, text: '乙', excluded: true }], snapshotDialogueExclusions(local))
  assert.deepEqual(merged.map(item => item.excluded), [true, false])
})

test('apply payload carries the analyzed content hash and exclusions', () => {
  assert.match(detail, /content_hash: dialogueAnalysis\.content_hash/)
  assert.match(detail, /items: dialogueAnalysis\.items/)
  assert.match(detail, /dialogueAnalysis\.content_hash = value\.content_hash/)
})
