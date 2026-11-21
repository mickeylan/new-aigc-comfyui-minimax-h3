// 导出目的地的可用性判定。单独放成纯函数，是因为宿主没有媒体库时
// 仍然默认勾选「Save to my media library」，导出结束必然报
// "Library upload failed"，用户会以为导出失败（其实文件已存盘）。

/**
 * @param {{
 *   supportsLibrary?: boolean,
 *   supportsScene?: boolean,
 *   saveToDisk?: boolean,
 *   saveToLibrary?: boolean,
 *   saveToScene?: boolean,
 * }} input
 *   supportsLibrary / supportsScene 来自宿主提供的 export sink；未声明视为不支持。
 *   supportsScene 表示编辑器是从某个分镜打开的，可以把成片写回该 Scene 的候选。
 * @returns {{ disk: boolean, library: boolean, scene: boolean, canExport: boolean }}
 */
export function resolveExportDestinations(input = {}) {
  const disk = input.saveToDisk !== false
  // 宿主不支持媒体库时，无论界面状态如何都不能把 library 交给 sink。
  const library = input.supportsLibrary === true && input.saveToLibrary !== false
  // 写回分镜只对带 Scene 上下文的宿主要意义，别的入口不显示也不提交。
  const scene = input.supportsScene === true && input.saveToScene !== false
  return { disk, library, scene, canExport: disk || library || scene }
}