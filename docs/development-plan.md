# Talka Development Plan

## Guiding Principles

- Build the smallest complete local-first loop first: iOS audio to Mac, ASR, Ollama cleanup, text insertion.
- Validate risky native dependencies before polishing UI.
- Keep ASR, LLM, transport, and text injection behind interfaces.
- Prefer an embedded, app-bundled FunASR runtime in the default path, while keeping external runtime and legacy sidecar support behind the same ASR interface.
- Keep debug data private by default.

## Milestone 0: Technical Spike

Goal: prove that the core native runtime path works on the target Mac.

Tasks:

- Build or install FunASR C++/ONNX Runtime on macOS.
- Prepare ONNX model directories for Paraformer ASR, FSMN-VAD, CT-Transformer punctuation, and ITN.
- Run file transcription against a local WAV sample.
- Run realtime or 2-pass WebSocket recognition with streamed PCM input.
- Measure cold-start time, model load time, and average recognition latency.
- Verify Ollama is reachable at `http://localhost:11434`.
- Verify Ollama can perform conservative Chinese text cleanup.

Acceptance:

- A local audio file can be transcribed without Python.
- A short streamed utterance can produce a final transcript.
- The ASR runtime can be launched as a standalone local process.
- Ollama cleanup returns only final text with no extra commentary.

## Milestone 1: Project Skeleton

Goal: create the codebase structure and core interfaces.

Tasks:

- Create Go module.
- Create iOS app skeleton.
- Create macOS SwiftUI shell skeleton.
- Create Go command `cmd/talka-server`.
- Define core interfaces:
  - `ASRProvider`
  - `LLMProvider`
  - `TextInjector`
  - `PairingStore`
  - `SessionTransport`
- Add config loading and validation.
- Add structured logging.

Acceptance:

- Go server starts locally.
- macOS shell can query server status.
- Config can be loaded from a user config file.
- Unit tests cover config defaults and validation.

## Milestone 2: ASR Runtime Integration

Goal: connect Go to FunASR C++/ONNX Runtime across embedded and external modes.

Tasks:

- Add embedded ASR runtime process manager.
- Add direct websocket client for external FunASR runtimes.
- Keep legacy Talka sidecar compatibility behind a separate provider.
- Add health checks for embedded and external runtime modes.
- Add local WebSocket or Unix socket ASR client.
- Implement phrase-level transcription request path.
- Implement streaming session path if FunASR runtime supports it cleanly.
- Normalize ASR responses into Talka transcript segments.
- Add runtime restart policy.
- Ensure the macOS shell only generates embedded defaults on first launch and preserves saved external or sidecar config on later launches.

Acceptance:

- Go can start and stop the embedded ASR runtime.
- Go can connect directly to an external FunASR runtime.
- Go can send audio and receive final ASR text in both modes.
- ASR runtime failure is detected and surfaced.
- Missing model paths produce actionable errors.

## Milestone 3: Ollama Text Processing

Goal: implement text cleanup and final optimization through local Ollama.

Tasks:

- Implement Ollama client for `/api/chat`.
- Add prompt templates for:
  - Segment cleanup.
  - Final cleanup.
- Add timeout and cancellation handling.
- Add tests with fixture ASR outputs.
- Add config for base URL and model.

Acceptance:

- Raw ASR text is cleaned without added facts.
- Chinese punctuation and spacing improve.
- Ollama failures do not lose the raw transcript.
- The final output contains only the text to insert.

## Milestone 4: macOS Text Injection

Goal: insert final text into the focused app.

Tasks:

- Implement native Accessibility permission detection in the Swift app process with `AXIsProcessTrusted()`.
- Add UI guidance to open macOS Privacy & Security settings.
- Implement a Swift local paste broker over Unix domain socket.
- Add a broker `preflight` operation before clipboard mutation.
- Implement clipboard save, paste, and restore in Go.
- Add insertion history for failed paste recovery.
- Test against common apps:
  - Apple Notes.
  - Safari text field.
  - WeChat.
  - VS Code.

Acceptance:

- Text can be pasted into common focused inputs.
- Missing permission is detected before insertion.
- Missing permission does not mutate the clipboard.
- Clipboard is restored after successful insertion.
- Failed insertion keeps the final text recoverable.
- Ad-hoc signing and TCC reset behavior are documented for local test builds.

## Milestone 5: Local Discovery

Goal: allow iOS to find the Mac service.

Tasks:

- Implement Bonjour advertisement in Go or macOS shell.
- Define service type `_talka._tcp`.
- Add iOS Bonjour browsing.
- Add iOS local network permission strings.
- Show discovered Macs in the iOS app.

Acceptance:

- iOS discovers a nearby Mac running Talka.
- Discovery waits until the user initiates connection.
- iOS handles local network permission denial clearly.

## Milestone 6: PIN Pairing And Secure Sessions

Goal: prevent unauthorized local devices from streaming audio.

Tasks:

- Implement pairing start endpoint on macOS.
- Show six-digit PIN in macOS window.
- Implement iOS PIN entry UI.
- Implement X25519 key exchange.
- Implement HMAC-SHA256 transcript confirmation using the PIN.
- Derive session keys with HKDF-SHA256.
- Encrypt protocol messages with ChaCha20-Poly1305.
- Store paired identity in Keychain on both sides.
- Add failed-attempt rate limiting.

