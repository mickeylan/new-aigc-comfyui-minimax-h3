// 生成试验场的结果文件来自任务快照 result_files：[{type,filename,subfolder}]。
// 图片/视频/音频必须拼成可访问地址才能在界面上看到，光把 JSON 打印出来是看不见图的。
const IMAGE_RE = /\.(png|jpe?g|webp|gif|bmp|avif)$/i
const VIDEO_RE = /\.(mp4|webm|mov|mkv)$/i
const AUDIO_RE = /\.(wav|mp3|flac|ogg|m4a|aac)$/i

// parseRunResults 同时接受后端返回的数组、字符串化 JSON 与空值。
export function parseRunResults(result) {
  if (Array.isArray(result)) return result.filter(Boolean)
  if (typeof result === 'string' && result.trim()) {
    try {
      const parsed = JSON.parse(result)
      return Array.isArray(parsed) ? parsed.filter(Boolean) : []
    } catch {
      return []
    }
  }
  return []
}

export function runResultKind(file) {
  const name = String(file?.filename || '')
  if (IMAGE_RE.test(name)) return 'image'
  if (VIDEO_RE.test(name)) return 'video'
  if (AUDIO_RE.test(name)) return 'audio'
  return 'file'
}

export function runResultPath(file) {
  const name = String(file?.filename || '').replace(/^\/+/, '')
  const sub = String(file?.subfolder || '').replace(/^\/+|\/+$/g, '')
  return sub ? `${sub}/${name}` : name
}

// runResultUrl 优先走实例输出（/api/instances/:id/output/*path 会转发到 ComfyUI /view）；
// 没有实例时退回 /api/output/<gpu>/*path，gpu 缺失按 0 处理。
export function runResultUrl(run, file) {
  const path = runResultPath(file)
  if (!path) return ''
  const instanceId = run?.instance_id
  if (instanceId) return `/api/instances/${instanceId}/output/${path}`
  const gpu = Number.isInteger(run?.gpu_index) && run.gpu_index >= 0 ? run.gpu_index : 0
  return `/api/output/${gpu}/${path}`
}