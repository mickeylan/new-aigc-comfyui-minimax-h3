<template>
  <div ref="host" class="studio-draw-host" />
</template>

<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { takeStudioScene } from '../studio/initial-media.js'
import { buildSceneWriteback } from '../studio/host/scene-writeback.js'

const host = ref(null)
let handle = null
let unmounted = false

// 从项目编辑器进来时带分镜上下文：载入原图并允许写回；直接打开 /studio/draw 则只是画布。
function drawOptions(scene) {
  const writeBack = buildSceneWriteback(scene)
  return {
    exitTo: scene?.returnTo || '/materials',
    initialImageUrl: scene?.imageUrl || undefined,
    writeBack: writeBack || undefined,
    writeBackLabel: scene?.mode === 'frame' ? '写回衔接帧（保留原图）' : '写回分镜（另存候选）',
  }
}

onMounted(async () => {
  const { mountPageDraw } = await import('../studio/host/mount-pagedraw')
  // 动态 import 期间用户可能已经切走；再挂载会写进已卸载的节点，并把
  // studio-theme 留在 body 上没人清理。
  if (unmounted || !host.value) return
  handle = mountPageDraw(host.value, drawOptions(takeStudioScene()))
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