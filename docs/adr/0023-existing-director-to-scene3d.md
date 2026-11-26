# ADR-0023：复用已有小说理解与导演结果，衔接可执行3D预演

- 日期：2026-11-25
- 状态：方向经用户确认，分项实施中；不是整链路验收通过

## 背景与目标

核心目标是小说到可编辑动画预演。小说理解已存在，不再开发第二套分析器。
后端 `NovelAnalysisService` 已提供章节/窗口分析及故事圣经；`scene_director_draft.go`
已有场次分析、表演节拍和镜头规划；`models.Scene` / `models.Shot` 已承接落地结果。
缺口是这些已有语义数据与 scene3d 的布景、动作资源和摄像机之间的执行适配。

当前 `draftShotDirection` 仍需要显式对象/clip映射，且限制单主体；
`ScriptShotPlan` 并非后端 Shot 的同构表示，不能把每句台词再拆一镜来替代已有导演设计。
自然语言 blocking / camera_movement 不等于空间坐标或真实动作资源。

## 决策

1. 复用已有分析链，不新增小说分析入口，不改既有 Skill、提示词、用户模板。
2. 首先接收已存在的 Scene + Shots：保留项目/场次/镜头ID、原顺序、时长、导演来源、
   表演、动作时间轴和对白范围。输入来自既有只读API，不调用导演生成端点，不写业务库。
3. 增加独立的内存来源快照契约，不能把后端导演数据伪装成已经可播放的 ScriptShotPlan。
   原字段及未知字段完整复制保留；来源修订标识用于拒绝迟到结果和使旧确认失效。
   此标识是内容一致性键，不是安全签名。
4. 后续执行规划以此快照为输入，结合真实布景/对象/骨骼clip目录生成建议；
   缺资产、无法映射动作、多主体超出现有采样器能力等须列为阻塞。
   自动适配是目标，不能把所有任务退回人工完成而宣称核心功能交付。
5. 结构合法、语义符合剧情、动作可执行、视觉验收是四种不同信号。
   原始动作文字必须保留，不能以通用走路或静止画面冒充剑术/交互等复杂表演。
6. 用户审阅确认后才原子应用到编辑场景；来源、资源或场景改变后旧确认失效。
   不自动提交图像/视频任务，不修改正式Scene/Shot，不改变现有v6/v7存档契约。
7. 新增模型提示词如确有必要，先原文展示并获批准，再实施；不得借此ADR绕过批准。

## 小项与验收

- A：已有导演结果来源适配。测试非手写分场输入、无对白动作镜头、镜头顺序与时长、
  动作时间轴原样保留、旧导演来源拒绝、跨场次/重复ID/无效时长拒绝、来源变化键改变、
  不修改输入且不产生网络或写操作。
- B：只读项目/场次选择与来源审阅入口，加载竞态和取消不污染场景。
- C：基于现有资产的布景/表演/镜头执行建议与缺口报告，接真实编辑器预览。
- D：确认应用、播放、保存回读、导出与代表性小说样例的端到端验收。

A完成不等于B–D完成，不得把“数据接通”报告成“小说自动生成准确动画”通过。
不重做理解层、不新增框架、不并入video-editor；本轮不提交或推送，不运行H3。

## 执行规划入口（第二小项）

用户已逐字批准独立“3D预演执行规划器”系统提示词，新增Skill v1；不修改既有Skill。
后端新增独立只读来源与规划端点，从业务库重取Scene/Shot，使用服务端来源哈希拒绝旧稿，
调用前后复核来源；客户端不能提交一段替代正文让模型绕过现有导演设计。
规划调用仅允许写既有Skill审计，不保存Scene/Shot/素材/预演、不创建媒体任务。
资产和当前对象目录由编辑器按实际资源提交，只代表本次目录快照，不作为服务端已核实资产的证明；
应用前须由编辑器重新校验真实资源。服务端校验JSON、来源、镜头覆盖、时长和目录引用。
首版执行结构支持目录资产空间摆放、复用对象、固定机位和单对象直线clip走位；
运镜、原地骨骼表演及复杂多主体动作仍不能冒充已有采样器支持，必须列出阻塞。
模型草稿不等于已可播放；本小项先交付后端规划与隔离测试，编辑器应用及真实模型质量另验。

## 首轮实施与验证

- A 已实施：`directorSource.ts` 复制完整既有Scene/Shot JSON，验证项目/场次、顺序、
  时长、动作区间及过期状态；SHA-256修订键包含原文、来源和未知字段，不生成可播放计划。
- B 已接UI：整套布景面板按需加载“从已有小说项目读取导演结果”，显式选项目/场次，
  只读GET并展示已有原文、镜头和时间轴；变更选择/卸载取消旧结果。两次读取场次发现变化时拒绝；
  现有API没有事务快照，因此不声称并发一致性已完整解决，应用前仍须重新校验。
