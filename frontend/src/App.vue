<template>
  <div class="app-shell">
    <header class="nav">
      <div class="nav-inner">
        <router-link to="/" class="brand">
          <span class="brand-icon">C</span>
          <span class="brand-name">Comfy<span class="gradient-text">Studio</span></span>
        </router-link>
        <nav id="primary-navigation" class="nav-links" :class="{ open: menuOpen }" aria-label="主导航">
          <router-link v-for="item in navItems" :key="item.to" :to="item.to" class="nav-link" active-class="active">
            <span class="nav-link-icon" aria-hidden="true">{{ item.icon }}</span>
            <span>{{ item.label }}</span>
          </router-link>
        </nav>
        <div class="nav-status">
          <button class="nav-theme-btn" @click="toggleTheme" :title="theme === 'light' ? '切换深色' : '切换浅色'">{{ theme === 'light' ? '🌙' : '☀️' }}</button>
          <span v-if="staticDemo" class="status-chip demo-chip">
            <span class="dot blue"></span>静态预览
          </span>
          <span v-else-if="store.connected" class="status-chip">
            <span class="dot green"></span>实时连接
          </span>
          <span v-else class="status-chip">
            <span class="dot gray pulse"></span>连接中
          </span>
          <span class="status-chip" v-if="runningCount > 0">
            <span class="dot blue"></span>{{ runningCount }}/{{ store.instances.length }} 实例运行
          </span>
        </div>
        <button class="nav-menu-btn" type="button" :aria-expanded="menuOpen" aria-controls="primary-navigation"
          :aria-label="menuOpen ? '关闭导航' : '打开导航'" @click="menuOpen = !menuOpen">
          <span></span><span></span><span></span>
        </button>
      </div>
    </header>
    <main class="main">
      <router-view />
    </main>
    <div class="toast-wrap" aria-live="polite">
      <transition-group name="toast">
        <div v-for="t in toast.items" :key="t.id" class="toast" :class="'toast-' + t.type" @click="toast.dismiss(t.id)">
          <span class="toast-icon">{{ toastIcon(t.type) }}</span>
          <span class="toast-msg">{{ t.message }}</span>
        </div>
      </transition-group>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAppStore } from './stores/app'
import { useToastStore } from './stores/toast'
import { wsUrl } from './api'
import { taskSuccessNotification } from './utils/taskNotifications'

const store = useAppStore()
const toast = useToastStore()
const route = useRoute()
const staticDemo = import.meta.env.VITE_STATIC_DEMO === 'true'
const menuOpen = ref(false)
const navItems = [
  { to: '/projects', label: '漫剧工作台', icon: '✦' },
  { to: '/skills', label: '漫剧 Skill', icon: '◇' },
  { to: '/materials', label: '素材库', icon: '▧' },
  { to: '/create', label: '创建任务', icon: '＋' },
  { to: '/playground', label: '生成试验场', icon: '◫' },
  { to: '/qwen-image-prompts', label: 'Qwen 提示词', icon: '◈' },
  { to: '/dashboard', label: '总览', icon: '⌁' },
  { to: '/tasks', label: '任务中心', icon: '◷' },
  { to: '/instances', label: '实例管理', icon: '◉' },
  { to: '/model-catalog', label: '模型目录', icon: '▦' },
  { to: '/settings', label: '平台设置', icon: '⚙' }
]
function toastIcon(type) {
  return { success: '✓', error: '!', info: 'i' }[type] || 'i'
}

