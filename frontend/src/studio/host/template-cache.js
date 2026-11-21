// 模板清单查询：三个 studio 编辑器都要按 code 找模板 id，而 /api/templates
// 会把所有模板（含完整 workflow_json，约 150–190 KB）一次性返回。每次生成都拉
// 一遍既慢又浪费，因此这里把清单缓存到模块级：同一页面生命周期内只请求一次，
// 并发查询共用同一次请求；请求失败不缓存，下一次可以重试。

/** @type {Promise<Array<{ id: number, code?: string }>> | null} */
let pending = null

async function loadTemplates(fetchImpl) {
  const response = await fetchImpl('/api/templates')
  if (!response.ok) throw new Error(`读取模板失败：${response.status}`)
  const payload = await response.json()
  const templates = Array.isArray(payload) ? payload : payload?.items ?? []
  return templates
}

/** 清空缓存。后端新装模板后需要重新读取时调用。 */
export function resetTemplateCache() {
  pending = null
}

/**
 * 按模板 code 取 id。清单只请求一次，多个 code 共用同一份结果。
 * @param {string} code
 * @param {{ fetchImpl?: typeof fetch }} [options]
 */
export async function findTemplateId(code, options = {}) {
  const fetchImpl = options.fetchImpl ?? globalThis.fetch
  if (!pending) {
    const request = loadTemplates(fetchImpl)
    pending = request
    // 失败时不能把 rejected promise 留在缓存里，否则后续重试会永远拿到同一个错误。
    request.catch(() => {
      if (pending === request) pending = null
    })
  }
  const templates = await pending
  const template = templates.find((item) => item?.code === code)
  if (!template) throw new Error(`缺少工作流模板：${code}`)
  return template.id
}