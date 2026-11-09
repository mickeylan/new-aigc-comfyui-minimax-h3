import test from 'node:test'
import assert from 'node:assert/strict'
import { defaultTransform,normalizeTransform,transformAt } from '../src/utils/clipTransform.js'
import { normalizeClip,splitClip,buildEditSequence,serializeDraft } from '../src/utils/finalCutEditing.js'
const tr={...defaultTransform(),fit:'cover',crop_left:.1,keyframes:[{time:0,zoom:1,x:0,y:0,rotation:0},{time:4,zoom:2,x:.4,y:0,rotation:90}]}
test('linear output-local animation holds endpoints and interpolates all pose fields',()=>{
 assert.equal(transformAt(tr,-1).zoom,1);assert.equal(transformAt(tr,10).zoom,2)
 assert.deepEqual(transformAt(tr,2),{zoom:1.5,x:.2,y:0,rotation:45})
})
test('invalid transforms, keyframe ordering and duration overflow reject',()=>{
 for(const value of [{...tr,zoom:0},{...tr,rotation:Infinity},{...tr,crop_right:.9},{...tr,keyframes:[tr.keyframes[1],tr.keyframes[0]]},{...tr,keyframes:[{...tr.keyframes[0],time:5}]}])assert.throws(()=>normalizeTransform(value,4))
})
test('split re-bases animation without jumps and persists through drafts and export',()=>{
 const clip=normalizeClip({scene_id:1,duration:10,source_start:1,source_end:9,speed:2,transform:tr})
 const [left,right]=splitClip(clip,5)
 assert.equal(left.transform.keyframes.at(-1).zoom,1.5)
 assert.equal(right.transform.keyframes[0].time,0)
 assert.equal(right.transform.keyframes[0].zoom,1.5)
 assert.equal(right.transform.keyframes.at(-1).time,2)
 assert.deepEqual(buildEditSequence([left,right])[0].transform,left.transform)
 assert.deepEqual(JSON.parse(serializeDraft([right])).clips[0].transform,right.transform)
 assert.throws(()=>normalizeClip({...clip,source_end:3}),'must not silently squeeze keyframes when trimming')
})
