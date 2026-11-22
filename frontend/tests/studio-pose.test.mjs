import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const read = (p) => readFileSync(new URL(p, import.meta.url), 'utf8')
const stage3d = read('../src/studio/scene3d/Scene3DStage.tsx')
const fkHelper = read('../src/studio/scene3d/posing/FKHelper.ts')
const bonePose = read('../src/studio/scene3d/posing/bonePose.ts')

// 角色摆姿是骨级 FK：点红球选中骨，用局部旋转 gizmo 单骨摆姿，
// 与之前的「整体姿势混合（A/B 两态插值）」是两件事，不能混称。
test('3D 编辑器接入骨级 FK 摆姿而不是整体变换混合', () => {
  assert.match(stage3d, /from "\.\/posing\/FKHelper\.js"/, '应引入 FKHelper')
  assert.match(stage3d, /serializeBonePose/, '应能序列化骨骼姿势')
  assert.match(stage3d, /applyBonePose/, '应能还原骨骼姿势')
  assert.match(stage3d, /findSkeletonRoot/, '应能定位骨树根')
  assert.match(stage3d, /togglePosing/, '缺少摆姿开关')
  assert.match(stage3d, /开启摆姿/, '缺少开启入口文案')
  assert.match(stage3d, /退出摆姿/, '缺少退出入口文案')
  assert.match(stage3d, /整体姿势混合/, '原来的整体姿势混合应改名区分')
})

test('只有带骨骼的模型才显示摆姿入口，其他给出可读提示', () => {
  assert.match(stage3d, /hasSkeleton/, '应判断模型有无骨骼')
  assert.match(stage3d, /selectedHasSkeleton/, '面板应按选中对象是否带骨骼分支')
  assert.match(stage3d, /这个模型没有骨骼/, '没有骨骼时要说明原因')
})

test('FK 交互：红球选骨、局部旋转、拖动时暂停相机', () => {
  assert.match(fkHelper, /SphereGeometry/, '每根骨应挂红球')
  assert.match(fkHelper, /depthTest:\s*false/, '红球要画在模型前面才点得到')
  assert.match(fkHelper, /setSpace\("local"\)/, '骨骼旋转必须用局部空间')
  assert.match(fkHelper, /setMode\("rotate"\)/, '骨骼 gizmo 只做旋转')
  assert.match(fkHelper, /SkeletonHelper/, '应显示整体骨架参考线')
  assert.match(fkHelper, /dragging-changed/, '拖动骨骼时应通知外部')
  assert.match(fkHelper, /Raycaster/, '点选骨需要射线检测')
  assert.match(fkHelper, /getHelper\(\)/, 'three 0.180 的 TransformControls 需挂 helper 节点')
})

test('画布点选骨不会把转相机误判成选骨', () => {
  assert.match(stage3d, /pointerdown/, '需要记录按下位置')
  assert.match(stage3d, /pointerup/, '释手时判定是否点选')
  assert.match(stage3d, /moved > 4/, '位移超过阈值算转相机')
  assert.match(stage3d, /posingRef\.current/, '事件处理要读最新开关状态')
})

test('骨骼姿势随场景存档往返，且能恢复初始姿势', () => {
  assert.match(stage3d, /bonePose: entry\.bonePose \?\? null/, '场景存档要写入骨骼姿势')
  assert.match(stage3d, /initialBonePose/, '要记一份绑定姿势')
  assert.match(stage3d, /saved\?\.bonePose/, '载入场景时读回骨骼姿势')
  assert.match(stage3d, /applyBonePose\(child, saved\.bonePose\)/, '载入后应套回骨骼姿势')
  assert.match(stage3d, /保存姿势/, '缺少保存姿势入口')
  assert.match(stage3d, /已恢复初始姿势/, '缺少恢复初始姿势的反馈')
})

test('摆姿逻辑层：黑名单过滤手指面部骨，无名骨不参与', () => {
  assert.match(bonePose, /export const FK_BONE_BLACKLIST/, '缺少黑名单')
  assert.match(bonePose, /isPoseableBone/, '缺少可摆骨判定')
  assert.match(bonePose, /mixamorighips/i, '应优先认 Mixamo 的 hips')
  assert.match(bonePose, /updateMatrixWorld\(true\)/, '还原后要刷新世界矩阵')
})