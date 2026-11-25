// Derived from Toonflow-app sceneViewport.ts createPrimitive; Copyright (c) 2026 HBAI-Ltd; MIT.
// See ../THIRD_PARTY_NOTICES.md. Extracted without the upstream viewport/editor host.
import { BoxGeometry, BufferGeometry, CapsuleGeometry, ConeGeometry, CylinderGeometry, Group, Mesh, MeshStandardMaterial, Object3D, PlaneGeometry, SphereGeometry, TorusGeometry } from 'three';
import { capsuleHipHeight, capsuleJointNames, capsuleRigName } from './toonflow/capsuleRig';
import type { SceneAsset } from './toonflow/sceneDocument';
import { createRiggedPerson } from './toonflow/riggedPerson';

export function createToonflowPrimitive(asset: SceneAsset, color: string) {
  if (asset.shape === 'riggedPerson') return createRiggedPerson(color);
  const group = new Group();
  const material = new MeshStandardMaterial({ color, roughness: 0.72, metalness: 0.05 });
  function addEyes(y: number, z: number) {
    const eyes = new Mesh(new BoxGeometry(0.17, 0.035, 0.05), new MeshStandardMaterial({ color: '#2b2f36', roughness: 0.6, metalness: 0.05 }));
    eyes.material.userData.fixedColor = true;
    eyes.position.set(0, y, z); group.add(eyes);
  }
  function add(geometry: BufferGeometry, x = 0, y = 0, z = 0) {
    const mesh = new Mesh(geometry, material);
    mesh.position.set(x, y, z); mesh.castShadow = true; mesh.receiveShadow = true; group.add(mesh);
  }
  if (asset.shape === 'person') {
    const rig = new Group(); rig.name = capsuleRigName; group.add(rig);
    const joint = (name: string, parent: Object3D, y: number) => { const node = new Object3D(); node.name = name; node.position.y = y; parent.add(node); return node; };
    const hips = joint(capsuleJointNames.hips, rig, capsuleHipHeight);
    const torso = joint(capsuleJointNames.torso, hips, 0);
    const neck = joint(capsuleJointNames.head, torso, 1.44 - capsuleHipHeight);
    const leg = joint(capsuleJointNames.leg, hips, 0);
    const part = (parent: Object3D, geometry: BufferGeometry, y: number) => { add(geometry, 0, y); parent.attach(group.children.at(-1)!); };
    part(neck, new SphereGeometry(0.15, 24, 16), 1.59);
    part(torso, new CapsuleGeometry(0.24, 0.44, 8, 24), 1.02);
    part(leg, new CapsuleGeometry(0.09, 0.44, 6, 16), 0.31);
    addEyes(1.6, 0.13); neck.attach(group.children.at(-1)!);
  } else if (asset.shape === 'seatedPerson') {
    add(new SphereGeometry(0.15, 24, 16), 0, 1.16, -0.06);
    add(new CapsuleGeometry(0.22, 0.22, 8, 24), 0, 0.76, -0.08);
    add(new CapsuleGeometry(0.1, 0.32, 6, 16).rotateX(Math.PI / 2), 0, 0.5, 0.14);
    add(new CapsuleGeometry(0.085, 0.3, 6, 16), 0, 0.24, 0.38); addEyes(1.17, 0.07);
  } else if (asset.shape === 'kneelingPerson') {
    add(new CapsuleGeometry(0.08, 0.36, 6, 16).rotateX(Math.PI / 2), 0, 0.08, -0.08);
    add(new CapsuleGeometry(0.1, 0.24, 6, 16).rotateX(Math.PI / 2), 0, 0.24, 0.06);
    add(new CapsuleGeometry(0.22, 0.2, 8, 24), 0, 0.6, -0.1);
    add(new SphereGeometry(0.15, 24, 16), 0, 1.02, -0.08); addEyes(1.03, 0.06);
  } else if (asset.shape === 'sphere') add(new SphereGeometry(0.75, 32, 20), 0, 0.75);
  else if (asset.shape === 'cylinder') add(new CylinderGeometry(0.6, 0.6, 1.8, 32), 0, 0.9);
  else if (asset.shape === 'cuboid') add(new BoxGeometry(2.4, 1.2, 1.2), 0, 0.6);
  else if (asset.shape === 'cone') add(new ConeGeometry(0.75, 1.8, 32), 0, 0.9);
  else if (asset.shape === 'frustum') add(new CylinderGeometry(0.4, 0.75, 1.5, 32), 0, 0.75);
  else if (asset.shape === 'torus') add(new TorusGeometry(0.65, 0.2, 16, 48), 0, 0.85);
  else if (asset.shape === 'disc') add(new CylinderGeometry(0.9, 0.9, 0.08, 48), 0, 0.04);
  else if (asset.shape === 'pyramid') add(new ConeGeometry(1.5 / Math.sqrt(2), 1.5, 4).rotateY(Math.PI / 4), 0, 0.75);
  else if (asset.shape === 'capsule') add(new CapsuleGeometry(0.4, 1, 8, 24), 0, 0.9);
  else if (asset.shape === 'backdrop') {
    const panel = new Mesh(new PlaneGeometry(6, 3.4), material);
    panel.position.set(0, 1.7, 0); panel.receiveShadow = true; panel.userData.backdrop = true; group.add(panel);
  } else if (asset.shape === 'plane') add(new BoxGeometry(3, 0.08, 3), 0, 0.04);
  else if (asset.shape === 'box') add(new BoxGeometry(1.5, 1.5, 1.5), 0, 0.75);
  else { material.dispose(); throw new Error(`不支持的基础形状：${asset.shape}`); }
  return group;
}
