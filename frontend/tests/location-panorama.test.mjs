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

test('全景接口使用 multipart 并携带视角数', () => {
  assert.match(api, /splitLocationPanorama: \(id, aid, file, viewCount = 6\) =>/)
  assert.match(api, /fd\.append\('view_count', String\(viewCount\)\)/)
  assert.match(api, /`\/projects\/\$\{id\}\/assets\/location\/\$\{aid\}\/panorama`/)
})