- C/D 未实现；未接模型执行规划、真实资产自动绑定、确认应用或端到端小说动画验收。
- 直接适配器冒烟：`DIRECTOR_SOURCE_SMOKE=PASS`，exit0。
- 专项3文件40项通过，exit0；完整Vitest 43文件356项通过，exit0。
- `npm --prefix frontend run build` exit0；来源入口独立chunk 7.05kB（gzip 3.20kB），
  原有大chunk警告仍在。
- `npm --prefix frontend test` exit1：Node阶段的“已跟踪文件的相对 import 都指向已跟踪文件”
  失败，原因 `SetTemplatePanel.tsx -> DirectorSourcePanel.tsx` 新组件尚未纳入Git。
  紧凑Node诊断再次exit1确认同一原因后停止该路径；未改测试、未暂存或提交以绕过检查。
  因此前端完整test命令不能报通过（它在Node阶段停止，完整Vitest是另外独立执行的）。
- 组件编译首轮检查exit1 `missing UI`：esbuild默认ASCII转义导致中文字面量检查不匹配；
  夹具指定charset=utf8后相同文字断言通过，未改产品来迁就检查。
- 未访问真实业务数据；UI测试使用隔离响应，未做新入口浏览器/真实后端联调。
- 原生standard-change运行停在ADR用户决策节点；用户已通过提问工具批准先记录，
  当前工作流工具无决策提交字段，未宣称工作流完成。

## 执行规划入口接入与回归（2026-11-26）

- C 部分实施：前端捕获完整真实素材目录和可用场景对象/已挂载clip，生成目录修订；
  显式点击后重取服务端来源，再调用已批准的独立Skill生成待审草稿。模型回复后再捕获目录，
  若来源、资源修订或选择会话变化，拒绝或丢弃结果。此处“取消”是界面结果失效，
  不表示已经中止模型计算或计费。只展示草稿JSON和阻塞，不应用到编辑器。
- 实测素材目录11750条，服务端目录上限16384；真实上下文约1164195字节。
  请求超过2MiB直接拒绝、不静默截断。尚未验证真实模型的上下文容量、耗时与输出质量。
- 后端独立复核JSON、原文依据、镜头ID/顺序/时长、可见角色、目录资产和所属clip；
  未绑定的可见角色/表演、不能执行的运镜或动作结构列为阻塞，不伪装成执行成功。
  完成模型调用后来源发生变化则返回409，不向客户端交付可用计划。
- 增补UI测试：显式规划仅POST规划端点、请求使用服务端来源哈希与真实目录快照；
  资源变化拒绝、导演来源变化不调用模型、切换来源忽略迟到回复、模型失败不伪造草稿。
  增补后端测试：模型调用期间修改表演返回409；未知请求字段及无效目录在调用模型前拒绝。
- `go -C backend test ./...` exit0；随后新增并发来源测试后，
  `go -C backend test ./internal/service -run '^TestStudioExecution' -count=1` exit0。
- 完整Vitest `npm --prefix frontend exec -- vitest run --root frontend --reporter=default`
  exit0，45文件363项通过。此前同范围一轮exit1（362通过/1失败），新UI夹具在并发负载下
  用固定20ms等待WebCrypto，导致控件尚未出现；改为等待真实控件出现且可用，未放宽
  测试超时、未改变预期或产品行为；专项4项及随后完整363项通过。
- `npm --prefix frontend run build` exit0；来源面板独立chunk 9.02kB（gzip 3.75kB），
  完整目录和3D宿主的大chunk警告仍在。前述`npm test`的Git跟踪检查失败仍未解除，
  不把独立Vitest通过等同于完整`npm test`通过；未暂存、提交或推送以绕过检查。
- 未使用真实模型、未做浏览器/真实后端联调；未应用、播放、保存回读或导出执行草稿。
  C的编辑器预览和D端到端验收仍未完成，不能据此宣称小说自动生成准确动画已交付。

## 确认应用适配（2026-11-26，后续小项）

- C 增加独立应用边界 `studioExecutionAdapter.ts`：严格拒绝未知字段、非法素材/对象/clip、
  旧修订、镜头覆盖或时长变化、非原文依据、画外人物入画；不按台词重新拆镜。
  保留后端镜头ID、顺序和原时长，转换到现有 `ScriptShotPlan` 的固定机位/单对象XZ走位。
  完整权威来源、原始表演与动作时间轴、依据/假设及绑定通过既有action/diagnostics字段保留，
  不增加v6/v7存档字段或修改存档版本，也不覆盖用户已保存的执行稿。
- `studioExecutionApply.ts` 负责应用事务：按实际目录ID构建素材，沿用既有布景的米制定位和
  配方重建路径，保留草稿角度（弧度）、缩放和世界位置；已有对象不克隆、不替换素材。
  来源与真实目录在物化前后复核；编辑器同步指纹、场景epoch、来源选择及预览会话变化会拒绝。
  所有新增几何先暂存，最后同步提交；失败释放暂存资源，不留下半个布景。
  来源API仍非事务锁，两次读取不能证明提交后后端不再编辑，不声称跨端强一致性已解决。
