import test from 'node:test'
import assert from 'node:assert/strict'
import { layoutClips, snapTime, trimTimelineClip, createEditHistory } from '../src/utils/editingTimeline.js'
const clip = {duration:10,source_start:2,source_end:8,speed:2,transition:'cut'}
test('timeline accounts for speed and transition overlaps',()=>{
 const rows=layoutClips([{...clip,transition:'fade',transition_duration:.5},clip])
 assert.equal(rows[0].end,3);assert.equal(rows[1].start,2.5);assert.equal(rows[1].end,5.5)
})
test('trim maps output seconds to source seconds and clamps to physical range',()=>{
 assert.equal(trimTimelineClip(clip,'start',1).source_start,4)
 assert.equal(trimTimelineClip(clip,'end',20).source_end,10)
 assert.equal(trimTimelineClip(clip,'start',20).source_start,7.95)
 assert.equal(snapTime(1.04,[1,2],100),1)
 assert.equal(snapTime(1.04,[1],100,false),1.04)
})
test('immutable history restores edits and drops redo after branching',()=>{
 const history=createEditHistory(3);const value=[{...clip}];history.record(value)
 value[0].source_start=3;history.record(value)
 assert.equal(history.undo()[0].source_start,2);assert.equal(history.redo()[0].source_start,3)
 history.undo();history.record([{...clip,source_start:4}]);assert.equal(history.canRedo,false)
})
