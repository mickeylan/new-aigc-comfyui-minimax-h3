# ADR-0022：角色、导演、避让与现代地表统一增强

状态：本轮六类增强已接入真实入口、统一采样与存档，本机自动化及三对象浏览器闭环通过。跨设备、大场景性能及既有上限视频播放仍待独立验收；ComfyUI 实机测试 SKIPPED（本机无法运行）。

## 决策与兼容

1. 统一目标：IK/脚底贴合、动作混合、跨骨骼重定向、人工可引导自动机位/表演、动态避让、现代surfacePaint及完整程序化地表材质。不得分散成互不消费的模块；必须有真实入口、存档往返、错误提示和自动化验证。
2. 旧档不启用新语义；走位默认静态障碍拒绝保持。新行为以可选版本化配置明确启用，不静默猜角色、骨骼映射或动作语义。
3. 自动导演是本地确定性建议：人工选角色、动作、构图与方向，先生成可审阅草稿，确认后才写镜头表；不调用模型、不提交生成、不冒充剧情理解或物理仿真。
4. 播放、参考包与视频使用同一绝对时间采样；随机跳转、不同FPS与重复采样相同，停止恢复骨姿/对象/相机。避让使用受界路径搜索与时空碰撞检查，无法证明安全则拒绝，不能穿透或偷偷忽略碰撞。
5. IK采用显式双腿链配置及两段解析求解；不可达、错误层级、非均匀变换等须说明支持边界并拒绝，不能宣称支持任意骨骼。重定向由人确认骨骼映射与参考姿态，以烘焙普通动作资源保存，旧资源格式继续适用。
6. 上游地表完整库实际为59个稳定ID/程序化材质记录，不是59张外部贴图。现代surfacePaint每像素两个样式叠加于基础地形；第三种材质按上游规则保留占优层，不宣称无限层。gzip解码限额、CRC/ID校验、撤销深复制、持久化flush与GPU释放均是交付门槛。
7. 沿当前Three/React/Vite实现，不新引入重量框架。上游Toonflow源库只读，保留MIT许可。每次改文件立即执行相应验证后再改下一文件；不改弱测试。
8. 不提交/推送/部署、不接触业务数据或18000、不改超时重试、不启动连接ComfyUI。相关测试SKIPPED；不重复弱显卡1577实例/2880帧压力，真实小场景与CPU验证可跑。高性能电脑性能/兼容性仍按独立清单验收。

## 验证记录

记录日期：2026-11-25。以下信号来自主代理实际执行；子代理超时不作为完成证据。

| 验证 | 实际命令/信号 | 结果 |
|---|---|---|
| 前端全量 | `npm --prefix frontend test`；Node **662/662**，Vitest **41 文件、326/326** | PASS，exit 0 |
| 生产构建 | `npm --prefix frontend run build` | PASS，exit 0；仅用于隔离预览，未部署 |
| 后端全量 | `go -C backend test ./... -count=1`；service 包 46.535s | PASS，exit 0；没有启动/连接 ComfyUI |
| 骨骼增强 | `characterMotion.spec.ts` **10/10**：绝对时间混合、零权重、非骨骼根轨道拒绝、双脚高度/法线、直腿站姿、异常原子还原、校准重定向及烘焙预算 | PASS |
| 导演与避让 | `shotDirection.spec.ts` **6/6**、`shotNavigation.spec.ts` **10/10**；偏心模型半径、动态时序占用、细墙绕行、实际足迹不能缩小、扫掠区域漏检水格拒绝 | PASS |
| 人工入口 | `ShotEnhancementPanel.spec.tsx` **3/3**、`CharacterMotionPanel.spec.tsx` **1/1**；草稿必须确认、不自动挂载、禁用及错误反馈 | PASS |
| 现代地表 | `environmentModernTerrain.spec.ts` **5/5**：59 个 ID/调色板/GLSL 接入，gzip 长度/CRC/ID/预算校验，深复制、flush、水面兼容、纹理替换及重复 dispose | PASS |
| 真实 Stage 回调 | `studioRecipeRoundtrip.spec.ts` **21/21**（全量包含）；`STUDIO_ENHANCEMENT_CLOSED_LOOP=PASS` | 导演草稿→真实绕行→混合角度 **0.3rad**→双脚 **0.09m**→停止还原→重定向普通 clip + surfacePaint 的 **2 次 GLB/JSON 往返**均通过；另实测雨量/天空/太阳方向同步 |
| Chrome 三对象闭环 | `.rscode/verify-enhancement-browser.mjs`；`BROWSER_ENHANCEMENT_CLOSED_LOOP=PASS` | 实际 UI 校准/烘焙、人工确认导演、走位、混合、IK、两笔绘图、保存、刷新恢复；控制台错误 **0** |
| 真实绘图画面 | `.rscode/verify-modern-paint-view.mjs`；`MODERN_PAINT_REAL_MATERIAL=PASS` | 从真实场景取得现代材质并把相机对准绘图区；主代理查看截图确认黄绿色着色及渐变覆盖，而非只看存档字段。不是59种外观逐项或跨GPU验收 |
| ComfyUI 实机 | 模型加载、目标节点消费参考视频、真实输出及回溯 | **SKIPPED：本机无法运行 ComfyUI**；未索要实例、未执行此前真实生成授权 |

