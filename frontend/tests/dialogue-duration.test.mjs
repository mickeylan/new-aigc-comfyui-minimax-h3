import test from 'node:test'
import assert from 'node:assert/strict'
import {
  estimateChineseDialogueDuration,
  estimateDialogueListDuration,
} from '../src/utils/dialogueDuration.js'

test('uses nine spoken characters per second, one margin, and half-second ceiling', () => {
  const text = '字'.repeat(100)
  assert.equal(estimateChineseDialogueDuration(text), 100 / 9)
  assert.equal(estimateDialogueListDuration([{ text, character: '甲' }]), 12)
})

test('a contiguous punctuation cluster contributes only its largest pause', () => {
  assert.equal(estimateChineseDialogueDuration('你!!!好'), 2 / 9 + 0.15)
  assert.equal(estimateChineseDialogueDuration('你， “！？…” 好'), 2 / 9 + 0.15)
})

test('control tags do not count but the spoken text inside d tags must count', () => {
  const marked = '<scenetrans>[Chinese]你<cutoff><d>转身离开！</d>好'
  assert.equal(estimateChineseDialogueDuration(marked), estimateChineseDialogueDuration('你转身离开！好'))
  assert.equal(estimateChineseDialogueDuration('<d>[Chinese]哪里走！！！</d>'), estimateChineseDialogueDuration('哪里走！'))
})

test('speaker or speech-type changes add a switch pause except before the first line', () => {
  const lines = [
    { text: '你', character: '甲', speech_type: 'dialogue' },
    { text: '好', character: '乙', speech_type: 'dialogue' },
  ]
  assert.equal(estimateDialogueListDuration(lines), 1)
  assert.equal(estimateDialogueListDuration([{ text: '你', character: '甲' }, { text: '好', character: '甲' }]), 1)
})

test('quotes and whitespace neither count nor split punctuation clusters; symbols do count', () => {
  assert.equal(estimateChineseDialogueDuration('你！ ” ！好'), 2 / 9 + 0.15)
  assert.equal(estimateChineseDialogueDuration('你❤️好'), 4 / 9)
})
