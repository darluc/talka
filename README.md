# Talka

Talka is a local-first voice input system. An iOS device acts as a remote microphone, streams speech to a paired Mac, and the macOS app transcribes, cleans up, and inserts the final text into the active input field.

## Current Product Direction

- iOS stays focused on connection, pairing, voice capture, and a minimal Return-key shortcut.
- macOS owns service status, pairing PIN, AI/ASR configuration, connected-device visibility, diagnostics, and text insertion.
- ASR uses the app-bundled sherpa-onnx streaming recognizer, defaulting to the bilingual zh-en Paraformer model.
- AI cleanup defaults to a configurable Ollama/OpenAI-compatible endpoint.
- Text insertion uses clipboard write plus a macOS Accessibility-driven Cmd+V, guarded by a native preflight check. The same secure path can send a direct Return key press with optional Cmd, Alt, and Shift modifiers.

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

Shortcut flow:

1. iOS sends an encrypted `key_press` message for `enter`.
2. The message bypasses ASR and LLM processing.
3. Go forwards the key request to the Swift broker.
4. The broker posts Enter through CoreGraphics, including any selected Cmd, Alt, or Shift modifiers.

## Documentation

- Product behavior and UX: [docs/product-design.md](docs/product-design.md)
- Runtime and transport architecture: [docs/technical-architecture.md](docs/technical-architecture.md)
- Engineering milestones: [docs/development-plan.md](docs/development-plan.md)

## Release Builds

Pushing a tag that starts with `v` triggers the GitHub release workflow:

```sh
git tag v0.1.0
git push origin v0.1.0
```

The workflow builds and publishes these release artifacts:

- `TalkaMac-macOS-arm64.zip`
- `TalkaMac-macOS-x86_64.zip`
- `TalkaIOS-iOS-simulator.zip`
- `TalkaIOS-iOS-device-signed.ipa`

The release workflow expects Apple signing material in GitHub Actions secrets. macOS artifacts are Developer ID signed, notarized, stapled, and zipped. The iOS device artifact is exported as a signed IPA using an ad-hoc provisioning profile.

Required repository secrets:

- `APPLE_TEAM_ID`: Apple Developer Team ID.
- `BUILD_KEYCHAIN_PASSWORD`: random password for the temporary CI keychain.
- `MACOS_DEVELOPER_ID_CERT_BASE64`: base64-encoded Developer ID Application `.p12`.
- `MACOS_DEVELOPER_ID_CERT_PASSWORD`: password for the Developer ID `.p12`.
- `APP_STORE_CONNECT_KEY_ID`: App Store Connect API key ID for notarization.
- `APP_STORE_CONNECT_ISSUER_ID`: App Store Connect issuer ID for notarization.
- `APP_STORE_CONNECT_API_KEY_BASE64`: base64-encoded App Store Connect `.p8`.
- `IOS_DISTRIBUTION_CERT_BASE64`: base64-encoded Apple Distribution `.p12`.
- `IOS_DISTRIBUTION_CERT_PASSWORD`: password for the iOS distribution `.p12`.
- `IOS_PROVISIONING_PROFILE_BASE64`: base64-encoded `dev.talka.ios` provisioning profile.
- `IOS_PROVISIONING_PROFILE_NAME`: provisioning profile name, not the filename.

On macOS, encode local signing files with:

```sh
base64 -i DeveloperIDApplication.p12 | pbcopy
base64 -i AppleDistribution.p12 | pbcopy
base64 -i Talka_AdHoc.mobileprovision | pbcopy
base64 -i AuthKey_XXXXXXXXXX.p8 | pbcopy
```

## Development Verification

Common checks:

```sh
go test ./...
xcodebuild test -quiet -project apps/macos/TalkaMac/TalkaMac.xcodeproj -scheme TalkaMac
```

If macOS Settings shows Accessibility enabled but Talka still reports `Accessibility Required`, remove the old TalkaMac entry from System Settings > Privacy & Security > Accessibility, then add or enable the currently installed app again. Ad-hoc signed builds can change code-signing identity between packages, and macOS TCC can treat them as different authorization subjects.
