# Talka

Talka is a local-first voice input system. An iOS device acts as a remote microphone, streams speech to a paired Mac, and the macOS app transcribes, cleans up, and inserts the final text into the active input field.

## Current Product Direction

- iOS stays focused on connection, pairing, and voice capture.
- macOS owns service status, pairing PIN, AI/ASR configuration, connected-device visibility, diagnostics, and text insertion.
- ASR uses the app-bundled sherpa-onnx streaming recognizer, defaulting to the bilingual zh-en Paraformer model.
- AI cleanup defaults to a configurable Ollama/OpenAI-compatible endpoint.
- Text insertion uses clipboard write plus a macOS Accessibility-driven Cmd+V, guarded by a native preflight check.

## macOS Settings Shape

The current settings screen is intentionally compact:

- Top status area combines service state, Accessibility state, pairing PIN, and PIN countdown.
- The `Service listening` pill can trigger service recovery or test the service path.
- The Accessibility pill reflects native `AXIsProcessTrusted()` status and opens the permission flow.
- `Interfaces` contains AI endpoint, AI model, timeout, and ONNX model selection.
- ASR is ONNX-only in the product surface; legacy ASR provider names are migrated to ONNX config when the app refreshes its bundled runtime paths.
- AI API and ASR API health are visible inline.
- Connected devices appear at the bottom with device name and connection time.
- Footer actions are limited to Diagnostics, Reset Changes, and Save.

The menu bar menu only exposes:

- Settings
- Quit

The tray icon shows a green status dot only when the service, AI API, ASR API, and native Accessibility permission are all healthy.

## Text Insertion Contract

The macOS app starts a local Unix domain socket paste broker. The Go service talks to that broker instead of trying to synthesize the paste inside the background service process.

Insertion flow:

1. Go asks the Swift broker for a `preflight`.
2. The broker checks `AXIsProcessTrusted()` in the signed app process.
3. If Accessibility is missing, Go returns `accessibility_missing` without changing the clipboard.
4. If preflight succeeds, Go writes the final text to the clipboard.
5. Go asks the broker to `paste`.
6. The broker posts Cmd+V through CoreGraphics.
7. Go restores the previous clipboard when configured to do so.

This avoids the old half-success state where recognized text reached the clipboard but could not be pasted.

## Documentation

- Product behavior and UX: [docs/product-design.md](docs/product-design.md)
- Runtime and transport architecture: [docs/technical-architecture.md](docs/technical-architecture.md)
- Engineering milestones: [docs/development-plan.md](docs/development-plan.md)

## Development Verification

Common checks:

```sh
go test ./...
xcodebuild test -quiet -project apps/macos/TalkaMac/TalkaMac.xcodeproj -scheme TalkaMac
```

If macOS Settings shows Accessibility enabled but Talka still reports `Accessibility Required`, remove the old TalkaMac entry from System Settings > Privacy & Security > Accessibility, then add or enable the currently installed app again. Ad-hoc signed builds can change code-signing identity between packages, and macOS TCC can treat them as different authorization subjects.
