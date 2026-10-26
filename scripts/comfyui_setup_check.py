#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
短剧平台 ComfyUI 环境体检 / 模板路径校正脚本（最小版）

只读地问一次 ComfyUI 的 /object_info，用它自己给出的模型候选列表当作
「用户实际安装路径」的唯一权威来源，比对平台模板并修正模板里的相对路径。

判定项：
  1. 缺失模型 / LoRA / 编码器 / 解码器
  2. 缺失节点（对应 ComfyUI 插件）
  3. 路径不一致   —— 同名文件在用户机器上的相对路径不同，可自动改写
  4. 同名多候选   —— 同一文件名在多个模型根下都有，需用户指定用哪个
  5. 疑似改名     —— 用户机器上只有名字相近的同一个模型，需用户指定
  6. 取值不受支持 —— 模板写的枚举值不在该节点的可选值里

可选 --map 显式指定替换：--map "旧值=新值"（可重复）
可选 --fix 把可自动改写与已指定的映射写回平台模板文件。

只写平台侧模板，不下载、不安装、不改用户的 ComfyUI。

用法:
  python scripts/comfyui_setup_check.py --comfy http://127.0.0.1:8188
  python scripts/comfyui_setup_check.py --comfy http://127.0.0.1:8188 --json
  python scripts/comfyui_setup_check.py --comfy http://127.0.0.1:8188 --fix
  python scripts/comfyui_setup_check.py --comfy http://127.0.0.1:8188 \
      --map "old/path/x.safetensors=models/text_encoders/x.safetensors" --fix
