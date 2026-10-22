<template>
  <div class="page">
    <header class="head"><div><span class="eyebrow">EXTENSIBILITY</span><h1>扩展能力状态</h1><p>所有扩展默认关闭；此页面只显示诊断，不改变现有生产路径。</p></div><button class="btn" @click="load">刷新</button></header>
    <div v-if="loading" class="card">正在读取扩展状态…</div>
    <div v-else-if="error" class="card error">{{ error }}</div>
    <section v-else class="grid">
      <article class="card"><h2>文件化Skill包</h2><statusLine :enabled="status.skill_packages?.enabled"/><p>已安装版本：{{status.skill_packages?.installed_count||0}}</p><p>目录已配置：{{yesNo(status.skill_packages?.configured)}}</p></article>
      <article class="card"><h2>能力声明附加层</h2><statusLine :enabled="status.capability_manifests?.enabled"/><p>有效声明：{{status.capability_manifests?.loaded_count||0}}</p><p v-if="status.capability_manifests?.error" class="error">{{status.capability_manifests.error}}</p></article>
      <article class="card"><h2>受控Agent工具</h2><statusLine :enabled="status.agent_tools?.enabled"/><p>已注册：{{status.agent_tools?.registered_count||0}} · 审计：{{status.agent_tools?.audit_count||0}}</p><ul><li v-for="tool in status.agent_tools?.items||[]" :key="tool.code">{{tool.name}} · {{tool.permission}}</li></ul></article>
      <article class="card"><h2>生成适配器</h2><statusLine :enabled="status.generation_adapters?.enabled"/><p>已就绪：{{status.generation_adapters?.registered_count||0}}</p><p>没有真实模板和校验通过的适配器时不会开放提交。</p></article>
      <article class="card"><h2>精确依赖图</h2><statusLine :enabled="status.dependency_graph?.enabled"/><p>依赖边：{{status.dependency_graph?.edge_count||0}} · 失效事件：{{status.dependency_graph?.invalidation_event_count||0}}</p><p>领域表仍是权威，依赖图仅用于影响分析和审计。</p></article>
    </section>
  </div>
</template>
<script setup>
import { h, onMounted, ref } from 'vue'
import { api } from '../api'
const status=ref({}),loading=ref(true),error=ref('')
const statusLine=(props)=>h('span',{class:['status',props.enabled?'on':'off']},props.enabled?'已启用':'默认关闭')
function yesNo(v){return v?'是':'否'}
async function load(){loading.value=true;error.value='';try{const {data}=await api.extensibilityStatus();status.value=data||{}}catch(e){error.value=e.response?.data?.error||'扩展状态加载失败'}finally{loading.value=false}}
onMounted(load)
</script>
<style scoped>
.page{max-width:1100px;margin:auto;padding:36px 24px}.head{display:flex;justify-content:space-between;gap:16px;align-items:end;margin-bottom:20px}.eyebrow{color:var(--accent);font-size:11px;letter-spacing:.15em}h1{margin:6px 0;color:var(--text)}p,li{color:var(--text-secondary);font-size:13px}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(290px,1fr));gap:14px}.card{padding:18px;border:1px solid var(--border);border-radius:16px;background:var(--card-solid)}h2{margin:0 0 12px;color:var(--text);font-size:17px}.status{display:inline-block;padding:4px 9px;border-radius:999px;font-size:12px}.status.on{color:var(--green);background:rgba(52,199,89,.1)}.status.off{color:var(--text-tertiary);background:var(--chip-bg)}.error{color:var(--red)}.btn{padding:8px 14px;border:1px solid var(--border);border-radius:9px;background:var(--card-solid);color:var(--text);cursor:pointer}
</style>
