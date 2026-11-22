import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')

// 泼溅细节靠 Spark 的 LoD 上限调节：大场景吃显存时降档，不必重载场景。
test('细节档位有四个可选值，默认取「均衡」', () => {
  const block = stage.match(/const SPLAT_DETAIL_PRESETS = \[([\s\S]*?)\] as const;/)
  assert.ok(block, '缺少 SPLAT_DETAIL_PRESETS')
  const counts = [...block[1].matchAll(/count: ([\d_]+)/g)].map((m) => Number(m[1].replace(/_/g, '')))
  assert.deepEqual(counts, [500000, 1000000, 2000000, 5000000], '档位与顺序不该漂')
  assert.match(stage, /useState<number>\(SPLAT_DETAIL_PRESETS\[1\]\.count\)/, '默认档位应是第二项')
})

test('初始化时把档位交给 SparkRenderer，之后改档位热更新实例属性', () => {
  assert.match(stage, /createSplatRenderer\(renderer, \{[\s\S]{0,120}lodSplatCount: splatDetailRef\.current/)
  assert.match(stage, /if \(spark\) spark\.lodSplatCount = splatDetail;/)
  // 改档位不能重跑初始化 effect，否则整个场景重建
  assert.match(stage, /\}, \[splatDetail\]\);/, '档位变化应走独立 effect')
})

test('右栏有「泼溅细节」四个按钮，点了改档位', () => {
  assert.match(stage, />泼溅细节</)
  assert.match(stage, /SPLAT_DETAIL_PRESETS\.map\(/)
  assert.match(stage, /onClick=\{\(\) => setSplatDetail\(preset\.count\)\}/)
  assert.match(stage, /splatDetail === preset\.count/)
})

// LoD 打开后首帧要等 Spark 把细节树建完，大泼溅会空白几秒，必须给提示。
test('泼溅首次出现前挂「正在建细节层」提示，建完由 onDirty 收掉', () => {
  assert.match(stage, /const SPLAT_LOD_BUSY = "正在建泼溅细节层…"/)
  // 放素材入库与载入存档两条路径都要挂
  assert.match(stage, /setBusy\(SPLAT_LOD_BUSY\);/, '缺挂忙态的地方')
  assert.equal(
    (stage.match(/setBusy\(SPLAT_LOD_BUSY\)/g) || []).length,
    2,
    '放入泼溅与载入存档各一处',
  )
  // onDirty 只在正是这条提示时清，别把别的 busy 顺手抹掉
  assert.match(
    stage,
    /onDirty: \(\) => \{[\s\S]{0,200}setBusy\(\(current\) => \(current === SPLAT_LOD_BUSY \? null : current\)\)/,
  )
  // 两个 finally 都不能提前把泼溅的忙态清成 null
  assert.equal(
    (stage.match(/setBusy\(\(current\) => \(current === SPLAT_LOD_BUSY \? current : null\)\)/g) || []).length,
    2,
    '放入素材与载入存档的 finally 各一处守卫',
  )
})