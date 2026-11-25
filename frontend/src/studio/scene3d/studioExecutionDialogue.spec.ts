// @vitest-environment jsdom
import { webcrypto } from 'node:crypto';
import { afterEach, expect, it, vi } from 'vitest';
import { snapshotDirectorSource } from './directorSource';
import { adaptStudioExecutionDialogue } from './studioExecutionDialogue';
afterEach(() => vi.unstubAllGlobals());
async function fixture() {
 vi.stubGlobal('crypto',webcrypto);
 const source=await snapshotDirectorSource(1,{id:2,project_id:1,title:'院落',location:'院落',content:'甲在画外说：“别动😀。”',director_source_stale:false},[
 {id:9,scene_id:2,order:1,duration:3,dialogue:'过期展示缓存',dialogue_ranges:[{dialogue_id:3,group_key:'g',start_rune:0,end_rune:4}],action_timeline:[]},
 {id:10,scene_id:2,order:2,duration:3,dialogue_ranges:[],action_timeline:[]},
 ]);
 const speech={dialogues:[{id:3,scene_id:2,project_id:1,text:'别动😀。',character:'甲',speech_type:'offscreen_dialogue'}],dialogue_timeline:{version:1,lines:[{shot_id:9,dialogue_id:3,speaker:'甲',speech_type:'offscreen_dialogue',text:'别动😀。',start:0.5,end:2}],blockers:[]}};
 return {source,speech};
}
it('maps exact canonical speech and timing, not stale display text or guessed duration',async()=>{
 const {source,speech}=await fixture();const before=JSON.stringify(speech);const result=adaptStudioExecutionDialogue(source,speech);
 expect(result.blockers).toEqual([]);expect(result.byShot.get(9)).toEqual([{index:0,speaker:'甲',line:'别动😀。',start:0.5,end:2}]);expect(result.byShot.has(10)).toBe(false);
 expect(JSON.stringify(speech)).toBe(before);expect(result.provenance).toEqual(speech);
});
it('fails closed on incomplete rows and preserves server blockers',async()=>{
 const {source,speech}=await fixture();speech.dialogue_timeline.lines=[];speech.dialogue_timeline.blockers=['跨镜rune分段时序缺失'];
 const r=adaptStudioExecutionDialogue(source,speech);expect(r.byShot.size).toBe(0);expect(r.blockers.join()).toContain('跨镜rune');expect(r.blockers.join()).toContain('不可静默丢弃');
 expect(adaptStudioExecutionDialogue(source).blockers.join()).toContain('尚未读取权威对白');
});
it('rejects altered text, speaker, time, shot, source identity and duplicate rows',async()=>{
 for(const change of [(s:any)=>s.dialogue_timeline.lines[0].text='改写',(s:any)=>s.dialogue_timeline.lines[0].speaker='乙',(s:any)=>s.dialogue_timeline.lines[0].end=4,(s:any)=>s.dialogue_timeline.lines[0].shot_id=99,(s:any)=>s.dialogues[0].scene_id=5,(s:any)=>s.dialogues.push({...s.dialogues[0]})]){
 const {source,speech}=await fixture();change(speech);expect(()=>adaptStudioExecutionDialogue(source,speech)).toThrow();
 }
});
it('keeps two explicitly timed lines in original mirror order without re-splitting shots',async()=>{
 const {source,speech}=await fixture();const shots=structuredClone(source.shots);shots[1].dialogue_ranges=[{dialogue_id:4,group_key:'g2',start_rune:0,end_rune:2}];
 const updated=await snapshotDirectorSource(1,source.scene,shots);
 speech.dialogues.push({id:4,scene_id:2,project_id:1,text:'好。',character:'乙',speech_type:'dialogue'});
 speech.dialogue_timeline.lines.push({shot_id:10,dialogue_id:4,speaker:'乙',speech_type:'dialogue',text:'好。',start:3.2,end:4.1});
 const r=adaptStudioExecutionDialogue(updated,speech);expect(r.blockers).toEqual([]);expect(r.byShot.get(10)).toEqual([{index:1,speaker:'乙',line:'好。',start:3.2,end:4.1}]);
});