浏览器可复核证据（临时夹具，不入库）：`.rscode/enhancement-browser-result.json`、`.rscode/enhancement-browser-saved.json`、`.rscode/enhancement-browser.png`、`.rscode/modern-paint-view.png`。三对象存档在隔离内存 fixture 的编号为 **1000022**；绕行中段为 `[-1.456015308147824, 0, 1.398822751757935]`，双脚世界高度均为 `0.09000000000000002`，动作资源数 **3**，paint 样式数 **2**。这不是业务数据或生产部署证据。

### 发现并解决的验证问题

- 统一采样器在启动时登记所有未来角色的临时基线，会遮住尚未走位对象的真实编辑。改为首次实际采样该角色时才登记；保留原 `not.toBe` 脏标记断言，播放期间真编辑仍可识别，停止精确还原。
- 实际足迹、偏心原点、旋转后的时序占用须同时考虑；半径取人工配置与角色真实包围圆较大者。地形检查覆盖扫掠圆盘触及单元的两片三角形梯度与全部水域顶点，具有一百万单元预算。
- 次要 clip 不能绕过存档校验播放根/非骨骼轨道；运行时捕获并混合完整变换快照，非法轨道在任何变换前拒绝。零权重只保留主动作。
- 新文件最初未登记进 Git，项目的 tracked-import 测试报错（全量 exit 1）；仅对明确目标文件做 `git add -N` 后原命令全量通过。没有提交或推送，没有改弱测试。
- 新真实蒙皮集成夹具不能使用旧的普通 `.clone(true)` 导出桩；改用生产 `exportGeometry`（SkeletonUtils、稳定轨道路径）及真实 `applyBonePose` 依赖。没有放宽往返断言。
- Chrome 首次验收发现 GLTF 把第二角色的 `chest` 重命名成 `chest_1`；错误映射被拒绝。重定向面板补充展示双方实际骨名，验收按真实名称显式映射。
- 首轮现代绘图阶段发生一次 **30 秒 CDP Runtime.evaluate 超时**；截图无弹窗/报错，随后只读检查可响应、首笔已生效。未武断归因或改超时；重建后按相同超时再次完整闭环通过。**冷启动编译延迟没有建立性能保证**。
- 浏览器截图暴露增强表单白底白字及拥挤；两个面板统一深色控件、逐行布局和可读 JSON 区。重建后截图复核通过。

## 可操作入口与支持边界

