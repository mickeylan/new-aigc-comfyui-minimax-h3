import * as THREE from 'three';
import { createRiggedPerson } from './toonflow/riggedPerson';
import { solveWristContact, type WristContactBinding } from './characterMotion';
import { validateScriptShotPlan } from './scriptShotPlan';
import { defaultSceneEnvironment, validateSceneEnvironment } from './ground-path';

/** Authored six-second action study on OUR calibrated procedural rig only.
 * Not a general retargeter, physics balance solver, closed finger grip or finished
 * character asset. Ordinary bone clips + explicit wrist contact survive archives.
 * The shared sampler remains responsible for all posed/environment safety proofs.
 */
export function createWristRescuePreview() {
  const man = createRiggedPerson('#5583a2'), woman = createRiggedPerson('#d7a08e');
  // +Z is the rig's face. Pond is behind the woman (-Z); the man is inland.
  man.position.z = .78; man.rotation.y = Math.PI;
  const entries = [
    { id: 'rescue-man', name: '男子（蓝色骨骼代理）', object: man, locked: false },
    { id: 'rescue-woman', name: '女子（粉色骨骼代理）', object: woman, locked: false },
  ];
  const duration = 6;
  const contact: WristContactBinding = { version: 1, arm: { shoulder: 'leftUpperArm', elbow: 'leftForeArm', wrist: 'leftHand' }, targetBone: 'rightHand',
    // Hand-side patch beyond the deforming wrist blend seam, not an air anchor.
    // The source elbow bends AWAY from its partner, rather than bending through
    // the wrist plane during retraction. No part is removed from the skin proof.
    sourceAnchor: [.066, -.04, .0047], targetAnchor: [-.066, -.04, .0047], rotation: [0, 0, 0, 1], pole: [0, 0, 1] };
  const marker = new THREE.Group(), anchor = new THREE.Bone(); anchor.name = 'authored-anchor'; marker.add(anchor);
  const jointBinding = (shoulder: string, elbow: string, wrist: string): WristContactBinding => ({ ...contact,
    arm: { shoulder, elbow, wrist }, targetBone: anchor.name, sourceAnchor: [0, 0, 0], targetAnchor: [0, 0, 0] });
  const aimJoint = (root: THREE.Object3D, chain: [string, string, string], point: [number, number, number], pole: [number, number, number] = [0, 0, 1]) => {
    anchor.position.fromArray(point); anchor.quaternion.identity();
    if (chain[2] === 'rightHand') anchor.rotation.y = Math.PI / 2;
    try { solveWristContact(root, marker, { ...jointBinding(...chain), pole }); }
    catch (error) {
      const shoulder = root.getObjectByName(chain[0])!.getWorldPosition(new THREE.Vector3());
      throw Error(`${error instanceof Error ? error.message : error} (${root === man ? '男' : '女'} ${chain[0]}, ${shoulder.distanceTo(new THREE.Vector3(...point)).toFixed(6)}m)`);
    }
  };
  const bones = entries.map(e => {
    const list: THREE.Bone[] = []; e.object.traverse(n => { if (n instanceof THREE.Bone) list.push(n); }); return list;
  });
  const rest = bones.map(list => list.map(b => ({ p: b.position.clone(), q: b.quaternion.clone() })));
  const data = bones.map(list => list.map(() => ({ positions: [] as number[], rotations: [] as number[] })));
  // t, backward torso pitch, woman's pelvis Y/Z, target wrist Y/Z, man's pelvis Y/Z, man's torso pitch.
  const keys = [
    [0, 0, .875, 0, 1.16, .30, .875, .09, .30],
    [.6, .10, .86, -.015, 1.14, .25, .87, .07, .22],
    [1.2, .55, .80, -.06, 1.08, .10, .855, .15, .4],
    [1.9, 1.0, .75, -.09, 1.06, .04, .85, .22, .55],
    [2.25, 1.0, .75, -.09, 1.06, .04, .84, .22, .55],
    [3.1, .70, .78, -.08, 1.10, .16, .80, .10, .32],
    [4.1, .15, .85, .03, 1.17, .32, .83, .10, .30],
    [4.7, -.12, .86, .08, 1.18, .36, .855, .01, .05],
    [5.4, 0, .875, .07, 1.17, .33, .875, .02, .10],
    [6, 0, .875, .07, 1.17, .33, .875, .02, .10],
  ];
  const at = (t: number) => {
    const i = Math.max(1, keys.findIndex(k => k[0] >= t));
    const a = keys[i - 1], b = keys[i];
    const u = THREE.MathUtils.smoothstep(t, a[0], b[0]);
    return a.map((n, j) => THREE.MathUtils.lerp(n, b[j], u));
  };
  const times: number[] = [];
  const setRotation = (root: THREE.Object3D, name: string, x = 0, y = 0, z = 0) => root.getObjectByName(name)!.rotation.set(x, y, z);
  try {
    for (let frame = 0; frame <= duration * 30; frame++) {
      const t = frame / 30, [, lean, wy, wz, handY, handZ, my, mz, malePitch] = at(t); times.push(t);
      bones.forEach((list, r) => list.forEach((b, i) => { b.position.copy(rest[r][i].p); b.quaternion.copy(rest[r][i].q); }));
      woman.getObjectByName('hips')!.position.set(0, wy, wz);
      setRotation(woman, 'spine', -lean * .3); setRotation(woman, 'chest', -lean * .7);
      setRotation(woman, 'head', lean * .16);
      // Free arm flails backward toward the pond, not forward into her partner.
      setRotation(woman, 'leftUpperArm', .35 + lean * .5, 0, .12 + lean * .95);
      setRotation(woman, 'leftForeArm', .2 + lean * .5);
      man.getObjectByName('hips')!.position.set(0, my, mz);
      setRotation(man, 'spine', malePitch * .4); setRotation(man, 'chest', malePitch * .6);
      setRotation(man, 'head', -.04, -.28);
      setRotation(man, 'rightUpperArm', .25, 0, -.15); setRotation(man, 'rightForeArm', .6);
      // Fixed world ankle targets: the torso/pelvis really falls behind its support,
      // then is pulled forward. No hidden root teleport or sliding feet.
      for (const rig of [woman, man]) {
        for (const [side, sign] of [['left', 1], ['right', -1]] as const) {
          const foot = rig.localToWorld(new THREE.Vector3(sign * .105, .09, 0));
          aimJoint(rig, [`${side}Thigh`, `${side}Shin`, `${side}Foot`], foot.toArray(), [1, 0, 0]);
        }
      }
      // Keep her elbow on her own side of the contact plane during the fall.
      aimJoint(woman, ['rightUpperArm', 'rightForeArm', 'rightHand'], [-.255, handY, handZ], [0, 0, -1]);
      // Man reaches as the arm goes out; then visibly flexes/retracts during pull.
      solveWristContact(man, woman, contact);
      bones.forEach((list, r) => list.forEach((b, i) => { data[r][i].positions.push(...b.position.toArray()); data[r][i].rotations.push(...b.quaternion.toArray()); }));
    }
    entries.forEach((e, r) => {
      const tracks = bones[r].flatMap((b, i) => [
        new THREE.VectorKeyframeTrack(`${b.uuid}.position`, times, data[r][i].positions),
        new THREE.QuaternionKeyframeTrack(`${b.uuid}.quaternion`, times, data[r][i].rotations),
      ]);
      e.object.animations = [new THREE.AnimationClip(r ? '女子后仰失衡-受牵引前收' : '男子伸臂拦住-屈臂回拽', duration, tracks)];
    });
  } finally {
    bones.forEach((list, r) => list.forEach((b, i) => { b.position.copy(rest[r][i].p); b.quaternion.copy(rest[r][i].q); }));
    entries.forEach(e => e.object.updateMatrixWorld(true));
  }
  // The near shore is 25cm behind her ankles, not a water plane under dry ground.
  // Real terrain/water metadata remains authoritative for the unchanged dry gate.
  const size = 20, segments = 128, heights: number[] = [], surfaces: number[] = [], waterLevels: (number | null)[] = [];
  for (let row = 0; row <= segments; row++) for (let col = 0; col <= segments; col++) {
    const x = -size / 2 + col * size / segments, z = -size / 2 + row * size / segments;
    const pond = (x / 3.4) ** 2 + ((z + 3) / 2.75) ** 2 < 1;
    heights.push(pond ? -.65 : 0); surfaces.push(pond ? 2 : 0); waterLevels.push(pond ? -.18 : null);
  }
  const environment = validateSceneEnvironment({ ...defaultSceneEnvironment(), hour: 14,
    terrain: { size, segments, heights, surfaces, waterLevels, seed: 41317, surfaceColors: { '0': '#9b946e', '2': '#47676b' } } });
  const camera = { position: [-3.5, 2.1, 2.0] as [number, number, number], target: [0, .8, -.25] as [number, number, number], fov: 43 };
  const plan = validateScriptShotPlan({ version: 1, sceneId: 'authored-wrist-rescue', heading: '面对面·女子背临水潭后仰 → 男子正面拦住回拉（6秒动作稿）', location: '水潭岸边',
    sourceFingerprint: 'authored-rescue-facing-bank-v2', sceneFingerprint: 'facing-pair-dry-bank-real-pond',
    action: ['0–1.9秒：男女面对面，女子背临水潭，在岸边后仰失衡；男子从她正面前倾伸臂拦住。', '1.9–2.25秒：拦住下坠，支撑脚固定。', '2.25–4.7秒：男子沉身屈臂回拉，女子被牵引向前收回。', '4.7–6秒：晃动后恢复。'],
    diagnostics: ['人工关键帧动作预演，非物理动力学。当前整镜腕部贴合，不包含接触前放手/闭合手指或扑胸。', '蓝色为男子、粉色为女子的程序化成年代理；无成品服装/微表情，不是六镜完整成片。'],
    shots: [{ id: 'rescue-action', order: 1, start: 0, end: duration, camera, dialogue: [], execution: { version: 1,
      actors: entries.map(e => ({ targetId: e.id, targetName: e.name, beats: [{ start: 0, end: duration,
        from: { x: e.object.position.x, z: e.object.position.z }, to: { x: e.object.position.x, z: e.object.position.z }, yaw: e.object.rotation.y,
        animation: { index: 0, name: e.object.animations[0].name, duration } }] })),
      contacts: [{ version: 1, policy: 'whole-skin-separating-plane-v1', sourceId: entries[0].id, targetId: entries[1].id, start: 0, end: duration, normal: [-1, 0, 0], binding: contact }],
    } }],
  });
  return { entries, plan, camera, environment };
}