Acceptance:

- Wrong PIN cannot connect.
- Correct PIN creates a persistent pairing.
- Reconnect works without a new PIN.
- Captured traffic does not expose audio or text.
- Replayed messages are rejected.

## Milestone 7: iOS Audio Streaming

Goal: stream microphone audio from iOS to Mac.

Tasks:

- Implement AVAudioEngine capture.
- Resample to 16 kHz mono PCM if needed.
- Frame audio into 20 ms or 40 ms chunks.
- Send encrypted `audio_start`, `audio_frame`, and `audio_stop` messages.
- Add microphone button states.
- Add waveform or level animation.
- Add reconnect behavior for known paired Mac.

Acceptance:

- Mac receives continuous PCM frames from iOS.
- Audio frames can be reconstructed as a valid WAV during debug mode.
- Recording start/stop state is clear.
- Network interruption produces a recoverable error.

## Milestone 8: End-To-End MVP

Goal: complete one full dictation path.

Tasks:

- Connect iOS audio stream to ASR runtime.
- Accumulate ASR final segments.
- Call Ollama final cleanup on recording stop.
- Insert final text into the focused app.
- Add minimal macOS status display.
- Add transcript fallback if insertion fails.

Acceptance:

- User can speak into iPhone and see final text appear on Mac.
- The full path works without Python.
- The system handles ASR and Ollama failures gracefully.
- The MVP works after restarting both apps.

## Milestone 9: Settings And Device Management

Goal: make the MVP configurable and maintainable.

Tasks:

- Add compact macOS settings window.
- Combine service state, Accessibility state, PIN, and countdown in the top status area.
- Configure AI endpoint, AI model, and AI timeout.
- Configure user-facing ASR mode as `Embedded` or `External`.
- Enable ASR host and port editing only in `External` mode.
- Show AI API and ASR API health inline.
- Show connected devices at the bottom with name and connection time.
- Move diagnostics into a separate window focused on failure evidence, runtime evidence, and recovery.
- Keep low-frequency actions such as Diagnostics in the footer.

Acceptance:

- User can change Ollama endpoint without editing files.
- User can see whether ASR runtime is healthy.
- User can see connected devices and connection times.
- Settings do not expose advanced or low-value controls in the main compact view.
- Diagnostics are useful without duplicating the settings page or exposing private audio by default.
- Tray menu contains only Settings and Quit.
- Tray green dot only lights when service, AI, ASR, and native Accessibility permission are all healthy.

## Milestone 10: Packaging

Goal: package the local-first product for normal use.

Tasks:

- Bundle Go server with macOS app.
- Bundle FunASR runtime binary.
- Bundle or download ONNX models.
- Bundle ONNX Runtime dylibs.
- Handle app signing and permissions.
- Ensure Dock icon uses the rounded app icon asset.
- Ensure tray icon uses the latest template SVG-derived asset at the correct menu bar size.
- Document TCC behavior for ad-hoc builds and use stable signing for release builds.
- Add first-run onboarding.
- Build iOS app with required permissions.

Acceptance:

- macOS app launches the Go service and ASR runtime.
- Required dylibs load correctly on target architecture.
- First-run onboarding guides local network, microphone, and Accessibility permissions.
- The app can be installed and run without manual terminal commands.

## Milestone 11: Quality Pass

Goal: improve latency, stability, and recognition quality.

Tasks:

- Benchmark ASR latency by chunk size.
- Tune VAD endpointing.
- Add overlap deduplication if needed.
- Add prompt regression tests for Ollama cleanup.
- Add reconnection stress tests.
- Add long-session memory checks.
- Add crash recovery tests for the embedded ASR runtime.

Acceptance:

- Typical short dictation returns final text quickly enough for daily use.
- The embedded ASR runtime restarts without requiring app relaunch.
- Text cleanup does not over-edit in regression fixtures.
- Long sessions do not leak memory excessively.

## Later Enhancements

- True realtime partial text insertion with final correction.
- Opus transport to reduce bandwidth.
- Hotkey-triggered Mac-side recording mode.
- More text profiles.
- English and mixed-language model profiles.
- Custom vocabulary and hotwords.
- Local transcript history with explicit opt-in.
- Alternative ASR providers behind the same interface.
- Alternative LLM providers behind the same interface.

## Highest-Risk Work Items

1. macOS packaging of FunASR C++/ONNX Runtime and `libonnxruntime.dylib`.
2. ONNX model availability and compatibility with the selected runtime.
3. Latency of 2-pass ASR on the target Mac.
4. iOS local network permission timing and discoverability.
5. macOS Accessibility permission onboarding.
6. Clipboard restoration edge cases.
7. Ollama prompt discipline for conservative cleanup.

## Suggested First Sprint

Focus only on the native runtime and local Mac loop:

1. Build FunASR C++/ONNX Runtime.
2. Run a WAV file through ONNX ASR.
3. Create Go `ASRProvider` against the runtime.
4. Call Ollama cleanup from Go.
5. Paste cleaned text into the focused app.

This produces a Mac-only proof of the core value before adding iOS discovery, pairing, and streaming.
