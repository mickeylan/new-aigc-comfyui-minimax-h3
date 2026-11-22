import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')

// 泼溅进不了 GLB，只能把 .spz 原始字节单独交进素材库，并配一张当前画面当缩略图。
const saveFn = stage.match(/const saveSplatToLibrary = useCallback\(([\s\S]*?)\n  \}, \[captureThumbnail\]\);/)
// 缩略图改走 captureThumbnail（先等泼溅真画出来、藏 gizmo 再截），不再直取 canvas。
test('泼溅单独入库：取原始字节，按 type=3d 交给 /api/materials', () => {
  assert.ok(saveFn, '缺少 saveSplatToLibrary')
  const body = saveFn[1]
  assert.match(body, /await fetch\(entry\.splatUrl\)/, '要按来源地址取字节')
  assert.match(body, /const bytes = await source\.arrayBuffer\(\);/)
  assert.match(body, /form\.append\("file", new Blob\(\[bytes\]/, '原样交上去，不重新编码')
  assert.match(body, /form\.append\("type", "3d"\)/)
  assert.match(body, /fetch\("\/api\/materials", \{ method: "POST", body: form \}\)/)
})

test('入库带一张当前渲染帧当缩略图', () => {
  const body = saveFn[1]
  assert.match(body, /await captureThumbnail\(\)/, '缩略图要走等首帧的助手')
  assert.match(body, /if \(thumbnail\) form\.append\("thumbnail", thumbnail, "thumb\.png"\)/)
})

test('没有来源地址就不提交，避免存进一个空壳素材', () => {
  const body = saveFn[1]
  assert.match(body, /if \(!entry\.splatUrl\) \{/)
  assert.match(body, /没有来源地址，存不了素材库/)
  // 先判断后退，不能先发请求
  assert.ok(
    body.indexOf('if (!entry.splatUrl)') < body.indexOf('await fetch(entry.splatUrl)'),
    '地址校验要在请求之前',
  )
})

test('文件名归一：认得的泼溅后缀保留，其余补 .spz', () => {
  const helper = stage.match(/function splatFileName\(name: string\): string \{([\s\S]*?)\n\}/)
  assert.ok(helper, '缺少 splatFileName')
  const run = new Function('name', helper[1])
  assert.equal(run('world-1700.spz'), 'world-1700.spz')
  assert.equal(run('scan.splat'), 'scan.splat')
  assert.equal(run('scan.KSPLAT'), 'scan.KSPLAT')
  assert.equal(run('splat-abc123'), 'splat-abc123.spz')
  assert.equal(run('点云.ply'), '点云.ply.spz', 'PLY 不在素材库白名单里，补后缀改成 .spz')
})

test('对象列表只给泼溅加「存库」按钮，网格行不变', () => {
  assert.match(stage, /\{entry\.splat \? \(/, '按钮要按 entry.splat 决定是否出现')
  assert.match(stage, /void saveSplatToLibrary\(entry\)/)
  // 一行里不能出现按钮套按钮：行本身已改成 div
  assert.match(stage, /className=\{`mb-1 flex items-center gap-1 rounded-md/)
})

test('混合场景存 GLB 时说明泼溅层被跳过', () => {
  assert.match(stage, /层泼溅进不了 GLB，需逐层「存库」/)
})