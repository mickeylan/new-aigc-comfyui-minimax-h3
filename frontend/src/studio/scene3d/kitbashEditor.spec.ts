import { describe, it, expect, vi } from 'vitest';
import * as THREE from 'three';
import { ObjectHistory, duplicateMesh, surfaceDrop } from './kitbashEditor';
const entry = () => ({ id: crypto.randomUUID(), object: new THREE.Group(), locked: false });
describe('kitbash editor', () => {
  it('undoes add/delete/TRS/visibility/lock, restores redo and releases abandoned resources once', () => {
    const dispose = vi.fn(), h = new ObjectHistory(dispose, 2), a = entry(), b = entry();
    h.checkpoint([a]); a.object.position.x = 2; a.locked = true; a.object.visible = false;
    expect(h.undo([a])).toEqual([a]); expect(a.object.position.x).toBe(0); expect(a.locked).toBe(false); expect(a.object.visible).toBe(true);
    h.redo([a]); expect(a.object.position.x).toBe(2); expect(a.locked).toBe(true);
    h.checkpoint([a]); h.undo([a, b]); expect(dispose).not.toHaveBeenCalled();
    h.checkpoint([a]); h.collect([a]); expect(dispose).toHaveBeenCalledExactlyOnceWith(b);
    h.clear([a]); expect(dispose).toHaveBeenCalledTimes(1);
  });
  it('retains deletion tombstones until clear, without disposing the live scene', () => {
    const dispose = vi.fn(), h = new ObjectHistory(dispose), a = entry();
    h.checkpoint([a]); h.collect([]); expect(dispose).not.toHaveBeenCalled();
    expect(h.undo([])).toEqual([a]); h.redo([a]); h.clear([]); expect(dispose).toHaveBeenCalledExactlyOnceWith(a);
  });
  it('clones mesh geometry/material/texture ownership independently', () => {
    const root = new THREE.Group(), texture = new THREE.Texture();
    const mesh = new THREE.Mesh(new THREE.BoxGeometry(), new THREE.MeshStandardMaterial({ map: texture })); root.add(mesh);
    const clone = duplicateMesh(root), c = clone.children[0] as typeof mesh;
    expect(c.geometry).not.toBe(mesh.geometry); expect(c.material).not.toBe(mesh.material); expect(c.material.map).not.toBe(texture);
    expect(clone.position.x).toBe(.5); c.material.color.set('red'); expect(mesh.material.color.getHex()).not.toBe(0xff0000);
  });
  it('lands on actual surface and excludes hidden content, helpers and splats', () => {
    const a = entry(); a.object.add(new THREE.Mesh(new THREE.BoxGeometry(2, 2, 2), new THREE.MeshBasicMaterial())); a.object.position.y = 2; a.object.updateMatrixWorld(true);
    const ray = new THREE.Raycaster(new THREE.Vector3(0, 10, 0), new THREE.Vector3(0, -1, 0));
    expect(surfaceDrop(ray, [a])?.y).toBe(3); a.object.visible = false; expect(surfaceDrop(ray, [a])?.y).toBe(0);
    a.object.visible = true; expect(surfaceDrop(ray, [{ ...a, splat: true }])?.y).toBe(0);
    a.object.children[0].userData.poseEditorHelper = true; expect(surfaceDrop(ray, [a])?.y).toBe(0);
  });
});
