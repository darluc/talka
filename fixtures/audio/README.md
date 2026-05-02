# Audio Fixtures

This repository keeps only deterministic, non-private audio fixtures in git.

- `zh-short.pcm` is a 16kHz mono 20ms-frame fake dictation fixture used by the local fake ASR/LLM/injector smoke path.
- `zh-short.wav` is the public FunASR/ModelScope Paraformer example WAV copied from `iic/speech_paraformer-large_asr_nat-zh-cn-16k-common-vocab8404-online/example/asr_example.wav`; real FunASR should return `欢迎大家来体验达摩院推出的语音识别模型`.

No private or user-recorded audio should be committed.
