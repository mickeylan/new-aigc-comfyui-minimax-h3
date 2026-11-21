import test from 'node:test'
import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

// 主站界面的装饰性小标题（overline / eyebrow）必须是中文。
// 品牌名、模型名与通用缩写按约定保留原文。

const viewsDir = resolve(dirname(fileURLToPath(import.meta.url)), '../src/views')

const allowed = new Set(['COMFYSTUDIO', 'MINIMAX H3', 'QWEN-IMAGE-2.1', 'BGM', 'SRT'])

test('主站 Vue 视图里没有残留的英文小标题', () => {
  const offenders = []
  for (const file of readdirSync(viewsDir)) {
    if (!file.endsWith('.vue')) continue
    const src = readFileSync(resolve(viewsDir, file), 'utf8')
    for (const m of src.matchAll(/(?:overline|eyebrow)"[^>]*>\s*([A-Za-z][A-Za-z0-9 &/.-]{2,})</g)) {
      const label = m[1].trim()
      if (allowed.has(label)) continue
      offenders.push(`${file}: ${label}`)
    }
  }
  assert.deepEqual(offenders, [], `仍是英文的小标题：\n${offenders.join('\n')}`)
})