- UI 新增独立勾选确认与应用按钮：说明追加布景、更新复用对象及替换当前镜头表。
  未勾选不调用应用；失败保留草稿但清除确认；切换来源/卸载后旧事务失效。
  Stage在确认应用开始时还原旧播放基线，再校验；提交失败回滚复用对象位置/角度/缩放/名称及
  新增场景子对象，不替换当前镜头表。成功后不自动播放、不保存、不导出、不触发媒体任务。
- 执行阻塞不因人工勾选而被绕过：缺机位/可见角色/表演、多段动作、运镜、不能兑现的Y高度
  和超出采样器精度的动作时长均拒绝生成可播放替代。Shot对白缓存和rune引用不是权威对白与
  时间映射；含对白的镜头当前明确阻塞应用，不静默丢弃或猜台词。后续需复用现有结构化对白
  及其时序，不能通过删除对白或修改测试来获得“可播放”。动作clip的语义正确性仍未量化。
- 新增测试20项：适配9项、真实目录物化/重建及事务6项、执行生产Stage回调2项、UI确认3项。
  覆盖了真实配方存档重建、来源/资源改变、伪造目录、失败资源回收和注入发布异常后的回滚；
  不是仅以mock回调存在作为应用成功信号。Stage既有27项生产回调回归亦通过。
- 完整Vitest `npm --prefix frontend exec -- vitest run --root frontend --reporter=default`
  exit0，49文件383项通过；`npm --prefix frontend run build` exit0，应用模块按需chunk
  8.12kB（gzip 3.79kB），大chunk警告保留。完整`npm test`既有未跟踪文件检查阻塞仍在，
  未暂存、提交或推送绕过它。
- 新模块编译检查首轮exit1 `ERR_MODULE_NOT_FOUND: esbuild`，原因根目录没有该依赖；
  改为从frontend/package.json解析项目已有工具链后 `EXECUTION_ADAPTER_COMPILE=PASS`。
  UI新夹具首轮通过但出现act警告，改为在act中等待可观察就绪；未改断言、未抑制警告。
- 本小项未改提示词、Skill、用户H3模板及后端业务数据；未运行真实模型或H3。
  C已接入确认应用代码与隔离夹具，但代表性小说的真实模型→浏览器播放→完整保存回读→导出
  尚未验收；D仍未完成。新增布景的视觉效果、角色外观和具体动作语义不能据此宣称准确。

### C 补项：既有权威对白 → 确认应用的镜头字幕（2026-11-26）

- 读取来源时同步读取既有 Dialogue，并仅复用明确时间段或既有解析器支持的
  `【0.5—2秒】` 来源标记；不按字数估时，不改台词，不写回业务数据。
  Dialogue 与解析后的 timeline 同时进入 source_hash；模型调用中修改台词时序返回409，
  应用前后重取来源发现变化也不发布。
- 只接完整且唯一引用的对白；局部rune、跨镜切分、同组多行、不完整关联、缺时序、
  超镜时长均阻塞。现有字幕采样一次只显示一行，所以重叠/乱序对白同样阻塞，
  不静默隐藏后一句，也不自动重排或拆镜。
- 前端以权威行填充既有 ScriptTimedLine（全时间轴秒数、连续index、原文与说话人），
  不使用 Shot.dialogue 展示缓存；画外对白仅进入字幕，不创建可见角色或音频。
  SpeechType 与权威来源置于既有 diagnostics，未增加v6/v7存档字段或改版本。
- 验证：`go -C backend test ./...` exit0；
  `npm --prefix frontend exec -- vitest run --root frontend --reporter=dot`
  exit0，51文件389项通过；`npm --prefix frontend run build` exit0，
  studioExecutionApply chunk 10.48kB（gzip 4.60kB），大chunk警告保留。
  隔离集成断言覆盖确认应用→JSON往返→真实播放采样器的精确字幕起止与相机还原，
  以及来源变化后的零发布；这不是完整场景存档、浏览器或视频导出的端到端验收。
- 失败记录：新增哈希测试首轮exit1，夹具误新增第二条对白，造成既有行未关联；
  改为更新隔离夹具已有权威行后，原有唯一行及文本变动哈希断言通过。
  新集成用例专项通过但全量首轮exit1：`Test timed out in 5000ms`，388项通过、1项超时；
  原因是并发时在用例内首次加载11750条真实目录。改为模块准备阶段静态加载相同真实目录，
  未mock、未调高超时、未改断言；专项2项与全量389项随后通过。
- 完整`npm test`的Git未跟踪导入检查仍为已知阻塞，未重试、未暂存或提交绕过。
  本补项未改Skill/提示词/H3模板，未运行真实模型或生成媒体；真实模型剧情/动作质量、
  真实浏览器联调与D项完整闭环仍未验收。未提交、未推送。

### 用户授权提交与推送时的复核（2026-11-26）

- 用户明确要求提交并推送后，按精确文件清单正式暂存导演执行功能组，排除用户H3模板、
  所有临时产物与业务数据；此前未跟踪文件导入检查的阻塞因此解除，而非修改测试绕过。
