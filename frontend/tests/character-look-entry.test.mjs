import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const looks = readFileSync(new URL('../src/views/CharacterLooks.vue', import.meta.url), 'utf8')
const projectDetail = detail

test('角色卡提供明显的新形象和完整套装入口', () => {
  assert.match(detail, /基于标准像换装/)
  assert.match(detail, /AI换装设计/)
  assert.match(detail, /looks\?mode=outfits&design=1/)
  assert.match(detail, /管理完整套装/)
})

test('角色造型页支持从链接直接打开新资产表单并说明使用流程', () => {
  assert.match(looks, /r\.query\.design==='1'/)
  assert.match(looks, /基于角色标准像设计新形象/)
  assert.match(looks, /身份锚点：/)
  assert.match(looks, /Picture 1 固定使用这张标准像/)
  assert.match(looks, /characterPayload\?\.characters/)
  assert.doesNotMatch(looks, /\(characters\|\|\[\]\)\.find/)
  assert.match(looks, /当前角色不存在或不属于该项目/)
  assert.match(looks, /推荐流程/)
  assert.match(looks, /本场角色造型套装/)
})

test('角色设计卡使用主信息分区和宽卡布局', () => {
  assert.match(projectDetail, /class="character-card-main"/)
  assert.match(projectDetail, /\.character-grid\s*\{[^}]*minmax\(520px, 1fr\)/s)
  assert.match(projectDetail, /\.character-card-main\s*\{[^}]*grid-template-columns:\s*132px/s)
  assert.match(projectDetail, /\.character-look-entry\s*\{[^}]*grid-template-columns/s)
})

test('场景设计按可见角色加载新形象套装', () => {
  assert.match(projectDetail, /character_roles_set\?\(sc\?\.visible_characters/)
  assert.match(projectDetail, /api\.characterOutfits\(id\(\), ch\.id\)/)
  assert.match(projectDetail, /const outfitAssignments = Object\.entries\(selectedSceneOutfits\.value\)/)
  const saveOutfit = projectDetail.indexOf('await api.updateSceneOutfits(id(), editingScene.value.id, outfitAssignments)')
  const generatePrompt = projectDetail.indexOf('await api.redesignScenePrompt(id(), editingScene.value.id')
  assert.ok(saveOutfit >= 0 && saveOutfit < generatePrompt, 'AI提示词生成前必须先保存当前新形象套装')
})

test('新形象图片支持点击查看大图', () => {
  assert.match(looks, /openViewer\(o\.image/)
  assert.match(looks, /openViewer\(o\.sheet/)
  assert.match(looks, /class="look-viewer-mask"/)
  assert.match(looks, /role="dialog"/)
  assert.match(looks, /event\.key==='Escape'/)
  assert.match(looks, /用换装照生成 Krea2 四视图/)
  assert.match(looks, /Krea2 生成四视图中/)
})