1. **自动机位/角色表演**：3D 场景已有镜头表后，在右侧「镜头增强 / 导演草稿」逐个明确选择说话人对应角色及已有 clip，设景别、方位、距离、行程与避障，生成草稿→检查→勾人工确认→应用→连续逐镜播放。它是本地确定性建议，不理解剧情语义、不调用模型；每镜只支持一个明确映射主体，保留已有机位/走位，无法处理时直接说明原因。
2. **动作混合与脚底 IK**：选择「本镜」，按显示的实际骨骼名称填写「角色增强 JSON」再应用；空白明确移除增强。`blend` 包含第二 clip 的 `index/name/duration`、`weight`、`fadeSeconds`；`feet` 包含左右 `hip/knee/ankle`、`soleOffset`、`maxCorrection`。直接链、单位缩放骨架；装饰网格自身缩放不影响骨链校验。两 clip 独立循环，权重渐入按绝对时间计算。IK 为两段几何投影及地表法线对齐，**不是接触锁脚、物理仿真、骨盆求解或自动步幅匹配**；不可达或修正超限显式拒绝，不拉伸骨骼。
3. **跨骨骼重定向**：「Character motion / Rest-calibrated retargeting」选两棵不同骨架与源 clip，按双方显示的真实名称填写 JSON 映射。双方须处于人工校准参考姿态；先 Validate preview（仅校验烘焙，不挂载），再 Bake and attach；之后使用普通动作预览/镜头表，随既有资源存档。不是自动识别任意 FBX/Mixamo，也不推断映射；轨道全部须映射，单位缩放，30fps 烘焙，最多18001帧/200万采样值。
4. **动态障碍避让**：每镜 `navigation:{version:1,mode:'avoid',radius,speed}` 显式启用；有限可见性图搜索，顺序镜头按时序预留其他角色首镜前原位与完成后终点，释放已腾出的空间。**不是并行人群仿真、帧间实时重规划或物理碰撞引擎**。最多128个登记对象、512个搜索节点及上述预算；障碍在会话启动时快照，锁定物体仍阻挡，隐藏/splat排除。不可达、速度不足、占用冲突、带变换祖先或超过预算直接拒绝，不穿透、不偷偷忽略。
5. **现代 surfacePaint**：「环境→地形笔刷→现代材质绘图」，从完整59种目录选择材质/颜色，调中心、半径、强度、流量、硬度，显式应用；支持撤销及保存刷新。每像素最多两种 paint 样式叠加于基础地形，第三种依上游占优层规则替换；ID38原始地面不能绘制。不是外部图片贴图包或无限图层。
6. **统一输出与兼容**：播放、逐镜参考包、视频调用同一 `createScriptShotSampler`；版本化增强字段随镜头表与资源保存。旧档缺字段不启用增强；`samplerVersion` 保持 `script-shot-timeline-v1`，增强配置和导航证明随清单保留，后端原白名单仍接受。未改变上传限额、生成默认行为或重试/超时。

## 原任务续作：统一画幅与旧导出修订保护

- 核对原 `kitbash-frame` 的未验收描述，发现同场景材质/对象/画幅编辑可在异步PNG编码期间混入旧输出，序列结束还可能覆盖用户新机位。单图、首尾帧与相机路径序列现同时核对来源全指纹与position/target/FOV；修订变化拒绝整份输出，不回滚用户新机位，帧数不完整显式失败。生产回调新增 `LEGACY_EXPORT_REVISION=PASS operations=3 edits=3 staleOutputs=0 newerCamera=preserved`，零陈旧下载/上传。
- 逐镜视频此前固定16:9，与共用场景画幅不符；现在 `FRAME_VIDEO_SIZES` 同步驱动渲染、编码、WebM轨道、清单及Go入库白名单：16:9=1280×720、9:16=720×1280、1:1=960×960、3:2=1152×768、2:3=768×1152。均不超过旧1280×720像素/最长边预算；帧数2880、256MiB上限未变。画幅变化取消在途导出并清旧确认/产物，迟到回执保持会话隔离。
- 自动化：`sceneFrame.spec.ts`4/4、`scriptShotVideo.spec.ts`4/4、`ScriptVideoPanel.spec.tsx`11/11；`node --test frontend/tests/previs-video.test.mjs`10/10（五尺寸轨道逐字节核对、非法尺寸在编码器探测前拒绝）；Go `MATERIAL_FRAME_SIZES=PASS accepted=5 rejected=4 manifest=retained`；许可门禁5/5。前端全量现为662 Node + 326 Vitest，Go全量exit0。
- 隔离真实Chrome `.rscode/verify-kitbash-completion.mjs` exit0：`KITBASH_COMPLETION=PASS aspects=5 sequenceCombinations=60 videos=20 races=9 consoleErrors=[]`。20张截图/单pass PNG按实际画布尺寸断言，60组序列ZIP（5画幅×4宽度×3pass，每组3帧）逐PNG像素尺寸断言；20段短视频（5画幅×4mode）取得正确尺寸、1秒/1帧、readyState≥2/error=null，并人工入口存入隔离真实Go素材处理器、确认来源清单。**不是20段运动播放验收**，不覆盖既有2880帧Chrome故障。
- 五段viewport实际WebM分别经ffprobe核对尺寸/1帧/1秒及FFmpeg `-v error -xerror ... -f null -` 严格全片解码，5/5 exit0（`FRAME_FFMPEG=PASS`）。这是短产物独立解码，不等于长片Chrome播放。
- 9组浏览器PNG编码挂起后修改材质/对象/画幅与机位，分别覆盖单图/首尾帧/序列，均零陈旧下载且保留新机位。主代理看最终截图确认错误提示可读；最终画幅为竞态测试改后的9:16，**该截图不证明2:3画幅**，后者由实际输出像素断言证明。
- 验证纠错如实保留：首轮命令exit1，5184预览已退出（ERR_CONNECTION_REFUSED），仅恢复隔离预览；第二轮exit1，导航瞬间body为null，补验收脚本初始DOM等待；产品代码未因此变更。此后相同30秒CDP上限完成矩阵，不改超时、不施压1577实例/2880帧。
- 临时证据 `.rscode/kitbash-completion-result.json` / `.rscode/kitbash-completion.png` / `.rscode/frame-*.webm` 不入库，不依赖业务数据库，不接触18000。