// 主题切换（深色/浅色），持久化到 localStorage
const savedTheme = localStorage.getItem('theme')
const theme = ref(savedTheme || (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'))
document.documentElement.setAttribute('data-theme', theme.value)
function toggleTheme() {
  theme.value = theme.value === 'light' ? 'dark' : 'light'
  document.documentElement.setAttribute('data-theme', theme.value)
  localStorage.setItem('theme', theme.value)
}
let ws = null
let reconnectTimer = null
const notifiedTaskStates = new Set()

const runningCount = computed(
  () => store.instances.filter((i) => i.status === 'running').length
)

function connect() {
  if (staticDemo) return
  ws = new WebSocket(wsUrl())
  ws.onopen = () => store.setConnected(true)
  ws.onclose = () => {
    store.setConnected(false)
    reconnectTimer = setTimeout(connect, 3000)
  }
  ws.onmessage = (e) => {
    try {
      const msg = JSON.parse(e.data)
      if (msg.type === 'snapshot') store.setSnapshot(msg.data)
      if (msg.type === 'task_update') {
        store.lastEvent = msg.data
        const message = taskSuccessNotification(msg.data)
        const notificationKey = `${msg.data?.task_id}:${msg.data?.status}`
        if (message && !notifiedTaskStates.has(notificationKey)) {
          notifiedTaskStates.add(notificationKey)
          toast.success(message, 8000)
        }
      }
      if (msg.type === 'project_update') store.lastProjectUpdate = msg.data
    } catch (_) {}
  }
}

watch(() => route.fullPath, () => { menuOpen.value = false })

onMounted(connect)
onBeforeUnmount(() => {
  if (ws) ws.close()
  if (reconnectTimer) clearTimeout(reconnectTimer)
})
</script>

<style scoped>
.nav {
  position: sticky;
  top: 0;
  z-index: 100;
  background: rgba(22, 22, 23, 0.8);
  backdrop-filter: saturate(180%) blur(20px);
  -webkit-backdrop-filter: saturate(180%) blur(20px);
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  height: var(--nav-h);
}
[data-theme="light"] .nav {
  background: rgba(255, 255, 255, 0.72);
  border-bottom: 1px solid var(--border);
}
.nav-inner {
  max-width: 1280px;
  margin: 0 auto;
  height: 100%;
  padding: 0 24px;
  display: flex;
  align-items: center;
  gap: 18px;
}
.brand {
  display: flex;
  align-items: center;
  gap: 8px;
  text-decoration: none;
  color: #f5f5f7;
  font-weight: 700;
  font-size: 16px;
}
[data-theme="light"] .brand { color: var(--text); }
.brand-icon {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  background: linear-gradient(180deg, #0071e3, #0077ed);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
  font-weight: 800;
}
.nav-links {
  display: flex;
  gap: 2px;
  flex: 1;
  justify-content: center;
}
.nav-link {
  text-decoration: none;
  color: rgba(245, 245, 247, 0.8);
  font-size: 13px;
  font-weight: 400;
  padding: 6px 10px;
  border-radius: 980px;
  transition: all 0.2s;
}
.nav-link-icon { display: none; }
[data-theme="light"] .nav-link { color: var(--text-secondary); }
.nav-link:hover { color: #fff; background: rgba(255, 255, 255, 0.12); }
[data-theme="light"] .nav-link:hover { color: var(--text); background: var(--hover-bg); }
.nav-link.active { color: #fff; font-weight: 600; }
[data-theme="light"] .nav-link.active { color: var(--text); font-weight: 600; }
.nav-status { display: flex; gap: 8px; align-items: center; }
.nav-theme-btn {
  border: none;
  background: rgba(255, 255, 255, 0.12);
  font-size: 15px;
  cursor: pointer;
  padding: 4px 10px;
  border-radius: 980px;
  color: #f5f5f7;
  transition: all 0.2s;
}
[data-theme="light"] .nav-theme-btn { background: var(--chip-bg); color: var(--text-secondary); }
.nav-theme-btn:hover { background: rgba(255, 255, 255, 0.2); }
[data-theme="light"] .nav-theme-btn:hover { background: var(--hover-bg); }
.status-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  font-weight: 600;
  color: rgba(245, 245, 247, 0.85);
  background: rgba(255, 255, 255, 0.1);
  padding: 4px 12px;
  border-radius: 980px;
}
[data-theme="light"] .status-chip { color: var(--text-secondary); background: var(--chip-bg); }
.dot { width: 7px; height: 7px; border-radius: 50%; display: inline-block; }
.dot.green { background: var(--green); }
.dot.gray { background: var(--text-tertiary); }
.dot.blue { background: var(--accent); }
.demo-chip { color: #2997ff; background: rgba(10, 132, 255, .12); }
.nav-menu-btn { display: none; }
.main { min-height: calc(100vh - var(--nav-h)); }
.toast-wrap { position: fixed; top: calc(var(--nav-h) + 12px); right: 20px; z-index: 9999; display: flex; flex-direction: column; gap: 10px; pointer-events: none; }
.toast { pointer-events: auto; display: flex; align-items: center; gap: 10px; min-width: 240px; max-width: 380px; padding: 12px 16px; border-radius: 14px; background: var(--card-solid); backdrop-filter: blur(20px); -webkit-backdrop-filter: blur(20px); box-shadow: 0 12px 32px rgba(0, 0, 0, 0.16); border: 1px solid var(--border); font-size: 13.5px; color: var(--text); cursor: pointer; }
.toast-icon { width: 22px; height: 22px; border-radius: 50%; display: flex; align-items: center; justify-content: center; color: #fff; font-size: 13px; font-weight: 700; flex: 0 0 auto; }
.toast-success { border-color: rgba(52, 199, 89, 0.3); }
.toast-success .toast-icon { background: var(--green); }
.toast-error { border-color: rgba(255, 69, 58, 0.3); }
.toast-error .toast-icon { background: var(--red); }
.toast-info { border-color: rgba(10, 132, 255, 0.3); }
.toast-info .toast-icon { background: var(--accent); }
.toast-msg { line-height: 1.45; }
.toast-enter-active, .toast-leave-active { transition: all 0.28s cubic-bezier(0.2, 0.8, 0.2, 1); }
.toast-enter-from { opacity: 0; transform: translateX(24px); }
.toast-leave-to { opacity: 0; transform: translateX(24px); }
@media (max-width: 1050px) {
  .nav-inner { padding: 0 18px; }
  .nav-link { padding-inline: 7px; font-size: 12px; }
  .status-chip { display: none; }
}
@media (max-width: 820px) {
  .nav { height: var(--nav-h); }
  .nav-inner { min-height: var(--nav-h); padding: 0 16px; gap: 10px; }
  .brand { margin-right: auto; }
  .nav-status { margin-left: auto; }
  .nav-menu-btn {
    width: 36px; height: 36px; display: inline-flex; flex-direction: column; align-items: center; justify-content: center;
    gap: 4px; border: 0; border-radius: 50%; background: var(--chip-bg); cursor: pointer;
  }
  .nav-menu-btn span { width: 15px; height: 1.5px; border-radius: 1px; background: var(--text); transition: transform .2s, opacity .2s; }
  .nav-menu-btn[aria-expanded="true"] span:nth-child(1) { transform: translateY(5.5px) rotate(45deg); }
  .nav-menu-btn[aria-expanded="true"] span:nth-child(2) { opacity: 0; }
  .nav-menu-btn[aria-expanded="true"] span:nth-child(3) { transform: translateY(-5.5px) rotate(-45deg); }
  .nav-links {
    position: absolute; top: calc(var(--nav-h) + 8px); left: 16px; right: 16px; display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 6px; padding: 10px; border: 1px solid var(--border);
    border-radius: 18px; background: var(--card-solid); box-shadow: 0 20px 60px rgba(0,0,0,.18);
    opacity: 0; visibility: hidden; transform: translateY(-8px); transition: opacity .2s, transform .2s, visibility .2s;
  }
  .nav-links.open { opacity: 1; visibility: visible; transform: translateY(0); }
  .nav-link { display: flex; align-items: center; gap: 9px; padding: 11px 12px; color: var(--text-secondary); font-size: 13px; }
  .nav-link-icon { width: 20px; display: inline-block; color: var(--accent); text-align: center; }
  .nav-link:hover, .nav-link.active { color: var(--text); background: var(--accent-soft); }
}
</style>
