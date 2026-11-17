import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('只有场景资产卡提供全景图拆视角入口，并说明当前参考图不变', () => {
  assert.match(detail, /v-if="a\.kind === 'location'"[^>]*@click="splitPanorama\(a\)"/)
  assert.match(detail, /ERP）360° 全景图[^"]*当前参考图不变/)
})

test('拆出的视角只进入参考图版本，由用户在版本弹窗中选用', () => {
  const fn = detail.slice(detail.indexOf('function splitPanorama('), detail.indexOf('function uploadAssetImage('))
  assert.match(fn, /api\.splitLocationPanorama\(id\(\), a\.id, f, Number\(raw\.trim\(\)\) \|\| 6\)/)
  assert.match(fn, /openVariants\('asset_image', a\.id/)
  assert.doesNotMatch(fn, /selectAssetVariant/)
})

test('场景资产卡可用 MiniMax H3-Image 生成全景图，进行中禁用并显示错误', () => {
  assert.match(detail, /v-if="a\.kind === 'location'"[^>]*:disabled="busy \|\| !!a\.panorama_task_id"[^>]*@click="genLocationPanorama\(a\)"/)
  assert.match(detail, /MiniMax H3-Image[^"]*自动拆成 6 个视角[^"]*当前参考图不变[^"]*核对「正后方」视角/)
  assert.match(detail, /a\.panorama_task_id \? '全景图生成中…' : '生成全景图'/)
  assert.match(detail, /v-if="a\.kind === 'location' && a\.panorama_error"/)
  const fn = detail.slice(detail.indexOf('async function genLocationPanorama('), detail.indexOf('function splitPanorama('))
  assert.match(fn, /api\.generateLocationPanorama\(id\(\), a\.id\)/)
  assert.doesNotMatch(fn, /selectAssetVariant/)
})

test('生成全景接口只提交任务，不上传文件', () => {
  assert.match(api, /generateLocationPanorama: \(id, aid\) => http\.post\(`\/projects\/\$\{id\}\/assets\/location\/\$\{aid\}\/panorama\/generate`\)/)
})

test('全景接口使用 multipart 并携带视角数', () => {
  assert.match(api, /splitLocationPanorama: \(id, aid, file, viewCount = 6\) =>/)
  assert.match(api, /fd\.append\('view_count', String\(viewCount\)\)/)
  assert.match(api, /`\/projects\/\$\{id\}\/assets\/location\/\$\{aid\}\/panorama`/)
})