### 原10项任务回写口径

不新建替代任务：来源决策、素材/64套/实例持久化、剧本走位、环境接入、环境浏览器、画幅、逐镜视频和参考/生成交接的本地闭环按原任务回写完成；`toonflow-qa`与`remaining-acceptance`仍保留进行中，等待下述硬件/跨设备验收。大场景「实例化」仅表示可编辑实例与几何复用，不宣称对象级GPU instancing；生成交接已完成离线链，ComfyUI实机明确SKIPPED。绑定前审阅关系表不单独入档，已应用对象引用/身份/站位与镜头台词会保存。

## 仍独立待验收的项目

- [高性能电脑清单](../2026-11-25-Toonflow迁移-高性能电脑待验收清单.md) T01–T04：既有2880帧Chrome内嵌解码失败（根因未知）、1577实例交互、跨设备稳定性、峰值GPU显存。没有重复低配压力，也不能用本轮小场景PASS覆盖它们。
- 现代完整材质首次编译耗时、59种外观逐项/不同GPU表现、长片骨骼/避让性能仍需人工检查；不把单元测试或一幅着色截图当性能验收。
- T05 ComfyUI 实机继续 SKIPPED。
- 上述验收时未提交、推送或部署；用户随后明确授权本批提交并推送，版本按包含本文的实际提交及远端同步结果核对，不执行部署。新机同步步骤见高性能电脑清单§2.1。

### 提交前复验（2026-11-25）

- `npm --prefix frontend test`首次exit1：Node662/662，Vitest325/326，`studioRecipeRoundtrip.spec.ts:302` 的逐镜骨骼回调用例报 `TestTimeoutError: Test timed out in 5000ms`。当时前端构建及Go测试同时在跑；不能仅凭此断定根因。
- 构建/Go结束后，未修改源码、测试、断言或超时，单独执行相同前端全量命令一次，exit0：Node662/662、41文件326/326。首次超时保留，不以重跑抹去，也不宣称已建立负载性能保证。
- `go -C backend test ./... -count=1` exit0（service67.480s）；`npm --prefix frontend run build` exit0（4509 modules，2m58s）；声明源与dist逐字节一致11677字节，`git diff --cached --check` exit0。仅提交目标源码/测试/文档，不含业务数据、临时产物和`.rscode`夹具。

## 推送后复核：四类遗漏修复（2026-11-25）

本节为用户随后授权的修复，基于已推送 `ae04d3c`。修复验收时未提交、推送或部署；用户随后明确授权提交并推送这批修复，以包含本节的实际提交和远端同步结果核对，不执行部署。T01–T05及GPU/长片边界不因这些修复改变。

