import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const {
  SCENE_ARCHIVE_MAX_BYTES,
  archiveSizeLabel,
  archiveTimeLabel,
  parseSceneArchiveList,
  sceneArchiveBlocker,
  sceneArchiveFileName,
  sceneArchiveFormBody,
  sceneStudioContext,
  sceneArchiveQuery,
} = await import('../src/studio/scene3d/scene-archive.ts')

const stage = readFileSync(
  new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url),
  'utf8',
)

test('体积标签：字节 / KB / MB 三档都保留可读精度', () => {
  assert.equal(archiveSizeLabel(0), '0 B')
  assert.equal(archiveSizeLabel(512), '512 B')
  assert.equal(archiveSizeLabel(2048), '2.0 KB')
  assert.equal(archiveSizeLabel(18.4 * 1024 * 1024), '18.4 MB')
  assert.equal(archiveSizeLabel(-5), '0 B')
  assert.equal(archiveSizeLabel(Number.NaN), '0 B')
})

test('时间标签：本地时间补零，坏输入留空', () => {
  const label = archiveTimeLabel('2026-11-23T10:05:00+08:00')
  assert.match(label, /^2026-11-23 \d\d:05$/)
  assert.equal(archiveTimeLabel('not-a-date'), '')
  assert.equal(archiveTimeLabel(''), '')
})

test('列表解析：认 {items,total}', () => {
  const items = parseSceneArchiveList({
    items: [{ id: 3, project_id: 1, name: 'scene-1.json', size: 2048, created_at: '2026-11-23T10:05:00+08:00' }],
    total: 1,
  })
  assert.equal(items.length, 1)
  assert.deepEqual(
    { id: items[0].id, projectId: items[0].projectId, name: items[0].name, sizeLabel: items[0].sizeLabel },
    { id: 3, projectId: 1, name: 'scene-1.json', sizeLabel: '2.0 KB' },
  )
  assert.match(items[0].createdLabel, /^2026-11-23 \d\d:05$/)
})

test('列表解析：也认裸数组', () => {
  const items = parseSceneArchiveList([{ id: 9, name: 'a.json', size: 10 }])
  assert.equal(items.length, 1)
  assert.equal(items[0].id, 9)
  assert.equal(items[0].projectId, null)
})

test('列表解析：坏行走掉，不把整表打空', () => {
  const items = parseSceneArchiveList({
    items: [null, 'x', { id: 0 }, { id: -1 }, { id: 4 }, { id: 5, size: 'oops' }],
  })
  assert.deepEqual(items.map((item) => item.id), [4, 5])
  assert.equal(items[0].name, '存档 4')
  assert.equal(items[1].sizeLabel, '0 B')
})

test('列表解析：非对象 / 无 items 一律当空', () => {
  assert.deepEqual(parseSceneArchiveList(null), [])
  assert.deepEqual(parseSceneArchiveList({ total: 2 }), [])
  assert.deepEqual(parseSceneArchiveList({ items: 'nope' }), [])
})

test('文件名：同一毫秒可复现，坏输入退回当前时间', () => {
  assert.equal(sceneArchiveFileName(1764000000000), 'scene-1764000000000.json')
  assert.match(sceneArchiveFileName(Number.NaN), /^scene-\d+\.json$/)
})

test('上传闸门：空档与超限都要在浏览器里先拦', () => {
  assert.equal(sceneArchiveBlocker('{"version":3}'), '')
  assert.match(sceneArchiveBlocker(''), /空/)
  const oversize = 'a'.repeat(SCENE_ARCHIVE_MAX_BYTES + 1)
  const reason = sceneArchiveBlocker(oversize)
  assert.match(reason, /32 MB/)
  assert.match(reason, /超过/)
})

test('上传表单：file 与 name 同名，project_id 只在有值时才带', () => {
  const form = sceneArchiveFormBody('{"version":3}', 'scene-1.json', 7)
  const file = form.get('file')
  assert.ok(file instanceof Blob)
  assert.equal(file.name, 'scene-1.json')
  assert.equal(form.get('name'), 'scene-1.json')
  assert.equal(form.get('project_id'), '7')

  const noProject = sceneArchiveFormBody('{"version":3}', 'scene-2.json', null)
  assert.equal(noProject.get('project_id'), null)
  const zeroProject = sceneArchiveFormBody('{"version":3}', 'scene-3.json', 0)
  assert.equal(zeroProject.get('project_id'), null)
})

test('场次来源严格校验：缺失、数组、非整数不得悄悄成为另一场次', () => {
  assert.equal(sceneStudioContext(undefined, undefined), undefined)
  assert.deepEqual(sceneStudioContext('7', '11'), { projectId: 7, sceneId: 11 })
  for (const pair of [[7, undefined], [undefined, 11], [0, 11], [7, -1], [7, 1.2], [7, ['11']], [7, '1e2'], [7, ' 11'], [7, Number.MAX_SAFE_INTEGER + 1]]) {
    assert.throws(() => sceneStudioContext(...pair), /来源/)
  }
})

test('关联表单和分页过滤携带相同来源身份；旧存档仍无场次', () => {
  const form = sceneArchiveFormBody('{}', 'scene.json', 7, 11)
  assert.equal(form.get('project_id'), '7')
  assert.equal(form.get('scene_id'), '11')
  assert.throws(() => sceneArchiveFormBody('{}', 'scene.json', null, 11), /来源/)
  assert.equal(sceneArchiveFormBody('{}', 'scene.json', 7).get('scene_id'), null)
  const context = sceneStudioContext(7, 11)
  const query = new URLSearchParams(sceneArchiveQuery(2, 'scene', context))
  assert.deepEqual(Object.fromEntries(query), { page: '2', page_size: '10', project_id: '7', scene_id: '11' })
  assert.equal(new URLSearchParams(sceneArchiveQuery(1, 'all', context)).has('scene_id'), false)
  assert.throws(() => sceneArchiveQuery(1, 'scene'), /没有关联/)
  assert.equal(parseSceneArchiveList([{ id: 1, project_id: 7, scene_id: 11 }])[0].sceneId, 11)
  assert.equal(parseSceneArchiveList([{ id: 1 }])[0].sceneId, null)
})

test('没给 name 时用时间戳文件名兜底', () => {
  const form = sceneArchiveFormBody('{"version":3}', '')
  assert.match(form.get('name'), /^scene-\d+\.json$/)
})

test('3D 页真的接上了这三个路由，且本地与后端载入共用一条解析路径', () => {
  assert.match(stage, /fetch\("\/api\/scene-archives"/)
  assert.match(stage, /fetch\(`\/api\/scene-archives\$\{query\}`\)/)
  assert.match(stage, /fetch\(`\/api\/scene-archives\/\$\{item\.id\}`\)/)
  assert.match(stage, /存到后端/)
  assert.match(stage, /后端存档/)
  // 本地文件与后端存档都必须走 importSceneText，否则校验逻辑会分叉。
  // 定义行是 `const importSceneText = useCallback(`，不算调用；这里应正好 2 个调用点。
  const callers = stage.match(/importSceneText\(/g) ?? []
  assert.equal(callers.length, 2, `importSceneText 调用点有 ${callers.length} 个`)
})