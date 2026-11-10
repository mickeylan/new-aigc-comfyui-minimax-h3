<template>
 <details class="packaging-editor">
  <summary>视觉包装 / 标题动画 / 风格模板</summary>
  <p>只修改当前剪辑参数，不改原视频、Scene、对白或标题原文。字幕仍使用审核字体。效果以FFmpeg完整渲染为准。</p>
  <div class="row"><label>画面风格<select :value="value.look" @change="set('look',$event.target.value)"><option v-for="[code,label] in lookOptions" :key="code" :value="code">{{label}}</option></select></label><label>标题样式<select :value="value.title_style" @change="set('title_style',$event.target.value)"><option value="caption">清晰白色字幕</option><option value="chapter">金色题名</option><option value="nameplate">银色名牌</option></select></label><label>标题动画<select :value="value.title_animation" @change="set('title_animation',$event.target.value)"><option value="none">无</option><option value="fade">淡入淡出</option><option value="slide">短距离滑入</option><option value="typewriter">逐字高亮</option></select></label></div>
  <div class="row"><span>原创参数模板</span><button v-for="preset in packagingPresets" :key="preset.code" type="button" @click="apply(preset)">{{preset.name}}</button><button type="button" @click="emit('change',null)">移除包装</button></div>
  <small>暖/冷色为本项目原创逐通道RGB LUT；柔和/黑白为程序化滤镜，不加载未知第三方.cube文件。模板仅替换风格和标题样式/动画，不改变标题文字、镜头时长、转场或轨道。</small>
  <p v-if="error" role="alert">{{error}}</p>
 </details>
</template>
<script setup>
import {computed,ref} from 'vue'
import {lookOptions,normalizePackaging,packagingPresets} from '../utils/visualPackaging.js'
const props=defineProps({modelValue:{type:Object,default:null}}),emit=defineEmits(['change'])
const error=ref(''),value=computed(()=>props.modelValue||{look:'none',title_style:'caption',title_animation:'none'})
function set(key,next){try{emit('change',normalizePackaging({...value.value,[key]:next}));error.value=''}catch(e){error.value=e.message}}
function apply(preset){if(props.modelValue&&!window.confirm('替换当前片段的视觉包装设置？不会改写标题文字或原素材。'))return;emit('change',{...preset.packaging})}
</script>
<style scoped>
.packaging-editor{border:1px solid #677792;border-radius:6px;margin:12px 0;padding:8px}.packaging-editor summary{cursor:pointer}.row{display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin:8px 0}.row label{display:flex;gap:6px;font-size:12px}.packaging-editor p,.packaging-editor small{font-size:12px}
</style>
