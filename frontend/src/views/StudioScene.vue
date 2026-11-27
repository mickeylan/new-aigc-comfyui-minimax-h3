<template>
  <div class="studio-scene3d-host">
    <p v-if="error" role="alert">{{ error }} <router-link to="/projects">返回项目</router-link></p>
    <div ref="host" class="h-full w-full" />
  </div>
</template>

<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { sceneStudioContext } from '../studio/scene3d/scene-archive'

const route = useRoute()
const error = ref('')

const host = ref(null)
let handle = null
let unmounted = false
let request = 0
let stopWatch = null

onMounted(() => {
  stopWatch = watch(() => [route.query.project_id, route.query.scene_id], async ([projectId, sceneId]) => {
    const ticket = ++request
    handle?.unmount()
    handle = null
    error.value = ''
    try {
      const context = sceneStudioContext(projectId, sceneId)
      const { mountScene3D } = await import('../studio/host/mount-scene3d')
      // 懒加载期间切走或来源改变，不挂到旧场次/游离节点。
      if (unmounted || !host.value) return
      if (ticket !== request) return
      handle = mountScene3D(host.value, { context, exitTo: context ? `/projects/${context.projectId}` : '/materials' })
    } catch (e) {
      if (!unmounted && ticket === request) error.value = e instanceof Error ? e.message : String(e)
    }
  }, { immediate: true })
})

onBeforeUnmount(() => {
  unmounted = true
  ++request
  stopWatch?.()
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