- `npm --prefix frontend test`（项目原始完整命令：Node测试后执行Vitest）exit0；
  保留本轮出现的React act警告、Three法线规范化及WASM实验警告，不以警告消失为通过依据。
- 暂存新增ADR时发现尾部空行，移除纯空行后`git diff --cached --check` exit0。
  本次仅提交与推送既有改动，不追认真实模型、浏览器联调、导出或原生workflow完成。

## 三项缺口续作契约（2026-11-26）

用户要求继续解决小说执行、视频seek解码和来源回溯三个缺口。本轮不提交、推送或部署；不访问业务数据库，不启动/连接ComfyUI，不提交真实生成。

- 镜头表增加显式可选 `execution.version=1`，不改变原计划version=1或场景v6/v7；旧档缺字段不启用新语义。支持镜内线性插值机位键、多个角色的有界顺序clip段和原地动作；每镜最多32角色/128段/64机位键，保留原镜ID、顺序和时长。与旧motion混用明确拒绝，不静默丢弃任一方。
- 动作段必须引用实际对象挂载clip；每段以原始action_timeline索引、主体绑定和精确区间审计，不凭动作名称证明剑术/持物等语义。缺资源、缺依据、无法完整覆盖则阻塞。角色间及静态障碍不能证明安全时拒绝；运镜只提供线性空间取样，不宣称遮挡/穿模质量验收。
- 播放、参考包和视频继续使用同一绝对时间采样器；随机跳转一致，停止恢复对象/骨姿/机位；人工确认后原子应用。保留人工修订，不自动生成、保存或导出。既有系统提示词及Skill不修改；仅能力/结构数据随实现同步，不扩大模型权限。
- seek问题先复现并控制变量核实编码/封装，不靠降FPS、降清晰度、增超时或自动重试掩盖。无法确认根因时保留失败和范围，不宣布修复。
- 来源回溯用隔离SQLite/HTTP可控provider覆盖素材→Scene→真实任务创建→可控完成→输出身份→历史快照，不冒充真实模型通过。真实ComfyUI闭环本轮SKIPPED；旧机失败、新机成功与本轮证据分别记录。

### 本轮实现与实际边界

