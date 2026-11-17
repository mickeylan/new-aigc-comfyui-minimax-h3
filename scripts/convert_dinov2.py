"""把 VOSR2 用的 DINOv2-L 权重从 .pth 转成 safetensors。

来源：https://huggingface.co/CSWRY/VOSR/resolve/main/torch_cache/checkpoints/dinov2_vitl14_pretrain.pth
用法（必须用 ComfyUI 自带的 Python，便携版为 python_embeded\\python.exe）：

    python convert_dinov2.py <dinov2_vitl14_pretrain.pth 路径> <ComfyUI 根目录>

输出到 <ComfyUI 根目录>\\models\\vosr2\\VOSR2\\dinov2_vitl14.safetensors。
第二个参数也可以直接给一个以 .safetensors 结尾的输出文件路径。
"""

import os
import sys

MIN_SOURCE_BYTES = 500 * 1024 * 1024  # 完整文件约 1.2GB，过小说明下载到的是网页或不完整


def output_path(target: str) -> str:
    if target.lower().endswith(".safetensors"):
        return os.path.abspath(target)
    return os.path.abspath(os.path.join(target, "models", "vosr2", "VOSR2", "dinov2_vitl14.safetensors"))


def main() -> int:
    if len(sys.argv) != 3:
        print(__doc__)
        return 2
    src, dst = os.path.abspath(sys.argv[1]), output_path(sys.argv[2])
    print("python:", sys.executable)
    print("源文件:", src)
    print("输出到:", dst)
    if not os.path.isfile(src):
        print("错误：找不到源文件，请检查第一个参数")
        return 1
    size = os.path.getsize(src)
    print(f"源文件大小: {size / 1024 / 1024:.1f} MB")
    if size < MIN_SOURCE_BYTES:
        print("错误：文件太小（正常约 1.2GB），下载到的可能是网页或文件不完整，请重新下载")
        return 1

    try:
        import torch
        from safetensors.torch import save_file
    except ImportError as exc:
        print(f"错误：{exc}。请改用 ComfyUI 自带的 Python 运行本脚本")
        return 1
    print("torch:", torch.__version__)

    sd = torch.load(src, map_location="cpu", weights_only=True)
    if isinstance(sd, dict) and "model" in sd and isinstance(sd["model"], dict):
        sd = sd["model"]
    if not isinstance(sd, dict) or not sd:
        print(f"错误：源文件内容不是权重字典（类型 {type(sd).__name__}）")
        return 1
    print("权重数量:", len(sd))

    os.makedirs(os.path.dirname(dst), exist_ok=True)
    save_file({k: v.contiguous() for k, v in sd.items()}, dst)
    print(f"已生成: {dst}  {os.path.getsize(dst) / 1024 / 1024:.1f} MB")
    return 0


if __name__ == "__main__":
    sys.exit(main())
