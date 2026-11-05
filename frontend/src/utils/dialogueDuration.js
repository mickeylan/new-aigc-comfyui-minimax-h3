export const DIALOGUE_CHARS_PER_SECOND = 9
export const DIALOGUE_MARGIN_SECONDS = 0.4
export const SPEAKER_SWITCH_SECONDS = 0.15
export const COMMA_PAUSE_SECONDS = 0.08
export const SENTENCE_END_PAUSE_SECONDS = 0.15

const NON_SPOKEN_MARKER_RE = /<\/?(?:d|scenetrans|cutoff)\b[^>]*>|\[Chinese\]/giu
const COMMA_PUNCTUATION = new Set([',', '，', '、', ';', '；', ':', '：'])
const END_PUNCTUATION = new Set(['.', '。', '!', '！', '?', '？', '…', '—', '–', '―'])
const QUOTE_PUNCTUATION = new Set(['"', "'", '“', '”', '‘', '’', '「', '」', '『', '』', '《', '》', '〈', '〉', '（', '）', '(', ')', '[', ']', '【', '】'])

function spokenText(text) {
  return String(text ?? '').replace(NON_SPOKEN_MARKER_RE, '')
}

export function estimateChineseDialogueDuration(text) {
  let spokenCharacters = 0
  let punctuationPause = 0
  let clusterPause = 0

  for (const character of spokenText(text)) {
    if (/\s/u.test(character) || QUOTE_PUNCTUATION.has(character)) continue
    if (/\p{P}/u.test(character)) {
      const pause = COMMA_PUNCTUATION.has(character)
        ? COMMA_PAUSE_SECONDS
        : (END_PUNCTUATION.has(character) ? SENTENCE_END_PAUSE_SECONDS : 0)
      clusterPause = Math.max(clusterPause, pause)
      continue
    }
    punctuationPause += clusterPause
    clusterPause = 0
    spokenCharacters += 1
  }

  return spokenCharacters / DIALOGUE_CHARS_PER_SECOND + punctuationPause + clusterPause
}

export function estimateDialogueListDuration(dialogues) {
  let seconds = 0
  let previousSpeaker = null
  let hasDialogue = false

  for (const dialogue of Array.isArray(dialogues) ? dialogues : []) {
    const duration = estimateChineseDialogueDuration(dialogue?.text)
    if (!duration) continue
    const speaker = `${dialogue?.character ?? ''}\u0000${dialogue?.speech_type ?? ''}`
    if (hasDialogue && speaker !== previousSpeaker) seconds += SPEAKER_SWITCH_SECONDS
    seconds += duration
    previousSpeaker = speaker
    hasDialogue = true
  }

  if (!hasDialogue) return 0
  return Math.ceil((seconds + DIALOGUE_MARGIN_SECONDS) * 2) / 2
}