- 前后端接入可选execution：机位键覆盖整镜且段内视点不能重合；多个角色可绑定顺序真实clip段、原地动作和显式yaw。后端规划与前端应用均校验真实对象/所属clip、索引/名称/时长、sourceIndex的原区间及每个主体完整覆盖；整份计划拒绝执行角色与旧motion混用。旧档保持原语义，未改提示词或Skill。
- 统一采样器增加绝对时间机位/多角色采样、跨镜端点重放与停止还原；播放/参考包/视频沿用此同一实现。执行前以编辑基线的XZ包围体半径加0.35米保守边距检查静态障碍、连续相对路径检查角色间距，不宣称任意骨骼动画全程体积碰撞或机位遮挡已证明。
- 已验证：受控双人物小说片段（沈青/林禾院中相遇）保留原文/镜头ID/时长/动作时间轴，审计确认应用一次；各主体绑定同一原始顺序时间轴的精确区间，取样骨姿和移动机位数值正确，乱序跳转一致、停止还原。是手工可控执行草稿，不是真实模型规划质量或浏览器视觉验收。
- 已验证：生产GLB导出/存档载入回调在v6/v7均保留execution及真实蒙皮角色clip，载入后可取样且骨姿精确还原；无execution的旧镜头不启用新语义。最初纯骨骼无蒙皮夹具和普通网格动画夹具分别被载入/保存契约拒绝；停止试改并核对已有生产蒙皮夹具后复用真实导出路径，保留并加强数值断言，未放宽校验。
- 代表样例首轮因手工夹具把并发主体写成重叠的源时间段而被既有directorSource拒绝（exit1）；改成既有“顺序时间段、每段多个主体”格式，两个角色各完整绑定每段，专项2项通过。没有改原有导演时间轴契约或弱化断言。
- 机位/多角色轨道目前由导演草稿路径产生，尚无逐段人工编辑UI；既有人工旧走位入口不冒充新轨道编辑器。代表性小说真实模型→浏览器审阅→完整存档→参考包/真实编码视频的一次整链仍未验收，D及原有大场景/跨设备/许可未通过项不转为完成。
- seek对照：Chrome158默认路径两份旧片60秒seek均decode失败，复制码流remux仍失败；独立profile禁用硬件视频解码后均恢复至61.79秒（readyState4/error=null）。限定为本机平台/硬件解码路径相关；没有产品编码修复证据，不自动切换用户硬件加速，不追认默认路径/原片全片通过。详见高性能清单T01和ADR-0022补记。
- 来源链：共享及本项目目录两种夹具均通过真实上传/Scene回读/CreateTask、受控HTTP完成/生产输出接口身份、GenerationSnapshot的素材ID及派生清单不可变；中文主稿存在但英文执行稿为空时任务数0、首尾帧保持。受控输出仅标记字节，不代表可播放模型输出；本轮真实ComfyUI **SKIPPED（本机无法运行 ComfyUI）**。
- 原生standard-change仍因本地Chrome profile文件锁而blocked，未宣称workflow完成。`npm --prefix frontend test` exit1（Node669/670）：Git跟踪检查拒绝已有文件导入未跟踪新模块`shotExecution.ts`，Vitest阶段未执行；不暂存/提交绕过，也不把独立Vitest通过报成完整npm test通过。
- 只读复核前的独立完整Vitest：`node frontend/node_modules/vitest/vitest.mjs run --root frontend` exit0，53文件/400项；`go -C backend test ./... -count=1` exit0（service 102.664s）；`npm --prefix frontend run build` exit0；`git diff --check` exit0。保留既有React act、GLTF法线规范化、大chunk及动态/静态导入混用警告，不以警告消失作为通过信号。
- 只读复核前的本地EXE已在备份旧产物后重建（后续浮点修复后须再次构建，不能用此哈希声称最新交付），未启动/替换运行中服务。`go -C backend build -o comfyui-console.exe .` exit0；95项前端资源与嵌入目录、EXE内字节逐项一致，许可声明source/dist/embed/exe四处相同（不等于法律合规已完成）。76032512字节，SHA-256 `9f3f1f6bf3cf599e118c90be733650c8bb056707df7ed66d89c2ee690c205bff`；vcs.revision为基线78b3f90且vcs.modified=true，不能把未提交构建称为78b3f90干净发布版。备份在`.rscode/gap-deliverable-backup-20261126-105159`；EXE/dist/诊断产物不入库。
- 只读复核发现小数镜长累加后`end-start`与原时长不逐位相等，会误拒后镜机位键/动作末端。沿用现有1e-6秒时间容差，只处理累加浮点边界，不改写源区间或放宽sourceIndex精确匹配；新增上下舍入及真正越界拒绝回归，专项6项exit0。修后串行`go -C backend test ./... -count=1` exit0（service 44.793s）、独立完整Vitest exit0（53文件/401项）、`npm --prefix frontend run build` exit0；Monitor的最终退出码为0。随后仅澄清后端fallback注释，英文兼容及来源链专项再次exit0（service 0.782s）。完整`npm test`的跟踪文件门禁仍未通过，不以这些独立通过替代。
- 复核提出的“确认哈希硬门禁”不直接采纳：现有`TestSavedEnglishPromptBypassesTranslationFreshnessGate`明确要求已保存英文稿兼容、不受来源漂移拦截。保留此行为并修正注释；新增非空门禁不证明确认哈希有效。上述兼容专项与来源链专项exit0；不得把“空英文稿零任务”扩大解释为“所有未确认稿均禁止提交”。模板fallback仍可重新assemble提示词，注释已限定到当前选择，不能声称所有分支均逐字提交。
- 保留运行时按真实包围体的严格间距，不采纳降至0.7米换通过的建议。此前应用只做结构校验，实际几何阻塞延后到播放/导出；本轮已补应用前同源证明，详见下节。单独保存草稿仍不等于几何可播放保证，应用后的编辑也可能令计划失效，播放/导出继续重新验证。
- C4应用前缺口本轮已修：前后端结构校验均检查整计划角色并集≤32；确认应用前检查真实包围体半径≤10米、角色间距、静态障碍及逐段地形水域/坡度安全，包含跨镜held角色。静止段继续执行现有可行走证明，不免证、不降低阈值。
- 相邻未处理项：`SetTemplatePanel.tsx`的`verifyMotionTargets`门槛可能跳过执行角色检查；移动execution机位的参考包provenance仍用固定机位表述；面板仍按`shot.camera`计数；新增部分错误为英文。以上未声称已修复。
- 浮点修复后的上一版本地产物（已由下节应用前预检版本取代）：`backend/comfyui-console.exe`，76032512字节，SHA-256 `b6869332117c6b57990058c0b93c6016bb879a6a476ae683acae0d1194e931d1`。旧EXE/嵌入目录已备份到`.rscode/gap-deliverable-backup-20261126-110033`；嵌入新dist后`go -C backend build -o comfyui-console.exe .` exit0，`node .rscode/verify-gap-deliverable.mjs` exit0（CURRENT_DELIVERABLE=PASS，95项资源逐字节一致，notices四处一致）。构建信息为windows/amd64、CGO_ENABLED=0、vcs.revision=78b3f90、vcs.modified=true；不是干净提交发布版，也不代表许可合规。未启动/重启服务，未提交/推送/部署。

### 应用前几何预检与真实文本小样续验（2026-11-26）

