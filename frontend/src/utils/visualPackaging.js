export const transitionOptions=[['cut','硬切'],['fade','交叉溶解'],['dissolve','颗粒溶解'],['fadeblack','经黑淡变'],['fadewhite','经白淡变'],['wipeleft','向左擦除'],['wiperight','向右擦除'],['wipeup','向上擦除'],['wipedown','向下擦除'],['slideleft','向左滑动'],['slideright','向右滑动'],['slideup','向上滑动'],['slidedown','向下滑动'],['circleopen','圆形展开'],['circleclose','圆形收拢']]
export const lookOptions=[['none','原始'],['warm','暖色（原创RGB LUT）'],['cool','冷色（原创RGB LUT）'],['muted','低饱和柔和'],['monochrome','黑白']]
export const maskOptions=[['none','矩形 / 保持原透明度'],['ellipse','椭圆'],['rounded','圆角矩形']]
export function normalizePackaging(value){
 if(value==null)return null
 const p={look:String(value.look||'none'),title_animation:String(value.title_animation||'none'),title_style:String(value.title_style||'caption')}
 if(!lookOptions.some(([key])=>key===p.look)||!['none','fade','slide','typewriter'].includes(p.title_animation)||!['caption','chapter','nameplate'].includes(p.title_style))throw new RangeError('未知视觉包装参数')
 return p
}
export function normalizeMask(value){const mask=String(value||'none');if(!maskOptions.some(([key])=>key===mask))throw new RangeError('未知叠加蒙版');return mask==='none'?'':mask}
// Original reusable recipes. No title text, source identity, timing, dialogue or
// reference selection is changed when applying a recipe.
export const packagingPresets=[
 {code:'clear-caption',name:'清晰字幕',packaging:{look:'none',title_style:'caption',title_animation:'fade'}},
 {code:'warm-chapter',name:'暖色金色题名',packaging:{look:'warm',title_style:'chapter',title_animation:'fade'}},
 {code:'cool-nameplate',name:'冷色银色名牌',packaging:{look:'cool',title_style:'nameplate',title_animation:'slide'}},
 {code:'mono-type',name:'黑白逐字标题',packaging:{look:'monochrome',title_style:'caption',title_animation:'typewriter'}},
]
