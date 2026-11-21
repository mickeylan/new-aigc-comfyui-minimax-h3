<script setup>
import { onBeforeUnmount, onMounted, ref } from "vue";
import { useRoute } from "vue-router";
import { takeStudioMedia } from "../studio/initial-media.js";

const el = ref(null)
const route = useRoute();
const loading = ref(true);
let handle = null;
let unmounted = false;

onMounted(async () => {
  const initialMedia = takeStudioMedia();
  // Lazy-load the React island so Vue-only routes never download React or
  // the editor's wasm/renderer chunks.
  const { mountVideoEditor } = await import("../studio/host/mount");
  // The import may outlive the route: mounting then would attach the editor
  // to a detached node and leave the body scroll lock behind.
  if (unmounted || !el.value) return;
  handle = mountVideoEditor(el.value, {
    projectId: route.params.projectId,
    exitTo: "/projects",
    initialMedia,
  });
  loading.value = false;
});

onBeforeUnmount(() => {
  unmounted = true;
  handle?.unmount();
  handle = null;
});
</script>

<template>
  <div class="studio-page">
    <div ref="el" class="studio-root" />
    <div v-if="loading" class="studio-loading">加载剪辑器…</div>
  </div>
</template>

<style scoped>
.studio-page {
  position: fixed;
  inset: 0;
  z-index: 1000;
  background: #121316;
}
.studio-root {
  width: 100%;
  height: 100%;
}
.studio-loading {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: rgba(245, 245, 247, 0.7);
  font: 13px/1.5 system-ui, sans-serif;
  pointer-events: none;
}
</style>