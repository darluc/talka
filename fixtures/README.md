# Talka Fixtures

This scaffold keeps only lightweight, deterministic, non-private fixtures in git.

- `fixtures/asr/` holds text fixtures for fake ASR/LLM contract tests.
- `fixtures/audio/` holds deterministic synthetic audio fixtures such as `zh-short.pcm` and `zh-short.wav` for local smoke and provider tests.
- `fixtures/llm/` holds deterministic fake cleanup outputs for local-only LLM contract tests.
- `fixtures/config/` holds sample configuration files aligned with the documented schema.

No raw private audio, secrets, or downloaded model binaries should be committed.
