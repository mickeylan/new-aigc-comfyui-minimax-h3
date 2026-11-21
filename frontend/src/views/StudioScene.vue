<template>
  <div ref="host" class="studio-scene3d-host" />
</template>

<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'

const host = ref(null)
let handle = null
let unmounted = false

onMounted(async () => {
  const { mountScene3D } = await import('../studio/host/mount-scene3d')
  // 同 StudioDraw：import 期间已切走时不要再挂载。
  if (unmounted || !host.value) return
  handle = mountScene3D(host.value, { exitTo: '/materials' })
})

onBeforeUnmount(() => {
  unmounted = true
  handle?.unmount()
  handle = null
})
</script>

<style scoped>
.studio-scene3d-host {
  position: fixed;
  inset: 0;
  /* 与画布、视频剪辑一致：3D 自己的 h-14 顶栏不能被站点导航盖住。 */
  z-index: 1000;
}
</style>