- 新增同步 `assertScriptShotApplicationSafe`，在拟应用的真实位置/旋转/缩放与新增对象上运行同一 `assertScriptShotWalkable` / `compileShotNavigation`。`finally`按编辑器Euler契约还原借用变换；不await、不发布、不写历史。生产Stage的执行计划确认在checkpoint之前调用，单独替换镜头表也在setScriptShots之前调用。失败原子拒绝；它不替代应用后每次播放/参考包/视频的再预检。
- 生产Stage专项4项exit0：结构合法但1米间距的双角色计划拒绝，checkpoint/collect/setScriptShots/syncObjects均0次；6米安全计划应用一次。几何专项5项exit0：真实包围体间距、拟应用墙/缩放、变换还原、超大包围体、跨镜held角色、水域；没有通过减小半径或跳过静止段取巧。新增Stage用例初轮因夹具targetName与应用后名称不符失败，修夹具为真实对象名称后通过，未放宽绑定规则。
- 角色并集前移：前端结构验证新增34角色拒绝回归；后端新增32边界接受/34拒绝，且两镜各17分别通过完整校验。后端新增夹具首轮因共享素材ID违反`object:ID`实际挂载契约失败；给每个对象各自挂载真实clip身份后通过，没有修改身份校验。后端轨道专项exit0。
- 可见Chrome夹具页19049有标题、播放/跳60秒按钮与状态，不再以空白文档冒充媒体验收。默认49210：首帧1280×720正常，seek后`PIPELINE_ERROR_DECODE`，exit1；Media域实际记录`D3D11VideoDecoder`、`kIsPlatformVideoDecoder=true`、D3D11 backend，错误链`d3d_video_decoder.cc:912`→`D3DStatus::6`（:667，VDA Error=0）→`DecoderStatus::1`，最终renderer的`DecoderStatus::108`。软件对照49211使用`VpxVideoDecoder`、platform=false，seek后61.6437秒、readyState4、seeking=false、error=null，exit0。证据在`.rscode/visible-decode-49210-0.{png,json}`与`49211-0`对应文件。定位到本机D3D11硬件VP9解码路径；尚未确认驱动/GPU内部根因，也未证明默认路径已修复或排除所有编码兼容性问题。不自动切换用户硬件加速，不改编码/FPS/尺寸/超时/重试。
- 新增显式opt-in `TestStudioExecutionLiveTextPlanning`，默认SKIP；以`mode=ro`及query_only读取业务库的现有文本配置，小说/镜头夹具和Skill审计均在隔离测试库。真实小样用已核对的[《西游记》第十四回](https://zh.wikisource.org/wiki/西遊記/第014回)短摘录、人工分镜及明确无动画的代理角色，不冒充自动改编或真实模型资产。`COMFYUI_CONSOLE_LIVE_EXECUTION=1`下运行`go -C backend test ./internal/service -run '^TestStudioExecutionLiveTextPlanning$' -count=1 -v`，exit1：模型首次调用118.071秒成功返回5568字符，接口HTTP422，未产生通过产物。provider原始响应/错误正文未输出以避免泄露私有地址；本次未留足脱敏校验诊断，具体422原因未知，不能声称“真实模型规划通过”。未重试同参调用、未弱化校验、未改系统提示词/Skill。真实小说→导演→浏览器→存档→参考包/视频整链仍未完成；下一轮需先补安全的校验分类诊断再继续，不把文本返回成功当整链通过。独立模型日志为Monitor `work-001a215dcb0c7-307`。
- 最终回归：`go -C backend test ./... -count=1` exit0（service 44.901s，opt-in测试默认SKIP）；独立完整Vitest exit0，54文件/409项；`npm --prefix frontend run build` exit0（4517模块）。`npm --prefix frontend test`仍exit1，Node669/670，Git跟踪门禁拒绝未跟踪的`shotExecution.ts`，其Vitest阶段未执行；没有暂存或提交绕过。串行Monitor `work-001a215de5e57-308`最终exit1保留该npm失败，不以其后独立通过改称全绿。既有大chunk、动态/静态混用、React act/GLTF警告仍保留。
- 本节当时的本地EXE（已由后文VP8兼容修复版取代）：76035072字节，SHA-256 `5de9509c2bd76cb47d3674481fa1246dca5ea5dd718028f6510a7cdc460a1f5d`。先备份旧EXE/嵌入目录到`.rscode/gap-deliverable-backup-20261126-112734`，嵌入新dist并`go -C backend build -o comfyui-console.exe .` exit0；逐资源脚本CURRENT_DELIVERABLE=PASS，93项资源逐字节一致，notices source=dist=embed=exe。windows/amd64、CGO_ENABLED=0、vcs.revision=78b3f90、vcs.modified=true；非干净提交发布版、不代表许可合规。EXE/dist/诊断产物不入库；未启动/重启服务，未提交/推送/部署。
- 来源链仍仅受控provider通过；需ComfyUI的真实消费/生成/回溯本轮均 **SKIPPED（本机无法运行 ComfyUI）**。保留原未验收项，不新增替代任务凑完成。

### 迟到只读报告复核与诊断夹具修正（2026-11-26）

- 逐player复核本地Media事件：默认最终player `9A23F82AFEFD4462BEEB1E1B9EFBF246`的D3D11身份、60秒kSeek和D3DStatus/PIPELINE错误均属同一player；软件最终player的Vpx身份及seek恢复亦有记录。迟到报告“失败路径无落盘/无解码器身份”描述的是更早夹具，已被`visible-decode-49210-0.json`/`49211-0.json`补证取代。没有因该建议重跑seek，也不接受“容器/码流兼容性问题全部排除”的扩大结论。
- 仅修改`studio_execution_live_test.go`：新增测试内固定错误分类白名单，非422/未知内容只输出固定标签，不输出模型正文、错误插值或凭据；7种分类/敏感哨兵回归通过。只读设置连接限定单连接，使connection-local query_only持续生效。失败时也检查来源不变和零任务，不仅成功分支检查。
- 带新诊断的第二次真实小样：同一opt-in命令，Monitor `work-001a215ea14cf-30b` exit1。模型单次162.631秒返回5731字符；实际终止错误是`planning must not create tasks`，分类输出前已被夹具断言打断。复核`newTestDBWithNewModels`发现该导演测试库不迁移Task表，查询错误与非零任务共用该错误文案；**不能据此声称创建了任务，也不能确认本次HTTP状态或复现了第一次422**。源不变断言已执行通过；模型原始正文未输出/落盘，本次结果未验收。
- 修正仅限隔离测试库：在请求前迁移Task表并确认零任务基线，失败响应的固定分类先于其他后置断言记录。新增真实路由/受控HTTP用例，证明未知字段返回422/json_schema、源哈希不变、tasks查询有效且为0；专项exit0（2项PASS、真实模型入口默认SKIP）。未改生产校验或系统提示词/Skill，未改变模型超时/重试。两次真实小样均未完成验收，本轮停止外部模型调用，不追加第三次；修正后的真实运行尚未验收。下一步仍是取得真实422的固定分类，再按对应契约定位，不能凭猜测放宽JSON或资源校验。
- 修正夹具后的`go -C backend test ./... -count=1` exit0（service 43.020s，真实模型默认SKIP）；专项2项PASS与7种分类哨兵检查均未调用外部模型。本小节仅涉及测试与文档，生产源码/dist未变化，无需重建EXE；既有93项嵌入资源与哈希复验PASS。真实小说整链、默认解码和真实ComfyUI来源链状态不变，未提交/推送/部署。

### 视频首段解码故障修复与普通Chrome全片验收（2026-11-26）

- 用户明确反馈旧视频只能播放约1秒即错误。不做seek的`node .rscode/chrome-codec-check.mjs full old64`亦exit1，`PIPELINE_ERROR_DECODE: video decode error!`；首帧可解不能称播放正常，更不能只归因于拖动。复核进程发现先前49210含`--headless=new`：其页面截图和D3D11事件仍可用于诊断，但不能算普通可见窗口验收，先前相关措辞更正。
- 产品修复只改变共享`pickVideoCodec`顺序：优先VP8，仅VP8不可用/探测异常才回退VP9。逐帧时间、2秒关键帧、分辨率、FPS、预算、封装和采样器不变；不关闭硬件加速，不改超时或重试。Vue/React面板同步说明策略，React产物状态显示实际编码，解码错误不再建议“降低FPS”。旧VP9文件不会随升级自动修复，必须重新导出或另做兼容转码；保留旧文件及失败证据。
- 普通窗口重新验收：独立Chrome/158.0.8127.37，CDP49212，启动进程核对headless/禁GPU/禁视频解码/禁视频编码四项均false；`Browser.getWindowForTarget`为normal，`SystemInfo.getInfo`确认GPU compositing/WebGL/video_decode/video_encode均enabled，NVIDIA GTX960、驱动32.0.15.8278。环境证据`.rscode/chrome-visible-environment.json`；不是用无头截图或禁加速的软件profile冒充普通窗口。
- 用两份旧原片的全部768/2880帧，经显式软件解原片→WebGL画布→生产选择器/编码器/封装器输出VP8；编码及播放均在49212普通Chrome。**这是原画面兼容转码，不是从3D场景重新渲染，也不替代完整产品面板/小说整链验收。** 严格串行从0播放至ended后再seek，未降低FPS/尺寸、未漏帧：64秒12fps到64秒/768帧，seek60后61.839秒；120秒24fps到120秒/2880帧，seek60/110后61.7811/111.7927秒；均readyState4、seeking=false、error=null、Media错误0。默认对VP8自动选择VpxVideoDecoder、platform=false，GPU加速仍开启，这不是手动禁用硬件解码。
- Monitor `work-001a2160932b8-31e` exit0；`.rscode/chrome-visible-{encode,full}-production-old*.json/png`保留环境、逐player事件和截图。`node .rscode/chrome-visible-verify.mjs` exit0，`VISIBLE_CHROME_FULL_PLAYBACK=PASS`，逐项断言完整ended帧数/seek/编码/GPU功能与无Media错误，并核对普通窗口产物与已独立全片FFmpeg解码的字节SHA相同。ffprobe分别为VP8/1280×720/12fps/768帧/64秒与24fps/2880帧/120秒；`ffmpeg -v error -xerror ... -f null -`两片均exit0。兼容副本为`.rscode/预演64秒-兼容版VP8.webm`（16772264字节，SHA-256 `fd89be3817d374b9a6736ef16df632391b5d06c52062c2426b95b7a33a876798`）及120秒版（33335876字节，SHA-256 `ae8c96d436fdcdf17fdbf7c9ead922785a21d9866ed6ca43b20eb51b62d39041`），各附独立转码说明，不冒用原VP9来源清单。
- 对照边界：同源重新编码的VP9也有全片/seek通过，不能称“所有VP9不兼容”；观察到旧VP9为full-range、通过的新VP9为limited-range，只是线索，未证明色彩范围、驱动或GPU内部根因。VP8优先是已验证的本机兼容策略，不是所有机型/画面已验收。首轮VP8全片测试被另一并行测试切页而暂停，exit1 `complete playback must reach ended`；改为独占前台串行并增加非隐藏/非暂停断言，不延长原播放时限或自动恢复播放。诊断端口残留旧白名单导致save failed/EADDRINUSE，核对进程身份后只清理自有夹具；两次启动参数检查失败后读取实际开关，才发现headless并补普通窗口，未隐去这些失败。
- 回归：新增编码偏好/回退测试先在旧代码exit1（VP9≠VP8、探测顺序不符），产品修复后Node视频专项11/11；生产逐镜驱动4/4覆盖五尺寸的encoder/manifest/WebM均为VP8；React面板11/11、Vue模板及素材契约5/5。`node frontend/node_modules/vitest/vitest.mjs run --root frontend` exit0，54文件/409项；`go -C backend test ./... -count=1` exit0（service68.641秒，真实模型默认SKIP）；`npm --prefix frontend run build` exit0。完整`npm test`此前的未跟踪`shotExecution.ts`导入门禁仍未解决，未重跑相同确定性失败，未暂存/提交绕过，不把独立Vitest称为完整npm test通过。原生workflow仍因Chrome profile LOCK/os error33 blocked。
- 最新EXE已重新嵌入构建：`backend/comfyui-console.exe`，76035584字节，SHA-256 `a89feb7aa507a0e031c25c93a19eb7e9db468341b79fbc699a6c6355a2d42b11`；旧产物备份`.rscode/chrome-fix-deliverable-backup-20261126-120736`。构建与`node .rscode/verify-gap-deliverable.mjs`均exit0，CURRENT_DELIVERABLE=PASS/93资源逐字节一致/notices四处一致；windows/amd64、CGO_ENABLED=0、vcs.revision=78b3f90、vcs.modified=true，非干净提交发布版。不启动/重启业务服务，不部署、不提交/推送；源码/测试/文档未提交，EXE/dist/诊断与视频不入库。真实ComfyUI消费/生成/回溯仍 **SKIPPED（本机无法运行 ComfyUI）**；小说整链、跨设备、性能和许可等原未验收项不转为完成。

### 本批用户授权提交与推送复核（2026-11-26）

- 用户本轮明确要求“提交并推送”，授权范围为本批三缺口续作及VP8兼容修复的源码、测试和文档；此前“未提交/不提交”均是当时状态，以包含本节的实际提交及远端同步核对为准，不追认部署授权。精确暂存24个源码/测试文件（含9个新增文件），再包含3份相关文档；排除`.rscode`、dist、EXE、视频、node_modules、业务数据及无关诊断空壳，不修改系统提示词/Skill。
- 两份只读静态复核未发现明确阻塞性问题；复核提醒必须包含新`shotExecution.ts`及测试，本轮已显式登记。暂存范围/常见凭据模式检查PASS，不输出匹配正文；这不是穷尽式安全或功能证明。
- 正式暂存相关新模块后运行项目原始完整命令`npm --prefix frontend test` exit0：Node671/671（Git导入完整性门禁通过），其后Vitest54文件/409项均通过。此前Node669/670失败被实际完整通过取代，不是修改测试、跳过门禁或以独立Vitest冒充。`go -C backend test ./... -count=1` exit0（service91.666秒），真实文本opt-in显式关闭，不调用外部模型或ComfyUI。
- 最新生产源码与上一节成功构建时一致；`git diff --exit-code -- backend frontend` exit0，既有前端build的Monitor最终exit0，93资源的EXE逐字节复验CURRENT_DELIVERABLE=PASS（SHA仍为上一节未提交构建，不冒充本次新提交的干净构建）。`git diff --cached --check`通过。原生ship workflow `flow-1795671796375-11928-3`仍因历史Chrome profile的LOCK/os error33 blocked；本次不声称原生workflow通过，不为提交而关闭用户浏览器或更改忽略规则。
- 真实ComfyUI消费/生成/回溯仍 **SKIPPED（本机无法运行 ComfyUI）**；普通Chrome兼容转码全片通过不替代产品同档重渲/内嵌、小说整链、跨设备、性能及许可验收。提交与推送不包含EXE，不启动/重启服务，不部署。
