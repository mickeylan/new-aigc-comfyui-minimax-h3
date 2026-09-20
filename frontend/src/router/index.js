import { createRouter, createWebHashHistory, createWebHistory } from 'vue-router'

const routes = [
  { path: '/', redirect: '/projects' },
  { path: '/dashboard', name: 'dashboard', component: () => import('../views/Dashboard.vue') },
  { path: '/create', name: 'create', component: () => import('../views/CreateTask.vue') },
  { path: '/playground', name: 'playground', component: () => import('../views/Playground.vue') },
  { path: '/tasks', name: 'tasks', component: () => import('../views/Tasks.vue') },
  { path: '/tasks/:id', name: 'task-detail', component: () => import('../views/TaskDetail.vue') },
  { path: '/instances', name: 'instances', component: () => import('../views/Instances.vue') },
  { path: '/projects', name: 'projects', component: () => import('../views/Projects.vue') },
  { path: '/projects/new', name: 'project-new', component: () => import('../views/ProjectNew.vue') },
  { path: '/projects/:id', name: 'project-detail', component: () => import('../views/ProjectDetail.vue') },
  { path: '/projects/:id/editor', name: 'project-editor', component: () => import('../views/ProjectEditor.vue') },
  { path: '/projects/:id/episodes/:episode/screenplay', name: 'episode-screenplay', component: () => import('../views/EpisodeScreenplay.vue') },
  { path: '/projects/:id/characters/:cid/looks', name: 'character-looks', component: () => import('../views/CharacterLooks.vue') },
  { path: '/projects/:id/novel', name: 'project-novel', component: () => import('../views/NovelImport.vue') },
  { path: '/projects/:id/story-bible', name: 'story-bible', component: () => import('../views/StoryBible.vue') },
  { path: '/projects/:id/adaptation', name: 'adaptation', component: () => import('../views/AdaptationPlan.vue') },
  { path: '/skills', name: 'skills', component: () => import('../views/Skills.vue') },
  { path: '/materials', name: 'materials', component: () => import('../views/Materials.vue') },
  { path: '/model-catalog', name: 'model-catalog', component: () => import('../views/ModelCatalog.vue') },
  { path: '/settings', name: 'settings', component: () => import('../views/Settings.vue') }
]

export default createRouter({
  // GitHub Pages 没有 SPA 回退能力，静态演示使用 hash；完整部署保留干净的 history URL。
  history: import.meta.env.VITE_STATIC_DEMO === 'true'
    ? createWebHashHistory(import.meta.env.BASE_URL)
    : createWebHistory(import.meta.env.BASE_URL),
  routes
})
