import { defineStore } from 'pinia'

// 全局通知：普通消息自动消失；错误默认保留，便于阅读和复制。
export const useToastStore = defineStore('toast', {
  state: () => ({
    items: [],
    seq: 0
  }),
  actions: {
    show(message, type = 'info', timeout = 3400) {
      if (!message) return
      const id = ++this.seq
      this.items.push({ id, message, type })
      if (timeout > 0) {
        const seed = id
        setTimeout(() => this.dismiss(seed), timeout)
      }
      return id
    },
    success(msg, timeout) { return this.show(msg, 'success', timeout) },
    error(msg, timeout) { return this.show(msg, 'error', timeout ?? 0) },
    info(msg, timeout) { return this.show(msg, 'info', timeout) },
    dismiss(id) {
      const i = this.items.findIndex((x) => x.id === id)
      if (i >= 0) this.items.splice(i, 1)
    }
  }
})
