import test from 'node:test'
import assert from 'node:assert/strict'
import { parsePromptReferences, promptReferenceIssues, referenceInsertTag, referenceDisplayName, insertAtSelection } from '../src/utils/promptReferences.js'

const storyboardBindings = [
  { picture: 1, subject: 1, role: 'character', identity: '三毛' },
  { picture: 2, subject: 2, role: 'character', identity: '大憨' },
  { picture: 3, subject: null, role: 'location', identity: '大憨家客厅' }
]

test('segments rejoin to the exact original prompt', () => {
  const text = '[Shot 1] <Subject 1>坐在<Picture 3>的沙发上，<Subject 2>站在门边。\n第二行'
  const segments = parsePromptReferences(text, storyboardBindings)
  assert.equal(segments.map(s => (s.type === 'ref' ? s.raw : s.text)).join(''), text)
  const refs = segments.filter(s => s.type === 'ref')
  assert.deepEqual(refs.map(r => [r.kind, r.n, r.binding?.identity, r.issue]), [
    ['subject', 1, '三毛', ''],
    ['picture', 3, '大憨家客厅', ''],
    ['subject', 2, '大憨', '']
  ])
})

test('a tag beyond the reference count is flagged', () => {
  const issues = promptReferenceIssues('<Subject 5>转身，<Picture 4>亮起', storyboardBindings)
  assert.deepEqual(issues.map(i => i.raw), ['<Subject 5>', '<Picture 4>'])
  assert.match(issues[0].issue, /第 5 张参考图不存在（当前共 3 张）/)
})

test('a Subject tag pointing at a non-character picture is flagged', () => {
  const [issue] = promptReferenceIssues('<Subject 3>里很暗', storyboardBindings)
  assert.match(issue.issue, /第 3 张是场景「大憨家客厅」，不是人物主体/)
  assert.deepEqual(promptReferenceIssues('<Picture 3>里很暗', storyboardBindings), [])
})

test('repeated invalid tags are reported once and no bindings flags every tag', () => {
  assert.equal(promptReferenceIssues('<Subject 9> 和 <Subject 9>', storyboardBindings).length, 1)
  assert.equal(promptReferenceIssues('<Subject 1>', []).length, 1)
  assert.deepEqual(parsePromptReferences('', storyboardBindings), [])
})

test('a shifted picture number whose written name disagrees is flagged', () => {
  // 参考图删掉第一个人后，旧提示词的编号仍在范围内，只有名字对不上。
  // 旧顺序是 李小虎、三毛、大憨；删掉李小虎后当前是 三毛、大憨、场景。
  const stale = '<Subject 1> 是 <Picture 1> 中的角色「李小虎」四视图。\n<Subject 2> 是 <Picture 2> 中的角色「三毛」四视图。'
  const issues = promptReferenceIssues(stale, storyboardBindings)
  assert.deepEqual(issues.map(i => i.raw), ['<Picture 1>「李小虎」', '<Picture 2>「三毛」'])
  assert.match(issues[0].issue, /当前第 1 张参考图是「三毛」/)
  assert.match(issues[1].issue, /当前第 2 张参考图是「大憨」/)
  assert.deepEqual(promptReferenceIssues('<Picture 1> 中的角色「三毛」；<Picture 3> 场景「别处」', storyboardBindings), [])
})

test('video prompt wording "中名为「X」" is checked too, including a location picture', () => {
  const stale = '<Subject 1> 是四视图参考图（<Picture 1>）中名为「李小虎」的角色。\n<Subject 3> 是四视图参考图（<Picture 3>）中名为「金虎」的角色。'
  const issues = promptReferenceIssues(stale, storyboardBindings)
  // 标记本身的问题先列，名字对不上的随后列。
  assert.deepEqual(issues.map(i => i.raw), ['<Subject 3>', '<Picture 1>「李小虎」', '<Picture 3>「金虎」'])
  assert.match(issues[2].issue, /当前第 3 张参考图是「大憨家客厅」/)
  const frame = [{ picture: 1, subject: 1, role: 'storyboard', identity: '<Picture 1>：当前分镜画面' }]
  assert.deepEqual(promptReferenceIssues('<Picture 1>中名为「任意」', frame), [])
})

test('storyboard frame displays its purpose instead of the whole label', () => {
  const frame = { picture: 8, subject: 8, role: 'storyboard', identity: '<Picture 8>：当前分镜画面关键帧（通用重要时刻的构图与动作状态参考）' }
  assert.equal(referenceDisplayName(frame), '分镜图')
  assert.equal(referenceDisplayName(storyboardBindings[0]), '三毛')
})

test('insert tag follows subject binding and inserts at the cursor', () => {
  assert.equal(referenceInsertTag(storyboardBindings[0]), '<Subject 1>')
  assert.equal(referenceInsertTag(storyboardBindings[2]), '<Picture 3>')
  assert.deepEqual(insertAtSelection('甲乙', '<Subject 1>', 1, 1), { value: '甲<Subject 1>乙', caret: 12 })
  assert.deepEqual(insertAtSelection('甲乙丙', 'X', 1, 2), { value: '甲X丙', caret: 2 })
  assert.deepEqual(insertAtSelection('甲乙', 'X'), { value: '甲乙X', caret: 3 })
})
