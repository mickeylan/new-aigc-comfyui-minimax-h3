# 第三方来源与许可声明（Studio 岛）

`frontend/src/studio/` 下的代码有两类来源：移植自带许可的第三方项目，以及通过 npm 依赖引入的运行时库。本文件是这两类来源的清单，供分发时随包附上。

## 一、移植来源

| 目录 | 来源 | 许可 |
|---|---|---|
| `video-editor/` | [opencut-classic](https://github.com/opencut-app/opencut-classic)（原 opencut-app/opencut 的上一代实现）：时间线、属性面板、素材面板、字体选择器、对话框与编辑器外壳 | MIT |
| `pagedraw/`、`pagedraw-host/` | ArtCraft（本项目作者自有仓库）：2D 画布外壳、图层与笔刷交互 | 双许可 MIT / Apache-2.0 |
| `scene3d/`（下列单列目录除外） | ArtCraft（本项目作者自有仓库）：3D 场景外壳、变换 gizmo 与骨级摆姿（FK） | 双许可 MIT / Apache-2.0 |
| `scene3d/toonflow/`（分阶段迁入）及注明 Toonflow 来源的适配模块 | [Toonflow-app](https://github.com/HBAI-Ltd/Toonflow-app)，`packages/nodes/trumanMap/src/`：程序化素材、布景配方与预演逻辑 | MIT，Copyright (c) 2026 HBAI-Ltd |

### Toonflow 迁移许可边界

上述 Toonflow 内容采用以下 MIT 全文；来源核对见 ADR-0020。目录登记不代表全部功能已迁入。
源插件另列 Holodeck 派生内容（MIT，Copyright 2026 Matthew Kissinger）、examples（CC0）、Quaternius 动作（CC0）、fzstd（MIT）及 SunCalc（BSD-2-Clause）。
这些内容不能仅凭根 MIT 重新授权；实际迁入时须保留各自完整许可及来源。Kiln 建模引擎不随本次迁移打包，亦不自动下载。

```text
MIT License

Copyright (c) 2026 HBAI-Ltd

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

已迁入的 `characterAnimationData.json` / `characterAnimations.ts` 包含重定向后的 Quaternius Universal Animation Library 动作数据：
- 来源：<https://quaternius.com/packs/universalanimationlibrary.html> 与 <https://quaternius.com/packs/universalanimationlibrary2.html>。
- 许可：CC0 1.0 Universal，<https://creativecommons.org/publicdomain/zero/1.0/>；不把动作数据重新授权为本仓库 MIT。未迁入 Holodeck examples 或 fzstd 解压器。

布景本地工具中可能包含上游所述 Holodeck 派生逻辑，保留下列许可（不提供联网 AI 主机）：

```text
MIT License

Copyright (c) 2026 Matthew Kissinger

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

### SunCalc 2.1.1（BSD-2-Clause）

来源：<https://github.com/mourner/suncalc/tree/v2.1.1>，以下与 npm 包 LICENSE 一致：

```text
Copyright (c) 2026, Volodymyr Agafonkin
All rights reserved.

Redistribution and use in source and binary forms, with or without modification, are
permitted provided that the following conditions are met:

   1. Redistributions of source code must retain the above copyright notice, this list of
      conditions and the following disclaimer.

   2. Redistributions in binary form must reproduce the above copyright notice, this list
      of conditions and the following disclaimer in the documentation and/or other materials
      provided with the distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND ANY
EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF
MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE
COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL,
EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR
TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

### Zod 4.3.6（MIT）

```text
MIT License

Copyright (c) 2025 Colin McDonnell

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

opencut-classic 的 MIT 许可要求保留版权声明与许可全文。本项目根目录的 `LICENSE` 只覆盖本仓库自身代码，不替代上游声明，因此移植部分按下列方式声明：

```
Portions of frontend/src/studio/video-editor are derived from opencut-classic
(https://github.com/opencut-app/opencut-classic), licensed under the MIT License.
Copyright (c) opencut-app contributors.
```

`scene3d/animationPreview.ts` 的 GLTF CUBICSPLINE 插值逻辑参考 Three.js `examples/jsm/loaders/GLTFLoader.js`，为工程存档恢复用途改写。该部分保留以下上游 MIT 许可：

```text
The MIT License

Copyright © 2010-2025 three.js authors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
```

## 二、随 studio chunk 分发的运行时依赖

| 包 | 版本 | 许可 | 说明 |
|---|---|---|---|
| `react` / `react-dom` | 见 `frontend/package.json` | MIT | 岛内渲染 |
| `three` | 0.180.0 | MIT | 3D 场景、GLB 导入导出 |
| `zod` | 4.3.6 | MIT | Toonflow 本地配方及工具参数校验，不接入上游 AI 主机 |
| `suncalc` | 2.1.1 | BSD-2-Clause | Toonflow 时间草稿的日月位置算法 |
| `mediabunny` | 1.29.1 | MPL-2.0 | 媒体解封装/编码。**未修改源文件**，以依赖形式使用；对应源码见 npm 包内 `src/` 或 <https://github.com/Vanilagy/mediabunny> |
| `opencut-wasm` | 0.2.10 | MIT | 时间基重采样 |
| `lucide-react` | 0.512.0 | ISC | 图标（画布与编辑器工具栏） |
| `@hugeicons/core-free-icons` | 1.2.1 | MIT | 图标集，全部图标取自该包 |
| `@hugeicons/react` | 1.0.4 | Hugeicons Pro 许可条款 | 只用到 `HugeiconsIcon` 渲染组件；**未引入任何 Pro 图标文件** |
| `radix-ui` / `@radix-ui/react-*` | 见 `frontend/package.json` | MIT | 弹层、下拉、对话框等无样式原语 |

MPL-2.0 只对 `mediabunny` 自身的文件生效，要求保留其许可并可获取其源码；本项目对它的使用方式（原样 import，不改源文件）满足该要求。

## 三、不随代码分发的内容

- **Google Fonts**：字体选择器在运行时从 Google Fonts 加载字体，字体文件不打包进本仓库产物。
- **ComfyUI 产物**：任务生成的图片/视频/音频属于用户数据，不含第三方代码。

## 四、维护要求

新增移植目录或新增会打进 studio chunk 的依赖时，必须在本文件补一行。`frontend/tests/studio-notices.test.mjs` 会检查本文件仍在、关键条目未被删，以及三个移植入口保留了指向本文件的注释。