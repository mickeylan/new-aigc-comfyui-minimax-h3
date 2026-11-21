import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { parseRunResults, runResultKind, runResultPath, runResultUrl } from '../src/utils/playgroundResults.js'

const view = readFileSync(new URL('../src/views/Playground.vue', import.meta.url), 'utf8')

test('结果文件既能是数组也能是字符串化 JSON，非法内容不抛错', () => {
  const file = { type: 'images', filename: 'a.png', subfolder: '' }
  assert.deepEqual(parseRunResults([file]), [file])
  assert.deepEqual(parseRunResults(JSON.stringify([file])), [file])
  assert.deepEqual(parseRunResults('[]'), [])
  assert.deepEqual(parseRunResults('not json'), [])
  assert.deepEqual(parseRunResults(null), [])
})

test('按扩展名区分图片、视频、音频与其它文件', () => {
  assert.equal(runResultKind({ filename: 'Qwen_image_2.1_00164.png' }), 'image')
  assert.equal(runResultKind({ filename: 'x.JPG' }), 'image')
  assert.equal(runResultKind({ filename: 'clip.mp4' }), 'video')
  assert.equal(runResultKind({ filename: 'voice.wav' }), 'audio')
  assert.equal(runResultKind({ filename: 'archive.zip' }), 'file')
})

test('结果地址优先走实例输出，缺实例时退回 GPU 槽位', () => {
  const file = { type: 'images', filename: 'Qwen_image_2.1_00164.png', subfolder: '' }
  assert.equal(runResultUrl({ instance_id: 1, gpu_index: 0 }, file), '/api/instances/1/output/Qwen_image_2.1_00164.png')
  assert.equal(runResultUrl({ gpu_index: 2 }, file), '/api/output/2/Qwen_image_2.1_00164.png')
  assert.equal(runResultUrl({}, file), '/api/output/0/Qwen_image_2.1_00164.png')
  assert.equal(runResultUrl({ instance_id: 3 }, { filename: 'b.png', subfolder: '/samples/' }), '/api/instances/3/output/samples/b.png')
  assert.equal(runResultPath({ filename: 'c.png' }), 'c.png')
  assert.equal(runResultUrl({ instance_id: 3 }, {}), '')
})

test('结果卡片显示图片/视频/音频，而不是只打印 result JSON', () => {
  assert.match(view, /resultsOf\(run\)/)
  assert.match(view, /:src="runResultUrl\(run, file\)"/)
  assert.match(view, /<video v-else-if="runResultKind\(file\) === 'video'"/)
  assert.match(view, /<audio v-else-if="runResultKind\(file\) === 'audio'"/)
  assert.match(view, /<a v-if="runResultKind\(file\) === 'image'"[^>]*><img/)
  // 原始 JSON 只在没有可显示文件时折叠出现
  assert.match(view, /<details v-else-if="run\.result && run\.result !== '\[\]'" class="raw-result"><summary>原始结果/)
  assert.doesNotMatch(view, /<pre class="mono result">\{\{ run\.result \}\}<\/pre><\/article>/)
})