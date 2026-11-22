// 素材库列表接口 GET /api/materials 目前直接返回裸数组；
// 历史上也出现过 {items: []} 的分页包装（前端多处代码两种都认）。
// 只认其中一种会让下拉框静默变空，所以统一走这里。
export function materialList(data) {
  if (Array.isArray(data)) return data
  return Array.isArray(data?.items) ? data.items : []
}