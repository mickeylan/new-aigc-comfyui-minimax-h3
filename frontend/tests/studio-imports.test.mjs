import test from 'node:test'
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync, statSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

// 提交 C4 时漏掉了 frontend/src/studio/host/mount-pagedraw.tsx：文件在本地工作区
// 存在，`vite build` 也能过，但提交后 checkout 就是缺文件。这类「本地绿、提交红」
// 的缺口只有对着 git 索引检查才能发现，所以这里直接扫已跟踪文件的相对 import。

const frontendDir = resolve(dirname(fileURLToPath(import.meta.url)), '..')

let tracked
try {
  tracked = execFileSync('git', ['ls-files', 'src', 'tests'], {
    cwd: frontendDir,
    encoding: 'utf8',
  })
    .split(/\r?\n/)
    .filter(Boolean)
} catch {
  test('导入完整性检查', { skip: 'git 不可用，跳过（打包产物里不存在该问题）' }, () => {})
}

if (tracked) {
  const trackedSet = new Set(tracked.map((p) => p.replace(/\\/g, '/')))
  const exts = ['', '.ts', '.tsx', '.js', '.jsx', '.mjs', '.vue']
  const specRe = /(?:from|import)\s*\(?\s*['"]([^'"]+)['"]/g

  const resolveRelative = (file, spec) => {
    const base = resolve(frontendDir, dirname(file), spec)
    for (const ext of exts) {
      const candidate = base + ext
      if (existsSync(candidate) && statSync(candidate).isFile()) return candidate
    }
    for (const ext of ['/index.ts', '/index.tsx', '/index.js', '/index.vue']) {
      if (existsSync(base + ext)) return base + ext
    }
    if (spec.endsWith('.js')) {
      const alt = base.replace(/\.js$/, '.ts')
      if (existsSync(alt)) return alt
    }
    return null
  }

  test('已跟踪文件的相对 import 都指向已跟踪文件', () => {
    const problems = []
    for (const file of tracked) {
      if (!/\.(ts|tsx|js|jsx|mjs|vue)$/.test(file)) continue
      const src = readFileSync(resolve(frontendDir, file), 'utf8')
      for (const m of src.matchAll(specRe)) {
        const spec = m[1]
        if (!spec.startsWith('.')) continue
        // Vite 的后缀查询（?worker&inline 等）不是文件路径，跳过。
        if (spec.includes('?')) continue
        const hit = resolveRelative(file, spec)
        if (!hit) {
          problems.push(`缺失：${file} -> ${spec}`)
          continue
        }
        const rel = hit.slice(frontendDir.length + 1).replace(/\\/g, '/')
        if (!trackedSet.has(rel)) problems.push(`未跟踪：${file} -> ${rel}`)
      }
    }
    assert.deepEqual(problems, [], `有 import 指向不存在或未提交的文件：\n${problems.join('\n')}`)
  })
}