- **跨镜坡地高度**：复现同角色连续上坡两镜，切镜前后Y从1.0195降到0.02（约0.9995m）。统一采样器现在只在该角色首个走位起点确定地表偏移，后续镜次共用，不再逐镜归零；保持首镜旧行为、绝对时间随机访问和停止还原。`scriptShotSampler.spec.ts`新增普通/避让路径两种跨镜、乱序seek、终点及恢复断言，修复前失败（预期1.02实际0.02），修复后5/5。
- **旧档灯光**：环境为空的存档载入显式把`doc.lightPreset`传给环境回退，避免同一载入回调中旧闭包预设覆盖新灯光。真实保存/载入及灯光生产回调测试，修复前最终studio而非night；修复后night灯强/背景/状态一致，非空环境逻辑不变。
- **视频尺寸所有权**：逐镜导出期间ResizeObserver延迟调整；采样及异步捕获后校验真实画布尺寸、相机比例、截取尺寸与源画幅。异常拒绝整份输出；成功/取消/失败统一释放所有权，恢复当前容器尺寸与DPR，不恢复过期视口尺寸。生产回调新增五画幅×成功/取消/错误尺寸15组断言；修复前窗口缩放覆盖固定输出，修复后全部通过。不提高像素、体积或帧数上限。
- **参考视频范围/项目ID**：下拉取消前100条截断，沿现有不分页接口获取，再仅保留当前项目与共享视频；迟到成功/失败均校验项目与请求代次。上传/列表显式提供非法、空、零或溢出project_id均400，上传在文件读取与持久化前拒绝；缺省共享语义不变。这是参数格式校验，不新增项目存在性/鉴权机制。`scene3d-ref-video-options.test.mjs`4/4，含125条项目视频+共享/跨项目过滤和切项目迟到响应；`material_project_validation_test.go`用nil服务证明非法请求不能到达媒体/数据库处理。主代理独立复验均exit0。
- **当前全量**：串行执行`npm --prefix frontend test` exit0，Node663/663、Vitest41文件333/333（含真实Stage回调27/27）；`go -C backend test ./... -count=1` exit0（service43.289s）；`npm --prefix frontend run build` exit0（4509 modules，1m5s，保留既有大chunk/动态静态混用警告）。未调整断言或超时。源与dist许可声明逐字节一致11677字节，`git diff --check` exit0。
- **新构建真实Chrome**：`.rscode/verify-four-fix-browser.mjs` exit0，`FOUR_FIX_BROWSER=PASS oldArchiveLight=night aspects=5 encodedFrames=10 consoleErrors=[]`。载入无环境旧档后背景080b14、三盏方向光强度0.7/0.3/1.2；五画幅各导出2秒/1fps视频，在首帧真实PNG编码挂起时通过CDP改窗口并等待ResizeObserver，导出画布保持目标尺寸，实际10个VideoFrame、WebM播放器尺寸及清单全部一致，完成后按新容器恢复。视频readyState≥2/error=null；这是小样本尺寸/恢复回归，不代替2880帧播放、长片性能或跨设备验收。
- 临时浏览器脚本及`.rscode/four-fix-browser-result.json`不入库；未访问业务数据库或18000、未入库素材或创建生成任务。ComfyUI实机继续**SKIPPED：本机无法运行**。
- **验证纠错**：首个`npm --prefix frontend exec -- vitest ...`被本机npm解析掉参数，误用根目录配置收集Node套件（exit1，`No test suite found`/缺DOM）；检查项目`vitest.config.js`后使用`node frontend/node_modules/vitest/vitest.mjs run --root frontend ...`。新增回调夹具首轮缺天空盒ref、把TS类型import替换成运行时表达式；补齐依赖且只替换运行时导入后，原断言准确复现两项产品缺陷，再修改产品代码通过。没有调整测试超时或削弱断言。

## 镜头契约与布景引擎依赖边界（2026-11-25）

用户选择最小边界修复：抽出独立镜头契约模块，包含镜头类型、校验、时间轴纯数据计算与参考包清单；不改变原函数行为、错误文案、存档格式或版本。布景适配器可重导出原接口兼容既有调用；镜头契约不得运行时反向依赖布景模板、素材目录、模型解析及几何构建。增强校验允许复用已有角色校验，但须检查传递运行时依赖。

- 静态镜头消费者（导演、导航、增强面板、逐镜采样器）直接依赖独立契约，不经布景适配器重导出绕回重模块。
- 只修上述依赖边界；不改变布景/程序化素材面板挂载即加载、存档配方物化或环境渲染行为，不承诺所有大目录直到用户展开才加载，也不采用强制manualChunks隐藏循环依赖。
- 先采集当前构建的chunk静态闭包、模块归属和原始/gzip字节，改后同方式比较；纯镜头契约静态闭包不得含布景引擎/目录构建器。包体可能仅重新分配，不以单个chunk缩小冒充总下载减少或首屏性能提升。
- 逐文件验证；原接口重导出兼容、镜头全量回归及构建通过后才算此小项代码完成。新增源码按既有导入门禁仅逐路径登记intent-to-add，不暂存内容、不提交推送。

### 最小边界修复执行记录

