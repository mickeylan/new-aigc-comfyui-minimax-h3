<template>
  <div ref="host" class="studio-draw-host" />
</template>

<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'

const host = ref(null)
let handle = null
let unmounted = false

onMounted(async () => {
  const { mountPageDraw } = await import('../studio/host/mount-pagedraw')
  // 动态 import 期间用户可能已经切走；再挂载会写进已卸载的节点，并把
  // studio-theme 留在 body 上没人清理。
  if (unmounted || !host.value) return
  handle = mountPageDraw(host.value, { exitTo: '/materials' })
})

onBeforeUnmount(() => {
  unmounted = true
  handle?.unmount()
  handle = null
})
</script>

<style scoped>
.studio-draw-host {
  position: fixed;
  inset: 0;
  /* 画布自己的顶栏是 fixed top-0 h-14；站点导航 z-index:100 会盖住它，必须全屏接管。 */
  z-index: 1000;
}
</style>