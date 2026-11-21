// 导出目的地的可用性判定。单独放成纯函数，是因为宿主没有媒体库时
// 仍然默认勾选「Save to my media library」，导出结束必然报
// "Library upload failed"，用户会以为导出失败（其实文件已存盘）。

/**
 * @param {{ supportsLibrary?: boolean, saveToDisk?: boolean, saveToLibrary?: boolean }} input
 *   supportsLibrary 来自宿主提供的 export sink；未声明视为不支持。
 * @returns {{ disk: boolean, library: boolean, canExport: boolean }}
 */
export function resolveExportDestinations(input = {}) {
  const disk = input.saveToDisk !== false
  // 宿主不支持媒体库时，无论界面状态如何都不能把 library 交给 sink。
  const library = input.supportsLibrary === true && input.saveToLibrary !== false
  return { disk, library, canExport: disk || library }
}