- 将镜头段整体移入 `scriptShotPlan.ts`，原 `toonflowSets.ts` 保留兼容重导出。与HEAD比较原函数正文33065字节逐字一致（`SHOT_CONTRACT_BEHAVIOR_COPY=PASS`）；仅新增模块头和改变四个静态消费者的import路径。类型引用会被擦除，不作为运行时反向依赖。
- 新增esbuild真实打包闭包回归5项，修复前exit1：四个已有消费者各引入5个禁止重模块，新契约文件尚不存在；不是五项都复现了同一种产品缺陷。修复后5/5通过。逐文件专项也均exit0，未放宽断言。
- `npm --prefix frontend test` exit0：Node668/668，Vitest41文件343/343；`npm --prefix frontend run build` exit0：4510模块、25.48秒。存档、参考包、视频、增强与真实Stage回读沿用既有全量测试。
- 用同一临时Vite插件于generateBundle阶段捕获新旧代码与静态模块图（两次均exit0）：四个镜头消费者各自的禁止重模块数5→0，新契约为0；场景入口静态chunk闭包内禁止重模块数5→0，信号 `SHOT_BOUNDARY_VITE_GRAPH=PASS`。未改manualChunks或构建配置。
- 同一采样阶段的场景入口code字节15125435→3124305、gzip5433465→1082518；入口及其静态chunk闭包合计字节16252942→4251812、逐chunk gzip之和5754144→1403195。这是JS静态闭包，不含动态请求、CSS、WASM或浏览器缓存，亦非实际网络传输/首屏耗时。Vite最终写盘报告略有渲染差异（改后入口3125.39kB/gzip1082.91kB）。
- 大目录转到动态chunk而非被删除：`toonflowPrimitives` code10689800字节/gzip4001202字节，`toonflowSets` code116380/gzip37408；仍有大chunk警告。面板挂载仍可能立即请求动态目录，因此不能宣称进入3D页不下载目录，或用户可感知性能已通过。未做Chrome网络/像素验收或ComfyUI实机测试。
- 首次临时基线脚本从仓库根运行，Tailwind提示content缺失且PowerShell把stderr记为exit1；没有用此轮作为最终基线。修正脚本工作目录为frontend并用cmd捕获真实退出码后重新测量，未更改项目配置。临时日志/模块图在 `.rscode/shot-boundary-*`，不提交。原生workflow仍停在ADR user_decision，用户已在问答选择补ADR后最小实施，但工具没有接收该决定的入口，不宣称workflow完成。

## 基础走位编辑与增强配置的保留契约（2026-11-25）

用户已选择先补充本 ADR 再实施。本节规定基础走位表单的行为，不改变完整 motion 替换接口、存档字段或版本。

- 基础走位编辑不等于清除增强。同一稳定 targetId、同一主动作引用（index/name/duration 全部相等，或都未选）时，重复应用及修改起止坐标/时长须保留已有 `characterMotion` 与 `navigation`。
- 存在 `characterMotion` 时，主动作改变采取保守拒绝；存在任一增强时，目标改变拒绝。不按显示名、同名 clip 或索引单独推断兼容。不兼容操作不得写入计划，须明确提示先在增强面板移除对应配置，或显式清除走位后重新绑定。只有导航且目标不变时，切换主动作不影响导航配置。
- 保留配置不是保留安全证明。坐标/时长变化后的播放与导出仍走既有统一采样器重新检查，不复用旧路径或可走证明。
- 「清除走位」仍显式移除整份 motion；增强面板仍负责显式移除增强。无增强旧档保持原编辑行为，不自动启用新配置。
- 验收须覆盖真实 React 输入/点击的重复应用、坐标/时长修改、动作引用变化及目标切换原子拒绝、显式清除，以及完整 motion 经既有校验/JSON 回读后增强字段不丢失。实现与执行结果另按实际信号报告，不以本节契约宣称测试通过。

### 本项执行记录

