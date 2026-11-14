import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { compareGenerationSnapshots, parseSnapshotRefs } from '../src/utils/generationSnapshot.js'

const base = { template_code: 'minimax_h3_ref2v', template_sha256: 'aaa', prompt_sha256: 'p1', params_json: '{"seed":1}', refs_json: JSON.stringify([{ role: 'ref_images', file: '1/a.png', sha256: 'h1' }, { role: 'ref_images', file: '1/b.png', sha256: 'h2' }]) }

test('生成快照对比：相同输入无差异，参考图内容变化与增删逐条列出', () => {
  assert.deepEqual(compareGenerationSnapshots(base, { ...base }), [])
  const changed = { ...base, prompt_sha256: 'p2', refs_json: JSON.stringify([{ role: 'ref_images', file: '1/a.png', sha256: 'h9' }, { role: 'ref_images', file: '1/c.png', sha256: 'h3' }]) }
  const diff = compareGenerationSnapshots(changed, base)
  assert.ok(diff.includes('提示词已修改'))
  assert.ok(diff.some(line => line.startsWith('参考图内容已变：1/a.png')))
  assert.ok(diff.includes('新增参考：1/c.png'))
  assert.ok(diff.includes('移除参考：1/b.png'))
  assert.ok(!diff.includes('生成参数已修改'))
  assert.deepEqual(compareGenerationSnapshots({ ...base, template_sha256: 'bbb' }, base), ['模板 minimax_h3_ref2v 的工作流已更新：aaa → bbb'])
  assert.deepEqual(compareGenerationSnapshots(base, null), [])
  assert.deepEqual(parseSnapshotRefs({ refs_json: 'bad' }), [])
})

test('视频提示词弹窗按当前场景加载生成快照，慢回复不会落到其他场景', () => {
  const source = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
  assert.match(source, /api\.sceneGenerationSnapshots\(id\(\), sc\.id, 'video'\)\.then\(\(\{ data \}\) => \{ if \(epoch === videoPromptRequestEpoch\)/)
  assert.match(source, /compareGenerationSnapshots\(generationSnapshots\.value\[0\], generationSnapshots\.value\[1\]\)/)
  const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')
  assert.match(api, /scenes\/\$\{sid\}\/generation-snapshots`/)
})
