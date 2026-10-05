import importlib.util

import pytest

from transformers_compat import install_bigvgan_compat, install_gpt_generation_compat


def test_gpt_compat_adds_transformers5_generation_mixin():
    from transformers.generation import GenerationMixin
    from transformers.modeling_utils import PreTrainedModel

    class BareInferenceModel(PreTrainedModel):
        pass

    class FakeModule:
        GPT2InferenceModel = BareInferenceModel

    install_gpt_generation_compat(FakeModule)
    assert issubclass(FakeModule.GPT2InferenceModel, GenerationMixin)
    assert hasattr(FakeModule.GPT2InferenceModel, "generate")
    assert FakeModule.GPT2InferenceModel._supports_cache_class is False


def test_gpt_compat_reorders_tuple_cache_and_preserves_none():
    import torch
    from transformers.modeling_utils import PreTrainedModel

    class BareInferenceModel(PreTrainedModel):
        pass

    class FakeModule:
        GPT2InferenceModel = BareInferenceModel

    install_gpt_generation_compat(FakeModule)
    key = torch.tensor([[10], [20], [30]])
    value = torch.tensor([[11], [21], [31]])
    cache = ((key, value, None),)
    reordered = FakeModule.GPT2InferenceModel._reorder_cache(
        cache, torch.tensor([2, 0])
    )
    assert reordered[0][0].tolist() == [[30], [10]]
    assert reordered[0][1].tolist() == [[31], [11]]
    assert reordered[0][2] is None


def test_bigvgan_compat_supplies_removed_hub_arguments():
    calls = []

    class FakeBigVGAN:
        @classmethod
        def _from_pretrained(
            cls,
            *,
            model_id,
            revision,
            cache_dir,
            force_download,
            proxies,
            resume_download,
            local_files_only,
            token,
            **kwargs,
        ):
            calls.append((model_id, proxies, resume_download, kwargs))
            return "loaded"

    install_bigvgan_compat(FakeBigVGAN)
    assert FakeBigVGAN._from_pretrained(
        model_id="local-model",
        revision=None,
        cache_dir=None,
        force_download=False,
        local_files_only=True,
        token=None,
    ) == "loaded"
    assert calls == [("local-model", None, None, {"map_location": "cpu", "strict": False, "use_cuda_kernel": False})]


@pytest.mark.skipif(importlib.util.find_spec("transformers") is None, reason="transformers unavailable")
def test_compat_installs_removed_transformers_symbols():
    import transformers.cache_utils as cache_utils
    import transformers.generation.candidate_generator as candidate_generator
    import transformers.generation.configuration_utils as generation_configuration
    import transformers.modeling_utils as modeling_utils
    import transformers.pytorch_utils as pytorch_utils

    from transformers_compat import install

    install()

    assert hasattr(cache_utils, "OffloadedCache")
    assert hasattr(cache_utils, "QuantizedCacheConfig")
    assert hasattr(candidate_generator, "_crop_past_key_values")
    assert hasattr(generation_configuration, "NEED_SETUP_CACHE_CLASSES_MAPPING")
    assert hasattr(generation_configuration, "QUANT_BACKEND_CLASSES_MAPPING")
    assert hasattr(modeling_utils, "SequenceSummary")
    assert hasattr(pytorch_utils, "isin_mps_friendly")
    if importlib.util.find_spec("indextts") is not None:
        from indextts.BigVGAN.env import AttrDict

        assert AttrDict({"value": 3}).value == 3
