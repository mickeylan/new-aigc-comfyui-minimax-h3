/**
 * 极简 ZIP 打包：只做 STORE（不压缩）。
 *
 * 参考图序列是一堆 PNG，本身已经压过，再 deflate 收益很小；用不着为这个引依赖，
 * 手写 local header + central directory + EOCD 就够了（~60 行，能单测）。
 */

export interface ZipEntry {
  name: string;
  data: Uint8Array;
}

const CRC_TABLE = (() => {
  const table = new Uint32Array(256);
  for (let index = 0; index < 256; index += 1) {
    let value = index;
    for (let bit = 0; bit < 8; bit += 1) {
      value = value & 1 ? 0xedb88320 ^ (value >>> 1) : value >>> 1;
    }
    table[index] = value >>> 0;
  }
  return table;
})();

export function crc32(data: Uint8Array): number {
  let crc = 0xffffffff;
  for (let index = 0; index < data.length; index += 1) {
    crc = CRC_TABLE[(crc ^ data[index]) & 0xff] ^ (crc >>> 8);
  }
  return (crc ^ 0xffffffff) >>> 0;
}

/** DOS 时间/日期，ZIP 头里只能放这个格式。 */
function dosDateTime(date: Date) {
  const time =
    (date.getHours() << 11) | (date.getMinutes() << 5) | Math.floor(date.getSeconds() / 2);
  const day =
    ((date.getFullYear() - 1980) << 9) | ((date.getMonth() + 1) << 5) | date.getDate();
  return { time: time & 0xffff, date: day & 0xffff };
}

export function createStoreZip(entries: ZipEntry[], now = new Date()): Uint8Array {
  const encoder = new TextEncoder();
  const { time, date } = dosDateTime(now);
  const locals: Uint8Array[] = [];
  const centrals: Uint8Array[] = [];
  let offset = 0;

  for (const entry of entries) {
    const nameBytes = encoder.encode(entry.name);
    const crc = crc32(entry.data);
    const size = entry.data.length;

    const local = new Uint8Array(30 + nameBytes.length + size);
    const localView = new DataView(local.buffer);
    localView.setUint32(0, 0x04034b50, true); // 本地文件头签名
    localView.setUint16(4, 20, true); // 需要的版本
    localView.setUint16(6, 0, true); // 通用标志
    localView.setUint16(8, 0, true); // 压缩方式 0 = STORE
    localView.setUint16(10, time, true);
    localView.setUint16(12, date, true);
    localView.setUint32(14, crc, true);
    localView.setUint32(18, size, true); // 压缩后大小
    localView.setUint32(22, size, true); // 原始大小
    localView.setUint16(26, nameBytes.length, true);
    localView.setUint16(28, 0, true); // extra 长度
    local.set(nameBytes, 30);
    local.set(entry.data, 30 + nameBytes.length);
    locals.push(local);

    const central = new Uint8Array(46 + nameBytes.length);
    const centralView = new DataView(central.buffer);
    centralView.setUint32(0, 0x02014b50, true); // 中央目录签名
    centralView.setUint16(4, 20, true); // 打包工具版本
    centralView.setUint16(6, 20, true); // 需要的版本
    centralView.setUint16(8, 0, true);
    centralView.setUint16(10, 0, true); // STORE
    centralView.setUint16(12, time, true);
    centralView.setUint16(14, date, true);
    centralView.setUint32(16, crc, true);
    centralView.setUint32(20, size, true);
    centralView.setUint32(24, size, true);
    centralView.setUint16(28, nameBytes.length, true);
    centralView.setUint16(30, 0, true); // extra
    centralView.setUint16(32, 0, true); // 注释
    centralView.setUint16(34, 0, true); // 磁盘号
    centralView.setUint16(36, 0, true); // 内部属性
    centralView.setUint32(38, 0, true); // 外部属性
    centralView.setUint32(42, offset, true); // 本地头偏移
    central.set(nameBytes, 46);
    centrals.push(central);

    offset += local.length;
  }

  const centralSize = centrals.reduce((sum, item) => sum + item.length, 0);
  const eocd = new Uint8Array(22);
  const eocdView = new DataView(eocd.buffer);
  eocdView.setUint32(0, 0x06054b50, true);
  eocdView.setUint16(4, 0, true); // 当前磁盘
  eocdView.setUint16(6, 0, true); // 中央目录起始磁盘
  eocdView.setUint16(8, entries.length, true); // 本盘条目数
  eocdView.setUint16(10, entries.length, true); // 总条目数
  eocdView.setUint32(12, centralSize, true);
  eocdView.setUint32(16, offset, true); // 中央目录偏移
  eocdView.setUint16(20, 0, true); // 注释长度

  const total = offset + centralSize + eocd.length;
  const output = new Uint8Array(total);
  let cursor = 0;
  for (const chunk of [...locals, ...centrals, eocd]) {
    output.set(chunk, cursor);
    cursor += chunk.length;
  }
  return output;
}