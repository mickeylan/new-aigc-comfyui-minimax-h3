import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync, readdirSync } from 'node:fs'

const css = readFileSync(new URL('../src/styles/main.css', import.meta.url), 'utf8')

function rule(selector) {
  const match = css.match(new RegExp(`(^|\\n)${selector}\\s*\\{([^}]*)\\}`))
  return match ? match[2] : ''
}
function pixels(declarations, property) {
  const match = declarations.match(new RegExp(`${property}:\\s*(\\d+)px`))
  return match ? Number(match[1]) : null
}

test('.btn-xs 尺寸类有定义，且比 .btn-sm、.btn 更小', () => {
  const xs = rule('\\.btn-xs')
  const sm = rule('\\.btn-sm')
  const base = rule('\\.btn')
  assert.ok(xs, '.btn-xs 必须定义在 main.css，否则各页面用它的按钮会退回 .btn 的原始尺寸')
  assert.ok(sm && base)
  assert.ok(pixels(xs, 'padding') < pixels(sm, 'padding'))
  assert.ok(pixels(xs, 'padding') < pixels(base, 'padding'))
  assert.ok(pixels(xs, 'font-size') <= pixels(sm, 'font-size'))
  assert.ok(pixels(xs, 'font-size') < pixels(base, 'font-size'))
})

test('.btn-xs 只作为尺寸修饰类使用，始终与 .btn 同时出现', () => {
  const files = [
    ...readdirSync(new URL('../src/views/', import.meta.url)).filter(name => name.endsWith('.vue')).map(name => `../src/views/${name}`),
    ...readdirSync(new URL('../src/components/', import.meta.url)).filter(name => name.endsWith('.vue')).map(name => `../src/components/${name}`),
  ]
  let usages = 0
  for (const file of files) {
    const source = readFileSync(new URL(file, import.meta.url), 'utf8')
    for (const match of source.matchAll(/class="([^"]*btn-xs[^"]*)"/g)) {
      usages += 1
      assert.match(match[1], /(^|\s)btn(\s|$)/, `${file} 的 .btn-xs 必须和 .btn 一起用：${match[1]}`)
    }
  }
  assert.ok(usages > 0, '至少应有一个按钮使用 .btn-xs')
})