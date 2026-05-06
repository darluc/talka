# Talka Product Design

## Overview

Talka is a local-first voice input toolchain. An iOS app captures speech and streams audio to a macOS service on the same local network. The macOS service transcribes speech locally, refines the resulting text with a local LLM, and inserts the final text into the currently focused macOS input field.

The product is designed for fast personal dictation, short-form writing, chat replies, notes, and coding-adjacent text entry. Privacy and local control are core requirements: audio stays inside the user's local network, ASR runs locally through an embedded FunASR C++/ONNX Runtime by default, and text post-processing defaults to the user's local Ollama service. Advanced users can switch the ASR backend to an external FunASR runtime or a legacy Talka sidecar when needed.

## Goals

- Provide a simple iOS voice capture interface with clear recording feedback.
- Pair iOS and macOS devices securely with a one-time PIN.
- Stream audio from iOS to macOS over the local network.
- Run local Chinese-first ASR using an embedded FunASR C++/ONNX Runtime by default.
- Use Ollama for punctuation, sentence cleanup, style-preserving correction, and final text polishing.
- Insert generated text into the active macOS application with minimal user friction.
- Keep the system extensible so embedded ASR, external ASR, LLM, transport, and text-injection methods can evolve independently.

## Non-Goals For The First Version

- Building a full iOS custom keyboard extension.
- Cloud synchronization between devices.
- Multi-user account management.
- Public internet access to the macOS server.
- Fully custom cryptographic primitives.
- True character-by-character live insertion into the focused field.
- Supporting every language equally in the first release.

## Target Workflow

1. The user opens the Talka iOS app.
2. The app discovers nearby Talka macOS services through Bonjour/mDNS.
3. On first connection, the macOS app displays a short PIN.
4. The user enters the PIN on iOS.
5. The devices establish an encrypted session and remember the pairing.
6. The user taps or holds the microphone button on iOS.
7. iOS streams microphone audio to the macOS service.
8. macOS sends the audio to the selected ASR backend, using the embedded FunASR runtime by default.
9. ASR results are accumulated as partial and final segments.
10. When recording ends, macOS sends the recognized text to Ollama for final cleanup.
11. The final text is inserted into the currently focused macOS input field.

## Product Surfaces

### iOS App

The iOS app should feel like a remote microphone rather than a full writing app.

Primary UI:

- Microphone button.
- Connection status.
- Current Mac device name.
- Audio level or waveform animation.
- Recording state.
- Basic error states for missing microphone permission, missing local network permission, lost connection, and failed pairing.

User actions:

- Discover Mac.
- Pair with PIN.
- Start recording.
- Stop recording.
- Cancel current recording.
- Reconnect to a known Mac.

The MVP can use a single-screen interface. Settings can remain minimal: selected Mac, reconnect, forget device.

### macOS App

The macOS app should run quietly in the menu bar, because the main product value happens in other apps.

Primary UI:

- Menu bar status item.
- Service status: listening, paired, recording, transcribing, inserting text.
- PIN pairing window.
- Configuration window.
- Permission helper for Accessibility access.
- Diagnostics view for local runtime status.

Configuration:

- ASR provider selection: embedded runtime, external FunASR runtime, or legacy Talka sidecar.
- ASR runtime path.
- ASR model paths.
- Ollama base URL.
- Ollama model.
- Text insertion mode.
- Paired device list.
- Debug logging toggle.

### Focused-App Text Entry

For the first version, text insertion should use clipboard paste:

1. Save the current clipboard content.
2. Write the final Talka text to the clipboard.
3. Send Cmd+V through macOS Accessibility APIs.
4. Restore the previous clipboard after a short delay.

This approach is more compatible than synthesizing every character as keyboard events. Direct key event insertion can be added later as a fallback or advanced mode.

## Audio Interaction Model

The first release should prefer stable phrase-level dictation over aggressive live typing.

MVP behavior:

- Show partial ASR text in the macOS status window or iOS debug state if useful.
- Do not insert partial text into the focused field.
- Insert only the final optimized text after the user stops recording.

Later behavior:

- Insert finalized segments progressively.
- Mark tentative text separately.
- Replace tentative text after final optimization.

## Text Processing Behavior

The final text optimizer must be conservative. It should:

- Preserve the user's original meaning.
- Add punctuation and sentence boundaries.
- Remove obvious filler sounds when appropriate.
- Fix repeated fragments caused by ASR overlap.
- Normalize Chinese and English spacing.
- Avoid adding facts or expanding content.
- Avoid changing tone unless explicitly configured.

The optimizer should support profiles later:

- Plain dictation.
- Chat message.
- Formal writing.
- Developer note.
- Meeting note.

MVP should ship with one default profile: "clean dictation".

## Pairing Experience

Pairing should be deliberate and easy to understand.

Flow:

1. The iOS app sees a Mac named, for example, "Darluc's MacBook Pro".
2. The user taps it.
3. The Mac displays a six-digit PIN in a small pairing window.
4. The user enters that PIN on iOS.
5. Both sides complete an authenticated key exchange.
6. The Mac shows the iOS device as paired.

PIN properties:

- Six digits.
- Expires after two minutes.
- Regenerated after failed attempts or timeout.
- Rate limited after repeated failures.

## Privacy And Trust

Talka is local-first.

- iOS sends audio only to paired Macs.
- macOS accepts audio only from paired devices after encrypted session setup.
- ASR runs locally through the app-bundled FunASR runtime by default.
- Advanced deployments can point at a separately managed local FunASR runtime without changing the iOS client flow.
- LLM post-processing defaults to local Ollama.
- No telemetry is required for MVP.
- Debug logs must not store raw audio or full transcripts unless the user explicitly enables diagnostic capture.

## Key Product Risks

- Embedded ASR runtime packaging on macOS may be the hardest engineering part.
- macOS Accessibility permission may confuse users unless onboarding is clear.
- Local network permission on iOS must be requested at the right moment.
- Real-time ASR quality depends on chunking and VAD behavior.
- Clipboard-based insertion can briefly replace the user's clipboard if restoration fails.
- Ollama text optimization can over-edit unless prompts and tests are strict.

## MVP Acceptance Criteria

- iOS can discover and pair with macOS on the same network.
- PIN pairing blocks unauthorized devices.
- Audio can stream from iOS to macOS.
- The embedded FunASR runtime can produce Chinese transcription from live speech.
- The macOS app can also be reconfigured to use an external FunASR runtime or legacy sidecar without changing the pairing flow.
- Ollama can clean the recognized text.
- macOS can paste the final text into common apps such as Notes, Safari text fields, WeChat, and VS Code.
- The user can configure Ollama URL and model.
- The system can restart and reconnect to a previously paired device.
