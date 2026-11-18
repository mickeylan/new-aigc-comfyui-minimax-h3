import { validateEnvironment } from './previsEnvironment.js'
import { validateTimeline } from './previsTimeline.js'
export const clonePrevis = value => JSON.parse(JSON.stringify(value))
export function createPrevis(names = []) {
  return { schema_version: 1, objects: [...new Set(names)].slice(0, 32).map((name, i) => ({ id: `character-${i + 1}`, name, kind: 'character', position: { x: i * 2 - 2, y: 0, z: 0 }, yaw: 0, width: 0.6, height: 1.7, depth: 0.4 })), camera: { position: { x: 7, y: 5, z: 9 }, target: { x: 0, y: 1, z: 0 }, fov: 45 }, views: [] }
}
const coordinate = point => `X${Number(point.x.toFixed(2))}、Y${Number(point.y.toFixed(2))}、Z${Number(point.z.toFixed(2))}米`
function spatialFacing(o, doc) {
  const angle = o.yaw * Math.PI / 180
  const fx = Math.sin(angle), fz = Math.cos(angle)
  const dx = doc.camera.position.x - o.position.x, dz = doc.camera.position.z - o.position.z
  const distance = Math.hypot(dx, dz)
  const dot = distance > 0.01 ? (fx * dx + fz * dz) / distance : null
  const relative = dot === null ? '镜头近垂直俯视，不能判定正背面' : dot > 0.7 ? '相对镜头正面' : dot < -0.7 ? '相对镜头背面' : Math.abs(dot) < 0.3 ? '相对镜头侧面' : dot > 0 ? '相对镜头三分之二侧面' : '相对镜头侧背面'
  const ahead = doc.objects.filter(v => v.id !== o.id).map(v => { const x=v.position.x-o.position.x,z=v.position.z-o.position.z,d=Math.hypot(x,z); return {name:v.name,d,alignment:d>0.01?(fx*x+fz*z)/d:0} }).filter(v=>v.alignment>0.9).sort((a,b)=>a.d-b.d)[0]
  return `世界Y轴朝向${o.yaw}°（0°面向+Z，90°面向+X）；${ahead ? `几何上朝向${ahead.name}所在方向（非视线或互动事实）` : '朝向该世界方位，未指定互动对象'}；${relative}`
}
export function mergePrevisBlocking(blocking, suggestion) {
  const draft = clonePrevis(blocking)
  draft.characters ||= []
  for (const character of suggestion.characters) {
    const current = draft.characters.find(c=>c.name===character.name)
    if (current) { current.position=character.position; current.facing=character.facing }
    else draft.characters.push({...character})
  }
  draft.camera=suggestion.camera
  draft.suggestions=[...new Set([...(draft.suggestions || []),...suggestion.suggestions])]
  return draft
}
export function previsSuggestion(doc) {
  return { characters: doc.objects.filter(o => o.kind === 'character').map(o => ({ name: o.name, position: coordinate(o.position), facing: spatialFacing(o, doc), gaze: '', visibility: '' })), camera: `机位 ${coordinate(doc.camera.position)}；朝向 ${coordinate(doc.camera.target)}；视角${doc.camera.fov}°`, suggestions: doc.objects.map(o => `【3D预演建议，非原文事实】${o.name}位于${coordinate(o.position)}，朝向${o.yaw}°，尺寸${o.width}×${o.height}×${o.depth}米`) }
}
export function validatePrevis(doc) {
  const finite = (v, min, max) => Number.isFinite(v) && v >= min && v <= max
  const vector = p => p && ['x', 'y', 'z'].every(k => finite(p[k], -100, 100))
  const camera = c => c && vector(c.position) && vector(c.target) && finite(c.fov, 15, 100) && ['x', 'y', 'z'].reduce((s, k) => s + (c.position[k] - c.target[k]) ** 2, 0) > 0.0001
  if (![1,2,3].includes(doc?.schema_version) || !Array.isArray(doc.objects) || doc.objects.length > 32 || !Array.isArray(doc.views) || doc.views.length > 12 || !camera(doc.camera)) return '预演版本、对象数量或机位无效'
  const ids = new Set(), names = new Set()
  for (const o of doc.objects) {
    if (!o.id || o.id.length > 80 || ids.has(o.id) || !o.name?.trim() || [...o.name].length > 80 || !['character', 'prop', 'wall', 'building', 'tree', 'plant'].includes(o.kind) || !vector(o.position) || !finite(o.yaw, -360, 360) || !['width', 'height', 'depth'].every(k => finite(o[k], 0.05, 30))) return '对象身份、名称、坐标或尺寸无效'
    ids.add(o.id)
  }
  for (const v of doc.views) {
    if (!v.name?.trim() || [...v.name].length > 80 || names.has(v.name) || !camera(v.camera)) return '命名机位重复或无效'
    names.add(v.name)
  }
  if(doc.schema_version<3&&(doc.weather||doc.models?.length))return '模型与天气需要预演v3'
  return validateTimeline(doc)||validateEnvironment(doc)
}