"""

import argparse
import json
import re
import sys
import urllib.error
import urllib.request
from pathlib import Path

MODEL_EXT_RE = re.compile(r"\.(safetensors|ckpt|pt|pth|bin|gguf|onnx|sft)$", re.I)
PLACEHOLDER_RE = re.compile(r"^\{\{(.+?)\}\}$")

# 缺失模型时，提示用户放到哪个目录
MODEL_DIR_HINTS = {
    "unet_name": ("models/diffusion_models/", "models/unet/"),
    "model_name": ("models/diffusion_models/", "models/unet/"),
    "ckpt_name": ("models/checkpoints/",),
    "clip_name": ("models/text_encoders/", "models/clip/"),
    "clip_name_2": ("models/text_encoders/", "models/clip/"),
    "vae_name": ("models/vae/",),
    "lora_name": ("models/loras/",),
    "style_model_name": ("models/sd_models/",),
    "audio_encoder_name": ("models/audio_encoders/",),
    "vocoder_name": ("models/vocoders/",),
}

DEFAULT_TEMPLATES = (
    Path(__file__).resolve().parent.parent
    / "backend"
    / "internal"
    / "service"
    / "templates"
)


def norm_path(value):
    """统一分隔符与大小写，便于跨 Windows/Linux 比较。"""
    return str(value).replace("\\", "/").lstrip("./").lower()


def base_name(value):
    return norm_path(value).rsplit("/", 1)[-1]


def stem(value):
    """去掉扩展名的文件名，用于「同一个模型但改了名字」的近似匹配。"""
    name = base_name(value)
    return name.rsplit(".", 1)[0] if "." in name else name


def is_model_path(value):
    return isinstance(value, str) and bool(MODEL_EXT_RE.search(value.strip()))


def similar_stem(left, right):
    """两个文件名是否像同一个模型（忽略扩展名，或互为前缀且不太短）。"""
    if not left or not right:
        return False
    if left == right:
        return True
    shorter, longer = sorted((left, right), key=len)
    return len(shorter) >= 8 and shorter in longer


def fetch_object_info(base_url, timeout):
    url = base_url.rstrip("/") + "/object_info"
    try:
        with urllib.request.urlopen(url, timeout=timeout) as resp:
            raw = resp.read()
    except urllib.error.HTTPError as exc:
        raise SystemExit(
            f"ComfyUI 返回 HTTP {exc.code}（{url}）。"
            "请确认地址正确、ComfyUI 已启动且版本支持 /object_info。"
        )
    except urllib.error.URLError as exc:
        raise SystemExit(f"连不上 ComfyUI（{url}）：{exc.reason}。请确认已启动且地址/端口正确。")
    try:
        return json.loads(raw.decode("utf-8", "replace"))
    except json.JSONDecodeError as exc:
        raise SystemExit(f"ComfyUI /object_info 返回的不是合法 JSON：{exc}")


def node_input_specs(node):
    """返回节点的 required / optional 输入定义。"""
    inputs = (node or {}).get("input") or {}
    specs = {}
    for section in ("required", "optional"):
        part = inputs.get(section) or {}
        if isinstance(part, dict):
            specs.update(part)
    return specs


def combo_options(node, input_name):
    """
    取某个输入的可选值列表（combo 型）。
    返回 None 表示该输入不是枚举（无法据此判断）。
    """
    spec = node_input_specs(node).get(input_name)
    if spec is None:
        return None
    if isinstance(spec, list) and spec and isinstance(spec[0], list):
        return [o for o in spec[0] if isinstance(o, str)]
    # 其余形态是类型声明（如 ["STRING", {}] / ["INT", {...}]），不是枚举
    return None


def load_templates(templates_dir):
    templates = []
    for path in sorted(Path(templates_dir).glob("*.json")):
        try:
            doc = json.loads(path.read_text(encoding="utf-8"))
        except Exception as exc:  # noqa: BLE001 - 模板坏了就跳过，不影响其它模板
            print(f"[跳过] {path.name}: 解析失败 {exc}", file=sys.stderr)
            continue
        if not isinstance(doc, dict) or not isinstance(doc.get("workflow"), dict):
            continue
        templates.append((path, doc))
    return templates


def template_defaults(doc):
    defaults = {}
    for item in doc.get("inputs") or []:
        if isinstance(item, dict) and "key" in item:
            defaults[str(item["key"])] = item.get("default")
    return defaults


def resolve_placeholder(doc, raw):
    """把 {{key}} 解析成 inputs 里对应的默认值；不是占位符就原样返回。"""
    if not isinstance(raw, str):
        return None, None
    text = raw.strip()
    match = PLACEHOLDER_RE.match(text)
    if not match:
        return text, None
    key = match.group(1).strip()
    value = template_defaults(doc).get(key)
    if not isinstance(value, str):
        return None, key
    return value.strip(), key


def collect_refs(doc):
    """
    收集模板里用到的模型路径。
    覆盖两种写法：inputs[].default 占位默认值、workflow 节点里写死的路径。
    """
    refs = []
    for node_id, node in (doc.get("workflow") or {}).items():
        if not isinstance(node, dict):
            continue
        class_type = node.get("class_type") or ""
        for input_name, raw in (node.get("inputs") or {}).items():
            value, placeholder = resolve_placeholder(doc, raw)
            if value is None or not is_model_path(value):
                continue
            refs.append(
                {
                    "node_id": str(node_id),
                    "class_type": class_type,
                    "input_name": input_name,
                    "placeholder": placeholder,
                    "value": value,
                }
            )
    return refs


def collect_classes(doc):
    classes = set()
    for node in (doc.get("workflow") or {}).values():
        if isinstance(node, dict):
            class_type = (node.get("class_type") or "").strip()
            if class_type:
                classes.add(class_type)
    return classes


def check_template(path, doc, object_info, mapping):
    """检查单个模板，返回该模板的引用与问题。"""
    code = doc.get("code") or path.stem
    refs = collect_refs(doc)
    used_classes = collect_classes(doc)

    missing_classes = sorted(c for c in used_classes if c not in object_info)
    missing_models = []
    path_mismatches = []
    ambiguous = []
    renamed = []
    unsupported = []
    mapped = []

    for ref in refs:
        explicit = mapping.get(norm_path(ref["value"]))
        if explicit:
            mapped.append(dict(ref, template=code, template_file=path.name, new=explicit))
            continue
        node = object_info.get(ref["class_type"])
        if node is None:
            # 节点本身没装，模型判定无从谈起，交给 missing_classes 处理
            continue
        options = combo_options(node, ref["input_name"])
        if options is None:
            continue
        normalized = {norm_path(o) for o in options}
        if norm_path(ref["value"]) in normalized:
            continue
        same_name = [o for o in options if base_name(o) == base_name(ref["value"])]
        if len(same_name) == 1:
            path_mismatches.append(
                dict(ref, template=code, template_file=path.name, suggestion=same_name[0])
            )
        elif len(same_name) > 1:
            ambiguous.append(
                dict(ref, template=code, template_file=path.name, candidates=same_name)
            )
        else:
            near = [o for o in options if similar_stem(stem(o), stem(ref["value"]))]
            if near:
                renamed.append(
                    dict(ref, template=code, template_file=path.name, candidates=near[:6])
                )
            else:
                missing_models.append(
                    dict(
                        ref,
                        template=code,
                        template_file=path.name,
                        hint_dirs=MODEL_DIR_HINTS.get(ref["input_name"], ()),
                    )
                )

    for node_id, node in (doc.get("workflow") or {}).items():
        if not isinstance(node, dict):
            continue
        class_type = node.get("class_type") or ""
        node_def = object_info.get(class_type)
        if node_def is None:
            continue
        for input_name, raw in (node.get("inputs") or {}).items():
            value, _ = resolve_placeholder(doc, raw)
            if value is None or is_model_path(value):
                continue
            options = combo_options(node_def, input_name)
            if not options or len(options) > 64:
                continue
            if value not in options:
                unsupported.append(
                    {
                        "template": code,
                        "template_file": path.name,
                        "node_id": str(node_id),
                        "class_type": class_type,
                        "input_name": input_name,
                        "value": value,
                        "options": options[:12],
                    }
                )

    return {
        "code": code,
        "file": path.name,
        "used_classes": sorted(used_classes),
        "missing_classes": missing_classes,
        "missing_models": missing_models,
        "path_mismatches": path_mismatches,
        "ambiguous": ambiguous,
        "renamed": renamed,
        "unsupported": unsupported,
        "mapped": mapped,
    }


def apply_fixes(results, templates_dir):
    """把可自动改写与已指定的映射写回模板文件（只改平台侧模板）。"""
    changes = []
    for result in results:
        if not result["path_mismatches"] and not result["mapped"]:
            continue
        path = Path(templates_dir) / result["file"]
        if not path.exists():
            continue
        content = path.read_text(encoding="utf-8")
        original = content
        pairs = []
        for item in result["path_mismatches"]:
            pairs.append((item["value"], item["suggestion"]))
        for item in result["mapped"]:
            pairs.append((item["value"], item["new"]))
        # 长值先替换，避免前缀被短值先吃掉
        pairs.sort(key=lambda p: len(p[0]), reverse=True)
        for old, new in pairs:
            old_literal = json.dumps(old)[1:-1]
            new_literal = json.dumps(new)[1:-1]
            if old_literal not in content:
                continue
            content = content.replace(old_literal, new_literal)
            changes.append(
                {
                    "template": result["code"],
                    "file": result["file"],
                    "old": old,
                    "new": new,
                }
            )
        if content != original:
            path.write_text(content, encoding="utf-8")
    return changes


def print_report(results, changes=None):
    def total(key):
        return sum(len(r[key]) for r in results)

    node_classes = {}
    for result in results:
        for class_type in result["missing_classes"]:
            node_classes.setdefault(class_type, []).append(result["code"])

    print("=" * 68)
    print("ComfyUI 环境体检")
    print("=" * 68)

    print(f"\n[缺失模型 / LoRA / 编码器] {total('missing_models')} 项")
    if not total("missing_models"):
        print("  无")
    for result in results:
        for item in result["missing_models"]:
            hints = " 或 ".join(item["hint_dirs"]) if item["hint_dirs"] else "ComfyUI 默认模型目录"
            print(f"  - {item['value']}")
            print(f"      模板: {item['template']}  输入: {item['input_name']}")
            print(f"      建议放到: {hints}")

    print(f"\n[缺失节点（对应 ComfyUI 插件）] {len(node_classes)} 项")
    if not node_classes:
        print("  无")
    for class_type, codes in sorted(node_classes.items()):
        print(f"  - {class_type}   影响模板: {', '.join(sorted(set(codes)))}")
        print("      请按节点名在 ComfyUI-Manager 里搜索并安装对应插件")

    print(f"\n[路径不一致（可自动改写）] {total('path_mismatches')} 项")
    if not total("path_mismatches"):
        print("  无")
    for result in results:
        for item in result["path_mismatches"]:
            print(f"  - {item['template']} / {item['input_name']}")
            print(f"      模板写: {item['value']}")
            print(f"      实际有: {item['suggestion']}")

    print(f"\n[同名多候选（需你指定用哪个）] {total('ambiguous')} 项")
    if not total("ambiguous"):
        print("  无")
    for result in results:
        for item in result["ambiguous"]:
            print(f"  - {item['template']} / {item['input_name']}: {item['value']}")
            for candidate in item["candidates"]:
                print(f"      候选: {candidate}")
            print(f"      用 --map \"{item['value']}=<候选>\" 指定")

    print(f"\n[疑似改名（需你确认是不是同一个模型）] {total('renamed')} 项")
    if not total("renamed"):
        print("  无")
    for result in results:
        for item in result["renamed"]:
            print(f"  - {item['template']} / {item['input_name']}: {item['value']}")
            for candidate in item["candidates"]:
                print(f"      候选: {candidate}")
            print(f"      用 --map \"{item['value']}=<候选>\" 指定")

    print(f"\n[取值不受支持] {total('unsupported')} 项")
    if not total("unsupported"):
        print("  无")
    for result in results:
        for item in result["unsupported"]:
            print(f"  - {item['template']} / {item['class_type']}.{item['input_name']}")
            print(f"      模板写: {item['value']}")
            print(f"      可选值: {', '.join(item['options'])}")

    if total("mapped"):
        print(f"\n[已按 --map 指定] {total('mapped')} 项")
        for result in results:
            for item in result["mapped"]:
                print(f"  - {item['template']} / {item['input_name']}")
                print(f"      {item['value']}  ->  {item['new']}")

    if changes:
        print(f"\n[已改写模板文件] {len(changes)} 处")
        for change in changes:
            print(f"  - {change['file']}")
            print(f"      {change['old']}  ->  {change['new']}")

    print("\n" + "=" * 68)


def parse_mapping(pairs):
    mapping = {}
    for pair in pairs or []:
        if "=" not in pair:
            raise SystemExit(f"--map 需要 旧值=新值 形式: {pair}")
        old, new = pair.split("=", 1)
        old = old.strip()
        new = new.strip()
        if not old or not new:
            raise SystemExit(f"--map 需要 旧值=新值 形式: {pair}")
        mapping[norm_path(old)] = new
    return mapping


def main():
    parser = argparse.ArgumentParser(description="检查 ComfyUI 是否满足短剧平台模板要求")
    parser.add_argument("--comfy", default="http://127.0.0.1:8188", help="ComfyUI 地址")
    parser.add_argument("--templates", default=str(DEFAULT_TEMPLATES), help="平台模板目录")
    parser.add_argument("--timeout", type=float, default=30.0, help="请求超时秒数")
    parser.add_argument("--map", dest="mapping", action="append", help="显式替换 旧值=新值（可重复）")
    parser.add_argument("--json", action="store_true", help="输出 JSON")
    parser.add_argument("--fix", action="store_true", help="把可改写的路径写回平台模板")
    args = parser.parse_args()

    templates_dir = Path(args.templates)
    if not templates_dir.is_dir():
        raise SystemExit(f"模板目录不存在: {templates_dir}")

    mapping = parse_mapping(args.mapping)
    object_info = fetch_object_info(args.comfy, args.timeout)
    templates = load_templates(templates_dir)
    if not templates:
        raise SystemExit(f"模板目录里没有可用模板: {templates_dir}")

    results = [check_template(path, doc, object_info, mapping) for path, doc in templates]

    changes = []
    if args.fix:
        changes = apply_fixes(results, templates_dir)

    if args.json:
        print(
            json.dumps(
                {
                    "comfy": args.comfy,
                    "templates_dir": str(templates_dir),
                    "node_count": len(object_info),
                    "results": results,
                    "changes": changes,
                },
                ensure_ascii=False,
                indent=2,
            )
        )
        return

    print_report(results, changes)

    blocking = sum(len(r["missing_models"]) for r in results)
    blocking += sum(len(r["missing_classes"]) for r in results)
    if blocking:
        print("提示: 缺模型请自行下载后放入上述目录；缺插件请用 ComfyUI-Manager 安装。")
        print("      平台不会代为下载或安装。")


if __name__ == "__main__":
    main()
