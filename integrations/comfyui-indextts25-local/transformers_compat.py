"""Process-local compatibility aliases for the official IndexTTS 2.5 core.

The upstream core vendors generation helpers written for Transformers 4.52.
Current ComfyUI uses Transformers 5.x.  This module only restores names removed
from public import locations; it never changes the installed package.
"""

from __future__ import annotations

import importlib.util
import math
import sys
import types
from pathlib import Path


def _install_lazy_module_alias(module, name: str, value):
    """Replace a Transformers 5 lazy placeholder with a delegating module."""
    replacement = types.ModuleType(module.__name__)
    replacement.__dict__.update(module.__dict__)
    replacement.__dict__[name] = value
    fallback = module.__dict__.get("__getattr__")
    if fallback is not None:
        replacement.__dict__["__getattr__"] = fallback
    sys.modules[module.__name__] = replacement
    parent_name, child_name = module.__name__.rsplit(".", 1)
    parent = sys.modules.get(parent_name)
    if parent is not None:
        parent.__dict__[child_name] = replacement
    return replacement


def _load_module(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise ImportError(f"Cannot load compatibility module {name} from {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


def install_indextts_generation_stack() -> None:
    """Load the coordinated IndexTTS generator/GPT/cache stack for Transformers 5."""
    root = Path(__file__).with_name("indextts5_compat")
    if not root.is_dir():
        raise FileNotFoundError(f"IndexTTS generation compatibility stack missing: {root}")

    import indextts.gpt  # ensure parent packages exist for namespaced modules

    # Transformers 5 removed the public beam modules still used by the
    # IndexTTS-specific GenerationMixin. Restore those 4.52 modules process-
    # locally without replacing the installed Transformers package.
    for name, filename in (
        ("transformers.generation.beam_constraints", "beam_constraints.py"),
        ("transformers.generation.beam_search", "beam_search.py"),
    ):
        if name not in sys.modules:
            _load_module(name, root / filename)

    import transformers.generation.logits_process as logits_process
    if not hasattr(logits_process, "HammingDiversityLogitsProcessor"):
        class HammingDiversityLogitsProcessor(logits_process.LogitsProcessor):
            def __init__(self, diversity_penalty, num_beams, num_beam_groups):
                self._diversity_penalty = float(diversity_penalty)
                self._num_beams = int(num_beams)
                self._num_sub_beams = self._num_beams // int(num_beam_groups)

            def __call__(self, input_ids, scores, current_tokens, beam_group_idx):
                group_start = beam_group_idx * self._num_sub_beams
                if group_start == 0:
                    return scores
                group_size = min(group_start + self._num_sub_beams, self._num_beams) - group_start
                batch_size = current_tokens.shape[0] // self._num_beams
                result = scores.clone()
                for batch in range(batch_size):
                    prior = current_tokens[batch * self._num_beams : batch * self._num_beams + group_start]
                    frequency = prior.bincount(minlength=scores.shape[-1]).to(scores.device)
                    result[batch * group_size : (batch + 1) * group_size] -= self._diversity_penalty * frequency
                return result

        logits_process.HammingDiversityLogitsProcessor = HammingDiversityLogitsProcessor

    # Loading order matters: GPT2 imports the coordinated generation module,
    # and model_v2 imports both of them.
    _load_module(
        "indextts.gpt.transformers_generation_utils",
        root / "transformers_generation_utils.py",
    )
    _load_module("indextts.gpt.transformers_gpt2", root / "transformers_gpt2.py")
    _load_module("indextts.gpt.model_v2", root / "model_v2.py")


def install_gpt_generation_compat(model_module) -> None:
    """Legacy fallback retained for tests; production uses the coordinated stack."""
    from transformers.generation import GenerationMixin

    original = model_module.GPT2InferenceModel
    if issubclass(original, GenerationMixin):
        return

    class GPT2InferenceModel(original, GenerationMixin):
        # This vendored GPT implementation consumes legacy tuple caches. The
        # host Transformers 5 DynamicCache path corrupts its autoregressive
        # context unless the model is paired with T8's separate generator.
        _supports_cache_class = False

        @staticmethod
        def _reorder_cache(past, beam_idx):
            if hasattr(past, "reorder_cache"):
                past.reorder_cache(beam_idx)
                return past
            if past is None:
                return None

            def reorder(state):
                if state is None:
                    return None
                if isinstance(state, (tuple, list)):
                    return type(state)(reorder(item) for item in state)
                return state.index_select(0, beam_idx.to(state.device))

            return reorder(past)

    GPT2InferenceModel.__name__ = original.__name__
    GPT2InferenceModel.__qualname__ = original.__qualname__
    GPT2InferenceModel.__module__ = original.__module__
    model_module.GPT2InferenceModel = GPT2InferenceModel


def install_bigvgan_compat(bigvgan_class) -> None:
    """Adapt IndexTTS BigVGAN to huggingface_hub >= 1.33.

    New HubMixin versions stopped forwarding the obsolete `proxies` and
    `resume_download` arguments. The vendored BigVGAN still requires them even
    for a fully local model directory.
    """
    if getattr(bigvgan_class, "_indextts25_hub_compat", False):
        return
    descriptor = bigvgan_class.__dict__["_from_pretrained"]
    original = descriptor.__func__

    def compatible_from_pretrained(
        cls,
        *,
        model_id,
        revision=None,
        cache_dir=None,
        force_download=False,
        local_files_only=False,
        token=None,
        map_location="cpu",
        strict=False,
        use_cuda_kernel=False,
        proxies=None,
        resume_download=None,
        **model_kwargs,
    ):
        return original(
            cls,
            model_id=model_id,
            revision=revision,
            cache_dir=cache_dir,
            force_download=force_download,
            proxies=proxies,
            resume_download=resume_download,
            local_files_only=local_files_only,
            token=token,
            map_location=map_location,
            strict=strict,
            use_cuda_kernel=use_cuda_kernel,
            **model_kwargs,
        )

    bigvgan_class._from_pretrained = classmethod(compatible_from_pretrained)
    bigvgan_class._indextts25_hub_compat = True


def install() -> None:
    # Descript AudioTools imports TensorBoard training utilities at module import
    # time, although IndexTTS inference never uses them. Some ComfyUI installs
    # expose a partial TensorFlow module that makes torch.utils.tensorboard crash.
    # Keep that optional training surface isolated from inference.
    tensorboard_name = "torch.utils.tensorboard"
    try:
        import torch.utils.tensorboard  # noqa: F401
    except (ImportError, AttributeError):
        tensorboard_bridge = types.ModuleType(tensorboard_name)

        class SummaryWriter:
            def __init__(self, *args, **kwargs):
                pass

            def __getattr__(self, name):
                return lambda *args, **kwargs: None

            def close(self):
                pass

        tensorboard_bridge.SummaryWriter = SummaryWriter
        sys.modules[tensorboard_name] = tensorboard_bridge

    import transformers.cache_utils as cache_utils
    import transformers.configuration_utils as configuration_utils
    import transformers.generation.candidate_generator as candidate_generator
    import transformers.generation.configuration_utils as generation_configuration
    import transformers.modeling_utils as modeling_utils
    import transformers.pytorch_utils as pytorch_utils
    import transformers.tokenization_utils as tokenization_utils
    from transformers.models.gpt2.modeling_gpt2 import GPT2SequenceSummary
    from transformers.tokenization_python import ExtensionsTrie

    # Transformers 5 exposes a lazy placeholder module that discards setattr().
    tokenization_utils = _install_lazy_module_alias(
        tokenization_utils, "ExtensionsTrie", ExtensionsTrie
    )

    import torch

    if not hasattr(configuration_utils.PretrainedConfig, "_get_non_default_generation_parameters"):
        configuration_utils.PretrainedConfig._get_non_default_generation_parameters = lambda self: {}

    if not hasattr(pytorch_utils, "isin_mps_friendly"):
        pytorch_utils.isin_mps_friendly = lambda elements, test_elements: torch.isin(elements, test_elements)

    if not hasattr(pytorch_utils, "find_pruneable_heads_and_indices"):
        def find_pruneable_heads_and_indices(heads, n_heads, head_size, already_pruned_heads):
            mask = torch.ones(n_heads, head_size)
            heads = set(heads) - already_pruned_heads
            for head in heads:
                shifted = head - sum(1 if previous < head else 0 for previous in already_pruned_heads)
                mask[shifted] = 0
            index = torch.arange(mask.numel())[mask.view(-1).contiguous().eq(1)].long()
            return heads, index

        pytorch_utils.find_pruneable_heads_and_indices = find_pruneable_heads_and_indices

    if not hasattr(pytorch_utils, "prune_conv1d_layer"):
        def prune_conv1d_layer(layer, index, dim=1):
            index = index.to(layer.weight.device)
            weight = layer.weight.index_select(dim, index).detach().clone()
            bias = layer.bias.detach().clone() if dim == 0 else layer.bias[index].detach().clone()
            size = list(layer.weight.size())
            size[dim] = len(index)
            new_layer = pytorch_utils.Conv1D(size[1], size[0]).to(layer.weight.device)
            with torch.no_grad():
                new_layer.weight.copy_(weight.contiguous())
                new_layer.bias.copy_(bias.contiguous())
            return new_layer

        pytorch_utils.prune_conv1d_layer = prune_conv1d_layer

    if not hasattr(cache_utils, "OffloadedCache"):
        # IndexTTS never selects offloaded cache in its normal inference path.
        # Keeping an import-compatible subclass is safer than mutating generation.
        class OffloadedCache(cache_utils.DynamicCache):
            pass

        cache_utils.OffloadedCache = OffloadedCache

    if not hasattr(cache_utils, "QuantizedCacheConfig"):
        class QuantizedCacheConfig:
            def __init__(self, backend: str = "quanto", **kwargs):
                self.backend = backend
                for key, value in kwargs.items():
                    setattr(self, key, value)

        cache_utils.QuantizedCacheConfig = QuantizedCacheConfig

    if not hasattr(candidate_generator, "_crop_past_key_values"):
        def _crop_past_key_values(model, past_key_values, max_length):
            if isinstance(past_key_values, cache_utils.Cache):
                past_key_values.crop(max_length)
                return past_key_values
            if past_key_values is None:
                return None
            if getattr(model.config, "is_encoder_decoder", False):
                return tuple(
                    (layer[0][:, :, :max_length, :], layer[1][:, :, :max_length, :], layer[2], layer[3])
                    for layer in past_key_values
                )
            return tuple(
                (layer[0][:, :, :max_length, :], layer[1][:, :, :max_length, :])
                if layer != ([], [])
                else layer
                for layer in past_key_values
            )

        candidate_generator._crop_past_key_values = _crop_past_key_values

    if not hasattr(generation_configuration.GenerationConfig, "return_legacy_cache"):
        generation_configuration.GenerationConfig.return_legacy_cache = None

    if not hasattr(generation_configuration, "NEED_SETUP_CACHE_CLASSES_MAPPING"):
        generation_configuration.NEED_SETUP_CACHE_CLASSES_MAPPING = {
            "static": cache_utils.StaticCache,
        }
    if not hasattr(generation_configuration, "QUANT_BACKEND_CLASSES_MAPPING"):
        generation_configuration.QUANT_BACKEND_CLASSES_MAPPING = {}
    if not hasattr(modeling_utils, "SequenceSummary"):
        modeling_utils.SequenceSummary = GPT2SequenceSummary

    if "transformers.utils.model_parallel_utils" not in sys.modules:
        model_parallel = types.ModuleType("transformers.utils.model_parallel_utils")

        def assert_device_map(device_map, num_blocks):
            expected = set(range(num_blocks))
            assigned = [item for values in device_map.values() for item in values]
            if len(assigned) != len(set(assigned)) or set(assigned) != expected:
                raise ValueError("device_map must assign every attention block exactly once")

        def get_device_map(n_layers, devices):
            block = int(math.ceil(n_layers / len(devices)))
            layers = list(range(n_layers))
            return dict(zip(devices, [layers[i : i + block] for i in range(0, n_layers, block)]))

        model_parallel.assert_device_map = assert_device_map
        model_parallel.get_device_map = get_device_map
        sys.modules[model_parallel.__name__] = model_parallel

    # Keep the host PreTrainedModel implementation, but load IndexTTS's
    # coordinated generation/GPT/cache stack rather than host GenerationMixin.
    from transformers.modeling_utils import PreTrainedModel

    modeling_bridge = types.ModuleType("indextts.gpt.transformers_modeling_utils")
    modeling_bridge.PreTrainedModel = PreTrainedModel
    sys.modules[modeling_bridge.__name__] = modeling_bridge

    # The upstream tree references indextts.BigVGAN.env but does not ship that
    # module. The same project ships the canonical AttrDict implementation in
    # s2mel/modules/bigvgan/env.py, so expose that implementation under the
    # missing import path without changing the upstream source directory.
    if "indextts.BigVGAN.env" not in sys.modules:
        try:
            from indextts.s2mel.modules.bigvgan.env import AttrDict, build_env
        except ModuleNotFoundError as exc:
            if exc.name != "indextts":
                raise
        else:
            bigvgan_env = types.ModuleType("indextts.BigVGAN.env")
            bigvgan_env.AttrDict = AttrDict
            bigvgan_env.build_env = build_env
            sys.modules[bigvgan_env.__name__] = bigvgan_env

    # IndexTTS imports AutoModelForCausalLM through ModelScope, but uses the
    # standard Hugging Face from_pretrained API. Avoid adding another package
    # to ComfyUI's shared environment when Transformers already provides it.
    try:
        import modelscope  # noqa: F401
    except ModuleNotFoundError:
        from transformers import AutoModelForCausalLM

        modelscope_bridge = types.ModuleType("modelscope")
        modelscope_bridge.AutoModelForCausalLM = AutoModelForCausalLM
        sys.modules["modelscope"] = modelscope_bridge

    install_indextts_generation_stack()
