import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
const s=readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx',import.meta.url),'utf8');
test('存档归属独立于提示词项目，使用真实项目标签',()=>{
 const save=s.slice(s.indexOf('const saveSceneToServer'),s.indexOf('// 本地文件与后端存档'));
 assert.match(save,/Number\(archiveProjectId\)/);assert.doesNotMatch(save,/promptProjectId/);
 assert.match(s,/aria-label="新存档项目"/);assert.match(s,/\{p.label\}/);
});
test('列表分页和项目筛选有独立上下文',()=>{
 assert.match(s,/page_size=10/);assert.match(s,/archiveTotal\/10/);
 assert.match(s,/aria-label="存档列表项目筛选"/);assert.match(s,/<option value="0">无项目归属/);
 assert.match(s,/上一页/);assert.match(s,/下一页/);
});
test('改名与回收须明确操作及二次确认',()=>{
 assert.match(s,/method: 'PATCH' \| 'DELETE'/);
 assert.match(s,/JSON.stringify\(\{ name: renameValue.trim\(\) \}\)/);
 assert.match(s,/确认移入回收区/);assert.match(s,/取消删除/);assert.match(s,/原始文件保留/);
});
