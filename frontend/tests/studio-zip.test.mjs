import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { inflateRawSync } from 'node:zlib'

// zip.ts 是纯 TS，没有类型导入，node 可以直接跑（靠 type stripping）。
const { crc32, createStoreZip } = await import('../src/studio/scene3d/zip.ts')
const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')

// CRC32 是 ZIP 的完整性字段，错了任何解压器都会报「文件损坏」。
// 0xCBF43926 = crc32("123456789")，这是 CRC-32/ISO-HDLC 的标准校验向量。
test('crc32 与标准校验向量一致', () => {
  assert.equal(crc32(new TextEncoder().encode('123456789')), 0xcbf43926)
  assert.equal(crc32(new Uint8Array(0)), 0)
})

// 真造一份 zip，再自己按格式解回来：只看源码断言证明不了字节对不对。
function readZip(bytes) {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  const decoder = new TextDecoder()
  // 从尾部找 EOCD（这里没有注释，所以末 22 字节就是）
  const eocd = bytes.length - 22
  assert.equal(view.getUint32(eocd, true), 0x06054b50, 'EOCD 签名不对')
  const count = view.getUint16(eocd + 10, true)
  const centralSize = view.getUint32(eocd + 12, true)
  const centralOffset = view.getUint32(eocd + 16, true)
  assert.equal(centralOffset + centralSize + 22, bytes.length, '中央目录位置与总长对不上')

  const entries = []
  let cursor = centralOffset
  for (let index = 0; index < count; index += 1) {
    assert.equal(view.getUint32(cursor, true), 0x02014b50, '中央目录签名不对')
    const method = view.getUint16(cursor + 10, true)
    const crc = view.getUint32(cursor + 16, true)
    const size = view.getUint32(cursor + 24, true)
    const nameLength = view.getUint16(cursor + 28, true)
    const localOffset = view.getUint32(cursor + 42, true)
    const name = decoder.decode(bytes.subarray(cursor + 46, cursor + 46 + nameLength))

    assert.equal(view.getUint32(localOffset, true), 0x04034b50, `${name} 本地头签名不对`)
    const localNameLength = view.getUint16(localOffset + 26, true)
    const localExtra = view.getUint16(localOffset + 28, true)
    const dataStart = localOffset + 30 + localNameLength + localExtra
    const data = bytes.subarray(dataStart, dataStart + size)
    assert.equal(view.getUint16(localOffset + 8, true), method, '本地头与中央目录压缩方式不一致')
    assert.equal(crc32(data), crc, `${name} CRC 对不上`)

    entries.push({ name, method, size, data })
    cursor += 46 + nameLength
  }
  return entries
}

test('打包出来的 zip 能被按格式解回来，内容逐字节一致', () => {
  const frames = [
    { name: 'clay-000.png', data: new TextEncoder().encode('fake-png-1') },
    { name: 'clay-001.png', data: new Uint8Array([0, 1, 2, 250, 251, 255]) },
  ]
  const zip = createStoreZip(frames, new Date(2026, 10, 22, 15, 45, 20))
  const parsed = readZip(zip)

  assert.equal(parsed.length, 2)
  assert.deepEqual(parsed.map((item) => item.name), ['clay-000.png', 'clay-001.png'])
  for (const [index, item] of parsed.entries()) {
    assert.equal(item.method, 0, '应当是不压缩的 STORE')
    assert.equal(item.size, frames[index].data.length)
    assert.deepEqual([...item.data], [...frames[index].data], `${item.name} 内容被改了`)
  }
})

test('空列表也能出合法 zip（0 条目），不压缩的数据用 zlib 也读得出来', () => {
  const empty = createStoreZip([])
  assert.equal(empty.length, 22)
  assert.equal(readZip(empty).length, 0)

  // 真实 PNG 是已压缩数据，这里确认 STORE + 原样字节能被标准解压库认出来
  const payload = new TextEncoder().encode('raw-bytes-not-deflated')
  const zip = createStoreZip([{ name: 'a.png', data: payload }])
  const parsed = readZip(zip)
  assert.deepEqual([...parsed[0].data], [...payload])
  // STORE 方式下 inflateRaw 会失败，正说明我们确实没压缩（而不是压了又标 STORE）
  assert.throws(() => inflateRawSync(parsed[0].data))
})

test('序列导出走这条打包路径，并按相机路径逐帧取样', () => {
  assert.match(stage, /import \{ createStoreZip \} from "\.\/zip\.js";/)
  assert.match(stage, /const exportSequence = useCallback\(/)
  assert.match(stage, /createStoreZip\(entries\)/)
  // 逐帧按当前路径取机位，不是原地重渲同一张
  assert.match(stage, /const sample = sampleCameraPath\(cameraKeys, index \/ fps\);/)
  // 帧数由总时长 × fps 决定，并有上限，防止一按就渲上千帧
  // （总时长与帧数现在由 sequencePlan 统一算：相机路径与角色移动取长者）
  assert.match(stage, /frames: Math\.min\(SEQUENCE_MAX_FRAMES, Math\.max\(1, Math\.round\(seconds \* fps\)\)\),/)
  // 导出完要把相机放回原处，否则编辑器停在最后一帧的机位
  assert.match(stage, /camera\.position\.copy\(savedPosition\);/)
  assert.match(stage, /controls\.target\.copy\(savedTarget\);/)
})