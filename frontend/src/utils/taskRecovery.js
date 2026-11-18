export function taskRecoveryText(task) {
  if (['success', 'failed', 'cancelled'].includes(task?.status)) return ''
  return { submitting: '提交中（请勿重复提交）', unknown: '结果未知 · 待核查', awaiting_result: '已完成 · 待取回', cancel_requested: '停止请求待确认' }[task?.provider_outcome] || ''
}

// Reconnect only restores reads. No submission/cancel/retry callback exists here.
export function connectTaskUpdates({ url, onMessage, reload, onConnection, WebSocketImpl = globalThis.WebSocket, schedule = setTimeout, unschedule = clearTimeout }) {
  let socket, timer, closed = false
  function connect() {
    if (closed) return
    socket = new WebSocketImpl(url())
    const current = socket
    socket.onopen = () => {
      if (closed || current !== socket) return
      onConnection(true)
      Promise.resolve().then(reload).catch(() => onConnection(false))
    }
    socket.onmessage = event => { if (!closed && current === socket) onMessage(event) }
    socket.onerror = () => { if (!closed && current === socket) onConnection(false) }
    socket.onclose = () => {
      if (closed || current !== socket) return
      onConnection(false)
      timer = schedule(connect, 2000)
    }
  }
  connect()
  return () => {
    closed = true
    if (timer) unschedule(timer)
    if (socket) { socket.onclose = null; socket.close() }
  }
}