- 先新增10项真实React事件回归，专项命令 `node frontend/node_modules/vitest/vitest.mjs run --root frontend src/studio/scene3d/SetTemplatePanel.spec.tsx` 修复前exit1：10失败、原17通过；传给完整替换接口的值明确缺少增强字段，不兼容变更也未被拒绝。只改基础表单保留与拒绝逻辑后，相同27项exit0，没有修改既有断言。
- 新增保留用例进一步调用真实 `setScriptShotMotion`、目标校验与JSON回读校验；与分场、逐镜采样、真实Stage存档专项共4文件93项通过，含既有增强资源两轮GLB/JSON往返。未改变完整motion替换接口及存档版本。
- 首次前端全量exit1：新增用例内部冷加载完整素材模块触发 `Test timed out in 5000ms`，并产生后续React act/DOM级联错误（19项失败）。将该真实模块的加载移至测试文件收集阶段、React root创建前；未改超时、断言或产品逻辑。此后 `npm --prefix frontend test` exit0：Node663/663，Vitest41文件343/343。
- `npm --prefix frontend run build` exit0，4509模块、27.04秒，仍有既有大chunk及动态/静态混用警告；不将构建成功当作懒加载缺口已解决。
- 初轮没有新增真实Chrome验收；后续组件级Chrome补验见下。React测试与生产回调测试不冒充浏览器像素验收。不运行ComfyUI、不触碰业务数据、不部署、不提交推送，不修改用户H3模板。临时日志在 `.rscode/toonflow-motion-fix-*.log`，不提交。

### 真实Chrome组件级补验（2026-11-25）

- 检查时5173/18000均ECONNREFUSED，工具自带Chrome启动失败；改用独立隔离profile的HeadlessChrome/157.0.0.0，不接触用户浏览器。临时esbuild bundle挂载真实SetTemplatePanel与真实布景模块/契约，没有mock引擎；在127.0.0.1:18081夹具页操作原生输入/选择/按钮，并经真实校验和JSON回读捕获应用计划。
- `node .rscode/motion-upload-chrome.mjs` 最终exit0，`CHROME_COMPONENT_AND_UPLOAD_FIXTURE=PASS`：9组走位场景通过——双增强/仅导航/仅角色增强重复应用保留、坐标时长修改保留、换目标/换主动作/清空动作拒绝、仅导航时切主动作保留、显式清除。拒绝场景应用回调计数为0；浏览器未捕获error事件。
- 是真实Chrome组件/DOM事件验收，不是完整Scene3DStage/WebGL像素、真实存档上传或业务端到端验收；未证明视觉效果、避障安全或目录请求时机。与上传共用脚本首轮在后续上传夹具的非法project_id前置校验处失败，走位已执行但未当整轮PASS；修正上传输入准备且保留原断言后整轮通过。
- 临时脚本、bundle、结果与profile在 `.rscode/`，Go无数据库哨兵源在 `backend/.rscode-upload-probe/`，均不提交。临时哨兵已停止并断言端口拒绝连接，未启动真实前后端或ComfyUI。qa-only流程仍因缺fresh project-gate evidence而blocked，不宣称workflow完成。

### 原片seek解码对照补记（2026-11-26）

- Chrome/158.0.8127.37默认路径：旧64秒/768帧、120秒/2880帧原片seek到60秒均`PIPELINE_ERROR_DECODE`，exit1；复制码流的FFmpeg remux对照同样失败。原64秒片的32个cue位置有效、关键帧标记无失配，未发现支持“仅修封装即可恢复”的证据。
- 相同原片在独立profile加`--disable-accelerated-video-decode`后均seek恢复至61.79秒，`seeking=false, readyState=4, error=null`，exit0。范围为本机Chrome平台/硬件解码路径相关，未定位驱动/GPU内部根因；不修改编码、分辨率、FPS、超时或重试，不自动改变用户浏览器设置。
- 不追认默认路径或原片全片通过；新机RTX4070简化新片全片/seek通过是独立证据。详见高性能电脑待验收清单T01最新小节；真实ComfyUI本轮仍SKIPPED。
- 历史夹具19049/Media域补证：默认49210实际`D3D11VideoDecoder`、platform=true、D3D11 backend，首帧可解但60秒seek后`D3DStatus::6`→`DecoderStatus::1`，最终`PIPELINE_ERROR_DECODE`，exit1；软件49211实际`VpxVideoDecoder`、platform=false，同片恢复至61.6437秒、readyState4/error=null，exit0。截图和事件在`.rscode/visible-decode-49210-0.{png,json}`及49211对应文件。后查49210进程带`--headless=new`：页面有内容及截图不等于普通可见窗口验收，先前“可见”措辞更正；用户指出旧片起播约1秒即错误，后续不seek的完整播放测试也失败，首帧可解不能称起播播放通过。定位到本机D3D11硬件VP9路径相关，尚未定位驱动/GPU内部根因，不宣称排除所有编码兼容性问题。新增VP8优先导出修复、49212普通窗口（无headless/禁加速开关）验证及最新EXE另见ADR-0023末节；旧片失败证据不转为通过。
