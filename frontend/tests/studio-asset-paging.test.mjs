import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const panel = readFileSync(new URL('../src/studio/scene3d/AssetLibraryPanel.tsx', import.meta.url), 'utf8')
const service = readFileSync(
  new URL('../../backend/internal/service/material_service.go', import.meta.url),
  'utf8',
)
const handlers = readFileSync(
  new URL('../../backend/internal/service/project_handlers.go', import.meta.url),
  'utf8',
)

// 素材越存越多：面板只拉一页，后端要能按页返回；但老调用方（预演、画布）不能被改坏。
test('后端支持 page/page_size，缺省时仍返回全量', () => {
  assert.match(service, /func \(m \*MaterialService\) ListPaged\(ftype string, projectID \*uint, page, pageSize int, filters \.\.\.string\)/)
  // 不分页的老调用方必须还能拿到全部
  assert.match(service, /return m\.ListPaged\(ftype, projectID, 1, 0\)/)
  assert.match(service, /if pageSize > 0 \{/)
  assert.match(service, /q = q\.Limit\(pageSize\)\.Offset\(\(page - 1\) \* pageSize\)/)
  // page=0/负数要夹到第一页，别让 Offset 变负
  assert.match(service, /if page < 1 \{\s*\n\s*page = 1/)
  // 新素材在第一页
  assert.match(service, /Order\("id desc"\)/)
})

test('列表接口把 page/page_size 传下去，非法值不炸', () => {
  assert.match(handlers, /parseIntQuery\(c, "page"\), parseIntQuery\(c, "page_size"\)/)
  const helper = handlers.match(/func parseIntQuery\(c \*gin\.Context, name string\) int \{([\s\S]*?)\n\}/)
  assert.ok(helper, '缺 parseIntQuery')
  assert.match(helper[1], /if v == "" \{\s*\n\s*return 0/, '缺省返回 0 = 不分页')
  assert.match(helper[1], /if err != nil \{\s*\n\s*return 0/, '非法值也按未指定处理')
})

test('面板按页拉 24 条，能追加加载更多', () => {
  assert.match(panel, /const ASSET_PAGE_SIZE = 24;/)
  assert.match(panel, /\/api\/materials\?type=3d&page=\$\{target\}&page_size=\$\{ASSET_PAGE_SIZE\}/)
  // 刷新回到第一页并替换列表；加载更多是追加
  assert.match(panel, /const list = await fetchPage\(1\);/)
  assert.match(panel, /setItems\(\(current\) => \[\.\.\.current, \.\.\.list\]\)/)
  // 只有"这一页塞满了"才可能还有更多
  assert.match(panel, /setHasMore\(list\.length >= ASSET_PAGE_SIZE\)/)
  assert.match(panel, /加载更多/)
})

// 素材库页面原来一次拉全量，素材多起来会卡；跟 3D 面板同一套分页
test('素材库页面也按页拉取，能加载更多', () => {
  const view = readFileSync(new URL('../src/views/Materials.vue', import.meta.url), 'utf8')
  assert.match(view, /const PAGE_SIZE = 48/i, '缺页大小')
  assert.match(view, /const params = \{ page: target, page_size: PAGE_SIZE \}/, '请求要带分页参数')
  assert.match(view, /materials\.value = \[\.\.\.materials\.value, \.\.\.list\]/, '加载更多应追加')
  assert.match(view, /hasMore\.value = list\.length >= PAGE_SIZE/, '按本页是否塞满推断还有更多')
  assert.match(view, /加载更多/)
  // 未传 page_size 的老后端会一次给全量，这条退路不能被破坏
  assert.match(view, /Array\.isArray\(data\) \? data : \(data\.items \?\? \[\]\)/, '两种返回结构都要认')
})