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

test('生成后的全景原图可在场景卡上 360° 拖动查看，并保留查看平铺原图', () => {
  assert.match(detail, /v-if="a\.kind === 'location' && a\.panorama_image"[^>]*@click="viewLocationPanorama\(a\)"/)
  const fn = detail.slice(detail.indexOf('function viewLocationPanorama('), detail.indexOf('async function genLocationPanorama('))
  assert.match(fn, /panoramaView\.value = \{ src: api\.inputUrl\(project\.value\.id, a\.panorama_image\)/)
  assert.match(fn, /function viewPanoramaOriginal\(\) \{\s*viewer\.value = panoramaView\.value\?\.src/)
  const modal = detail.slice(detail.indexOf('<!-- 360° 全景查看'), detail.indexOf('<!-- 图片查看弹窗 -->'))
  assert.match(modal, /<PanoramaViewer :src="panoramaView\.src" \/>/)
  assert.match(modal, /@click="viewPanoramaOriginal"[^<]*查看原图/)
  // 原图大图弹窗写在全景弹窗之后，同层级时盖在上方，关闭后回到全景。
  assert.ok(detail.indexOf('<!-- 360° 全景查看') < detail.indexOf('<!-- 图片查看弹窗 -->'))
})

test('参考图版本逐条显示缩略图，点击打开大图，拆出的视角可直接核对', () => {
  const modal = detail.slice(detail.indexOf('<!-- 资产图片版本 -->'), detail.indexOf('<GeneratedMediaHistoryDrawer'))
  assert.match(modal, /<img :src="api\.inputUrl\(id\(\), variant\.file\)" class="variant-thumb"[^>]*@click="viewVariant\(variant\)"/)
  assert.match(modal, /variant\.prompt \|\| variant\.file/)
  const fn = detail.slice(detail.indexOf('function viewVariant('), detail.indexOf('async function selectVariant('))
  assert.match(fn, /viewer\.value = api\.inputUrl\(id\(\), variant\.file\)/)
  // 大图弹窗写在版本弹窗之后，同层级时盖在上方，关闭后回到版本列表。
  assert.ok(detail.indexOf('<!-- 图片查看弹窗 -->') > detail.indexOf('<!-- 资产图片版本 -->'))
})

test('生成全景接口只提交任务，不上传文件', () => {
  assert.match(api, /generateLocationPanorama: \(id, aid\) => http\.post\(`\/projects\/\$\{id\}\/assets\/location\/\$\{aid\}\/panorama\/generate`\)/)
})

test('全景接口使用 multipart 并携带视角数', () => {
  assert.match(api, /splitLocationPanorama: \(id, aid, file, viewCount = 6\) =>/)
  assert.match(api, /fd\.append\('view_count', String\(viewCount\)\)/)
  assert.match(api, /`\/projects\/\$\{id\}\/assets\/location\/\$\{aid\}\/panorama`/)
})
