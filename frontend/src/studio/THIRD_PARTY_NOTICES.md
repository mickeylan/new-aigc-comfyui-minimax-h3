# 第三方来源与许可声明（Studio 岛）

`frontend/src/studio/` 下的代码有两类来源：移植自带许可的第三方项目，以及通过 npm 依赖引入的运行时库。本文件是这两类来源的清单，供分发时随包附上。

## 一、移植来源

| 目录 | 来源 | 许可 |
|---|---|---|
| `video-editor/` | [opencut-classic](https://github.com/opencut-app/opencut-classic)（原 opencut-app/opencut 的上一代实现）：时间线、属性面板、素材面板、字体选择器、对话框与编辑器外壳 | MIT |
| `pagedraw/`、`pagedraw-host/` | ArtCraft（本项目作者自有仓库）：2D 画布外壳、图层与笔刷交互 | 双许可 MIT / Apache-2.0 |
| `scene3d/` | ArtCraft（本项目作者自有仓库）：3D 场景外壳、变换 gizmo 与骨级摆姿（FK） | 双许可 MIT / Apache-2.0 |

opencut-classic 的 MIT 许可要求保留版权声明与许可全文。本项目根目录的 `LICENSE` 只覆盖本仓库自身代码，不替代上游声明，因此移植部分按下列方式声明：

```
Portions of frontend/src/studio/video-editor are derived from opencut-classic
(https://github.com/opencut-app/opencut-classic), licensed under the MIT License.
Copyright (c) opencut-app contributors.
```

## 二、随 studio chunk 分发的运行时依赖

| 包 | 版本 | 许可 | 说明 |
|---|---|---|---|
| `react` / `react-dom` | 见 `frontend/package.json` | MIT | 岛内渲染 |
| `three` | 0.180.0 | MIT | 3D 场景、GLB 导入导出 |
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