# Talka Product Design

## Overview

Talka is a local-first voice input toolchain. An iOS app captures speech and streams audio to a macOS service on the same local network. The macOS service transcribes speech locally, refines the resulting text with a local LLM, and inserts the final text into the currently focused macOS input field.

The product is designed for fast personal dictation, short-form writing, chat replies, notes, and coding-adjacent text entry. Privacy and local control are core requirements: audio stays inside the user's local network, ASR runs locally through the bundled sherpa-onnx streaming recognizer, and text post-processing defaults to the user's local Ollama service.

## Goals

- Provide a simple iOS voice capture interface with clear recording feedback.
- Pair iOS and macOS devices securely with a one-time PIN.
- Stream audio from iOS to macOS over the local network.
- Run local Chinese/English ASR using the bundled sherpa-onnx bilingual zh-en Paraformer model by default.
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

The macOS app should run quietly in the menu bar, because the main product value happens in other apps. The settings surface should feel like a compact macOS control panel, not an advanced developer console.

Primary UI:

- Menu bar status item with a green dot only when service, AI, ASR, and native Accessibility permission are all healthy.
- Menu bar menu with only `Settings` and `Quit`.
- Settings window for the daily control surface.
- Separate diagnostics window for failure evidence and recovery actions.
- PIN pairing display integrated into the top status area.
- Connected-device list at the bottom of settings.

Settings layout:

- Top status card combines:
  - Service state.
  - Native Accessibility state.
  - Current six-digit PIN.
  - PIN refresh countdown.
- The Ready headline should be visually secondary to the useful state controls.
- The PIN countdown sits below or next to the PIN without causing layout jitter when seconds update.
- `Service listening` is actionable and can be used to test or recover the service path.
- `Accessibility OK` / `Accessibility Required` is actionable and opens the permission flow.
- `Interfaces` contains only settings the user can reasonably change:
  - AI endpoint.
  - AI model.
  - AI timeout.
  - ASR mode.
- AI API and ASR API health are shown inline in the interfaces section.
- Connected devices show device name plus connection time. They do not show low-value actions such as `Forget` in the main compact view.
- Footer contains Diagnostics, Reset Changes, Save, last update time, and config path.

Configuration:

- AI endpoint: configurable Ollama/OpenAI-compatible endpoint.
- AI model.
- AI timeout.
- ASR mode: `FunASR` or `ONNX`.
- Paired or connected device list.

ASR mode is a product-level setting. It must not expose FunASR's internal runtime mode such as `2pass` as the user-facing ASR mode. `FunASR` means Talka launches the bundled FunASR runtime and bundled models with streaming audio. `ONNX` means Talka uses the in-process sherpa-onnx streaming recognizer.

Diagnostics layout:

- Diagnostics should not duplicate the settings page.
- It should answer: what is broken, where is it running from, and what can the user do next?
- It should include:
  - Overall readiness.
  - Service, ASR runtime, AI API, and Accessibility status cards.
  - Failure evidence from the control API and runtime.
  - Runtime evidence such as config path, control API address, ASR runtime path, model path, and last refresh.
  - Recovery actions including refresh, opening Accessibility settings, and copying recoverable text when paste fails.

### Focused-App Text Entry

For the first version, text insertion uses clipboard paste guarded by a native macOS Accessibility preflight:

1. Ask the Swift app's local paste broker to run a preflight check.
2. The broker checks `AXIsProcessTrusted()` in the app process.
3. If Accessibility is missing, return a recoverable `accessibility_missing` error before changing the clipboard.
4. Save the current clipboard content.
5. Write the final Talka text to the clipboard.
6. Ask the broker to send Cmd+V through CoreGraphics.
7. Restore the previous clipboard after a short delay when restoration is enabled and the user has not overwritten it.

This approach is more compatible than synthesizing every character as keyboard events. Direct key event insertion can be added later as a fallback or advanced mode.

The key product requirement is to avoid half-success. Talka must not silently leave the final text in the clipboard when Accessibility prevents paste into the target app. When paste cannot proceed, the user should see a clear recovery state and the recognized text should remain recoverable.

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
- ASR runs locally through either the app-bundled FunASR runtime or the in-process ONNX recognizer.
- LLM post-processing defaults to local Ollama.
- No telemetry is required for MVP.
- Debug logs must not store raw audio or full transcripts unless the user explicitly enables diagnostic capture.

## Key Product Risks

- Embedded ASR runtime packaging on macOS may be the hardest engineering part.
- macOS Accessibility permission may confuse users unless onboarding is clear.
- Ad-hoc signed macOS builds can lose TCC Accessibility trust after repackaging; users may need to remove the old app entry and grant permission to the current installed app.
- Local network permission on iOS must be requested at the right moment.
- Real-time ASR quality depends on chunking and VAD behavior.
- Clipboard-based insertion can briefly replace the user's clipboard if restoration fails, so preflight and recovery behavior are critical.
- Ollama text optimization can over-edit unless prompts and tests are strict.

## MVP Acceptance Criteria

- iOS can discover and pair with macOS on the same network.
- PIN pairing blocks unauthorized devices.
- Audio can stream from iOS to macOS.
- The embedded FunASR runtime can produce streaming Chinese transcription from live speech.
- The macOS app can switch between FunASR and ONNX ASR without changing the pairing flow.
- Ollama can clean the recognized text.
- macOS can paste the final text into common apps such as Notes, Safari text fields, WeChat, and VS Code.
- The user can configure Ollama URL and model.
- The user can configure AI endpoint, AI model, AI timeout, ASR mode, and external ASR host/port from the macOS settings window.
- ASR host and port are disabled while ASR mode is embedded.
- Settings and tray status reflect native Accessibility permission, not only the Go service's control API.
- Missing Accessibility permission is detected before clipboard mutation.
- The system can restart and reconnect to a previously paired device.
