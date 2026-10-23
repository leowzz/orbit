import base64


def decode(segment):
    import av
    import numpy as np
    codec = av.CodecContext.create("opus", "r")
    codec.sample_rate = 16000
    codec.layout = "mono"
    resampler = av.AudioResampler(format="flt", layout="mono", rate=16000)
    chunks = []
    for data in segment["packets"]:
        frames = codec.decode(av.Packet(base64.b64decode(data, validate=True)))
        samples = 0
        for frame in frames:
            samples += round(frame.samples * 16000 / frame.sample_rate)
            for output in resampler.resample(frame):
                chunks.append(output.to_ndarray().reshape(-1))
        if samples != 320:
            raise ValueError("Opus packet is not a single 20 ms frame")
    for output in resampler.resample(None):
        chunks.append(output.to_ndarray().reshape(-1))
    audio = np.concatenate(chunks).astype(np.float32)
    if len(audio) != segment["duration_ms"] * 16:
        raise ValueError("decoded duration mismatch")
    return audio


def resolve_device(device):
    import torch
    selected = "cuda" if device == "auto" and torch.cuda.is_available() else device
    if selected == "auto":
        selected = "cpu"
    if selected == "cuda" and not torch.cuda.is_available():
        raise RuntimeError("CUDA requested but unavailable")
    return selected


class QwenModel:
    def __init__(self, model, device):
        import torch
        from qwen_asr import Qwen3ASRModel
        self.device = resolve_device(device)
        self.model = Qwen3ASRModel.from_pretrained(
            model, dtype=torch.float32 if self.device == "cpu" else torch.float16,
            device_map=self.device, max_inference_batch_size=1, max_new_tokens=512,
        )

    def transcribe(self, audio):
        result = self.model.transcribe(audio=(audio,16000), language=None, return_time_stamps=False)[0]
        return result.text, result.language
