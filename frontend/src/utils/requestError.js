// 把请求异常翻译成带原因的提示语。
//
// 过去上传失败只取 e.response?.data?.error，任何不符合这个形状的失败都退化成
// 一句没有原因的「上传失败」：
//   - 反向代理按请求体大小拦截（nginx client_max_body_size 默认 1MB）返回的是
//     HTML 错误页，data.error 不存在，而状态码 413 才是唯一线索；
//   - 请求超时或连不上后端时根本没有 response，e.message 是唯一线索；
//   - 网关返回的 502/504 同样没有 JSON 正文。
// 因此状态码始终保留：它经常比正文更能定位问题。
export function requestErrorMessage(error, fallback = '请求失败') {
  if (!error) return fallback
  const response = error.response
  if (response) {
    const served = serverMessage(response.data)
    const status = `HTTP ${response.status}${response.statusText ? ` ${response.statusText}` : ''}`
    if (served) return `${served}（${status}）`
    if (response.status === 413) return `${fallback}：文件超过服务器允许的请求体大小（${status}）`
    return `${fallback}：${status}`
  }
  if (error.code === 'ECONNABORTED' || /timeout/i.test(error.message || '')) {
    return `${fallback}：请求超时，可能文件过大或网络过慢`
  }
  return `${fallback}：${error.message || '无法连接后端服务'}`
}

// serverMessage 只提取可读的服务端说明；HTML 错误页会淹没真正的线索，丢弃之后
// 由状态码说明问题。
function serverMessage(data) {
  if (!data) return ''
  if (typeof data === 'string') {
    const text = data.trim()
    if (!text || text.startsWith('<')) return ''
    return text.slice(0, 300)
  }
  if (typeof data !== 'object') return ''
  return String(data.error || data.message || '').trim()
}
