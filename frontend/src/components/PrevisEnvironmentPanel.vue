<template>
 <section class="environment"><h4>天气 / GLB 骨骼动画 / 白模导出</h4>
 <div class="row"><label>天气<select :value="document.weather?.preset||'clear'" @change="weather('preset',$event.target.value)"><option v-for="(name,key) in presets" :key="key" :value="key">{{name}}</option></select></label><label>强度<input type="number" min="0" max="1" step="0.1" :value="document.weather?.intensity??0.5" @change="weather('intensity',Number($event.target.value))"></label><label>随机种子<input type="number" min="0" max="2147483647" :value="document.weather?.seed??1" @change="weather('seed',Number($event.target.value))"></label><label>太阳高度°<input type="number" min="0" max="90" step="1" :value="document.weather?.sun_elevation??45" @change="weather('sun_elevation',Number($event.target.value))"></label><label>雾密度<input type="number" min="0" max="0.1" step="0.005" :value="document.weather?.fog_density??0.025" @change="weather('fog_density',Number($event.target.value))"></label></div>
 <p>GLB仅存当前浏览器；换设备或清理浏览器后须重新导入相同文件。请选择有权使用的自包含、未压缩GLB（≤32MiB），不加载外部资源。模型自动按对象高度缩放、脚底对齐；尺寸宽深仅为原占位参数。优先使用原地动作片段，带根位移的片段与路线叠加可能滑步。</p>
 <div v-if="selected"><label>给 {{selected.name}} 导入 GLB<input type="file" accept=".glb" :disabled="busy" @change="importFile"></label><label v-if="models3d.length">从素材库选择<select v-model.number="libraryId" :disabled="busy"><option :value="0">选择 3D 素材</option><option v-for="m in models3d" :key="m.id" :value="m.id">{{m.name}}</option></select></label><button v-if="models3d.length" type="button" :disabled="busy||!libraryId" @click="importFromLibrary">绑定素材库模型</button><template v-if="binding"><p>{{binding.file_name}} · {{information?.message||'等待恢复本地模型'}} · SHA256 {{binding.asset_hash.slice(0,12)}}</p><label>动画片段<select :value="binding.clip_index" @change="changeBinding('clip_index',Number($event.target.value))"><option :value="-1">静态绑定姿态</option><option v-for="(clip,i) in information?.clips||[]" :key="i" :value="i">{{clip.name||'未命名片段'}}（{{clip.duration.toFixed(2)}}秒）</option></select></label><label>起秒<input type="number" min="0" step="0.1" :value="binding.start_ms/1000" @change="changeBinding('start_ms',Math.round(Number($event.target.value)*1000))"></label><label>止秒<input type="number" min="0" step="0.1" :value="binding.end_ms/1000" @change="changeBinding('end_ms',Math.round(Number($event.target.value)*1000))"></label><label><input type="checkbox" :checked="binding.loop" @change="changeBinding('loop',$event.target.checked)">循环</label><button type="button" @click="unbind">解除模型绑定（保留本地缓存）</button></template></div><p v-else>先选择对象再导入模型。</p>
 <p>导出排除网格、箭头、A/B标记、天气粒子和雾；白模保留体积光照，深度为近白远黑的8位线性灰度，仅下载，不自动接入生成。导出前请检查所有模型已恢复。</p>
 <div class="row"><label>深度近界（米）<input type="number" min="0" max="499.9" step="0.1" v-model.number="near"></label><label>深度远界（米）<input type="number" min="0.1" max="500" step="0.1" v-model.number="far"></label></div>
 <div class="row"><button type="button" :disabled="busy||!ready" @click="$emit('export',{mode:'white',depth_range:{near,far}})">导出当前帧白模 PNG</button><button type="button" :disabled="busy||!ready" @click="$emit('export',{mode:'depth',depth_range:{near,far}})">导出当前帧深度 PNG</button></div>
 <div class="row"><label>序列起秒<input type="number" min="0" step="0.1" v-model.number="from"></label><label>序列止秒<input type="number" min="0" step="0.1" v-model.number="to"></label><label>FPS<select v-model.number="fps"><option v-for="n in [1,6,12,24]" :key="n" :value="n">{{n}}</option></select></label><label>画面<select v-model="pass"><option value="white">白模</option><option value="depth">深度</option></select></label><button type="button" :disabled="busy||!ready" @click="$emit('export',{mode:pass,from_ms:Math.round(from*1000),to_ms:Math.round(to*1000),fps,depth_range:{near,far}})">导出序列ZIP（最多240帧）</button><button type="button" :disabled="busy||!ready" @click="$emit('export-video',{mode:pass,from_ms:Math.round(from*1000),to_ms:Math.round(to*1000),fps,depth_range:{near,far}})">导出 WebM 视频（最多2880帧）</button></div>
 <p>视频按帧序号逐帧编码（优先VP8，不支持时回退VP9，1280×720，无音轨；旧VP9文件需重新导出），渲染慢不会丢帧或变速；需浏览器支持 WebCodecs。深度视频为有损编码，仅供审阅运动与机位，精确深度请用PNG序列。</p>
 </section>
</template>
<script setup>
import{computed,ref}from'vue'
import{defaultWeather}from'../utils/previsEnvironment.js'
const props=defineProps({document:Object,selected:Object,information:Object,busy:Boolean,ready:Boolean,models3d:{type:Array,default:()=>[]}})
const emit=defineEmits(['update-document','import-model','export','export-video'])
const presets={clear:'晴',overcast:'阴',rain:'雨',snow:'雪',fog:'雾'},from=ref(0),to=ref(3),fps=ref(12),pass=ref('white'),near=ref(0.1),far=ref(50)
const libraryId=ref(0)
const binding=computed(()=>props.document.models?.find(b=>b.object_id===props.selected?.id))
function weather(key,value){emit('update-document',{...props.document,schema_version:3,weather:{...defaultWeather(),...props.document.weather,[key]:value}})}
function changeBinding(key,value){emit('update-document',{...props.document,models:props.document.models.map(b=>b===binding.value?{...b,[key]:value}:b)})}
function unbind(){emit('update-document',{...props.document,models:props.document.models.filter(b=>b!==binding.value)})}
function importFile(e){const file=e.target.files?.[0];e.target.value='';if(file)emit('import-model',{file,object_id:props.selected.id})}
// 素材库 3D 模型：取回字节后当作本地文件走同一条导入流程（本地缓存 + 校验 + 绑定）。
async function importFromLibrary(){
  const item=(props.models3d||[]).find(m=>m.id===libraryId.value)
  if(!item||!props.selected)return
  try{
    const i=item.path.indexOf('/')
    const url=i>0?`/api/input/${item.path.slice(0,i)}/${item.path.slice(i+1)}`:`/api/input/${item.path}`
    const res=await fetch(url)
    if(!res.ok)throw new Error(`读取素材失败（HTTP ${res.status}）`)
    const blob=await res.blob()
    emit('import-model',{file:new File([blob],item.name,{type:'model/gltf-binary'}),object_id:props.selected.id})
  }catch(e){emit('library-error',e.message)}
}
</script>
<style scoped>.environment{padding:.8rem;margin:.8rem 0;border:1px solid var(--border)}.row{display:flex;flex-wrap:wrap;gap:.7rem;align-items:center}input[type=number]{width:5rem}p{font-size:.85rem}label{display:inline-block;margin:.3rem}button{margin:.3rem}</style>
