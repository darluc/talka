import Foundation
import XCTest
@testable import TalkaMac

@MainActor
final class TalkaMacTests: XCTestCase {
    override func tearDown() {
        StubURLProtocol.reset()
        unsetenv("TALKA_CONFIG_PATH")
        unsetenv("TALKA_RESOURCES_PATH")
        super.tearDown()
    }

    func testRefreshMapsControlAPIStatesToDisplayStates() async {
        let scenarios: [(rawState: String, expected: ServiceDisplayState)] = [
            ("running", .listening),
            ("listening", .listening),
            ("paired", .paired),
            ("recording", .recording),
            ("transcribing", .transcribing),
            ("inserting", .inserting),
            ("error", .error),
        ]

        for scenario in scenarios {
            let client = FakeControlAPIClient(
                statusResults: [.success(.fixture(state: scenario.rawState))],
                devicesResults: [.success([])],
                configResults: [.success(.fixture())]
            )
            let viewModel = AppShellViewModel(client: client)

            await viewModel.refresh()

            XCTAssertEqual(viewModel.serviceDisplayState, scenario.expected, "raw state \(scenario.rawState)")
            XCTAssertTrue(viewModel.statusMessage.contains(viewModel.serviceDisplayState.label))
        }
    }

    func testRefreshLoadsLatencyDiagnostics() async throws {
        let trace = ControlLatencyTrace(
            traceID: "trace-1",
            deviceID: "iphone-1",
            acceptedAt: Date(timeIntervalSince1970: 0),
            audioStopReceivedAt: nil,
            completedAt: nil,
            insertCompletedAt: nil,
            bufferedMessages: 8,
            frames: 6,
            audioMSEstimate: 120,
            decryptDecodeMS: 3,
            asrMS: 220,
            llmMS: 330,
            responseWriteMS: 4,
            insertMS: 5,
            totalAfterStopMS: 557,
            rawTranscriptChars: 6,
            finalTextChars: 6,
            errorStage: nil,
            error: nil
        )
        let client = FakeControlAPIClient(diagnosticsResults: [.success(ControlDiagnostics(latencyTraces: [trace]))])
        let viewModel = AppShellViewModel(client: client)

        await viewModel.refresh()
        try await Task.sleep(nanoseconds: 50_000_000)

        XCTAssertEqual(viewModel.latencyTraces.first?.traceID, "trace-1")
        XCTAssertEqual(viewModel.latestLatencyTrace?.totalAfterStopMS, 557)
    }

    func testASRModeOptionsExposeOnlyFunASRAndONNX() {
        XCTAssertEqual(ASRModeOption.allCases.map(\.rawValue), ["funasr", "onnx"])
        XCTAssertEqual(ASRModeOption.allCases.map(\.title), ["FunASR", "ONNX"])
    }

    func testASRModeNormalizesLegacyProviders() {
        XCTAssertEqual(ASRModeOption.normalizedProvider("sherpa_onnx_streaming"), "onnx")
        XCTAssertEqual(ASRModeOption.normalizedProvider("sherpa"), "onnx")
        XCTAssertEqual(ASRModeOption.normalizedProvider("funasr_embedded"), "funasr")
        XCTAssertEqual(ASRModeOption.normalizedProvider("funasr_external"), "funasr")
        XCTAssertEqual(ASRModeOption.normalizedProvider("sidecar"), "funasr")
    }

    func testControlASRConfigDecodesLegacyProviderAsSupportedMode() throws {
        let decoder = JSONDecoder()
        let body = #"""
        {
          "provider": "sherpa_onnx_streaming",
          "runtime_path": "talka-asr-runtime",
          "host": "127.0.0.1",
          "port": 10095,
          "mode": "streaming",
          "sample_rate": 16000,
          "startup_timeout_seconds": 180,
          "container_image": "",
          "container_name": "",
          "download_dir": "",
          "hotword_path": "",
          "models": {
            "asr": "models/funasr/paraformer-zh-onnx",
            "online": "models/funasr/paraformer-zh-online-onnx",
            "vad": "models/funasr/fsmn-vad-onnx",
            "punc": "models/funasr/ct-punc-onnx",
            "itn": "models/funasr/itn-zh",
            "lm": ""
          }
        }
        """#.data(using: .utf8)!

        let config = try decoder.decode(ControlASRConfig.self, from: body)

        XCTAssertEqual(config.provider, "onnx")
    }

    func testRefreshShowsServiceUnavailableAndRecoveryCanRecover() async {
        let client = FakeControlAPIClient(
            statusResults: [
                .failure(ControlAPIClientError.serviceUnavailable),
                .success(.fixture(state: "listening")),
            ],
            devicesResults: [.success([])],
            configResults: [.success(.fixture())]
        )
        let viewModel = AppShellViewModel(client: client)

        await viewModel.refresh()

        XCTAssertEqual(viewModel.serviceDisplayState, .unavailable)
        XCTAssertEqual(viewModel.recoveryActionTitle, "Check Local Service")
        XCTAssertTrue(viewModel.statusMessage.contains("unavailable"))

        await viewModel.recoverService()

        XCTAssertEqual(viewModel.serviceDisplayState, .listening)
        XCTAssertNil(viewModel.recoveryActionTitle)
    }

    func testRefreshUsesNativeAccessibilityStatus() async {
        let checker = FakeAccessibilityPermissionChecker(statuses: [.missing])
        let viewModel = AppShellViewModel(
            client: FakeControlAPIClient(
                statusResults: [.success(.fixture(permissions: ControlPermissionsStatus(accessibility: "unknown")))]
            ),
            accessibilityChecker: checker
        )

        await viewModel.refresh()

        XCTAssertEqual(viewModel.accessibilityStatus, .missing)
        XCTAssertEqual(viewModel.accessibilityButtonTitle, "Accessibility Required")
        XCTAssertFalse(viewModel.isEverythingHealthy)
    }

    func testRequestAccessibilityGuidancePromptsThroughNativeChecker() async {
        let checker = FakeAccessibilityPermissionChecker(statuses: [.missing], requestStatus: .granted)
        let client = FakeControlAPIClient()
        let viewModel = AppShellViewModel(client: client, accessibilityChecker: checker)

        await viewModel.requestAccessibilityGuidance()

        XCTAssertEqual(checker.requestAccessCalls, 1)
        XCTAssertEqual(client.accessibilityOpenCalls, 1)
        XCTAssertEqual(viewModel.accessibilityStatus, .granted)
    }

    func testRefreshMapsAccessibilityRecoveryFromControlError() async {
        let client = FakeControlAPIClient(
            statusResults: [
                .failure(ControlAPIClientError.invalidResponse(statusCode: 409, error: ControlServiceError(
                    code: "accessibility_missing",
                    message: "Accessibility permission is required before Talka can paste.",
                    details: [
                        "user_message": "Open Accessibility guidance and try again.",
                        "recovery_action": "open_accessibility_guidance",
                        "failed_text": "Talka 测试文本。"
                    ]
                )))
            ]
        )
        let viewModel = AppShellViewModel(client: client)

        await viewModel.refresh()

        XCTAssertEqual(viewModel.serviceDisplayState, .error)
        XCTAssertEqual(viewModel.recoveryActionTitle, "Open Accessibility Guidance")
        XCTAssertEqual(viewModel.injectionRecovery?.diagnosticCode, "accessibility_missing")
        XCTAssertEqual(viewModel.injectionRecovery?.failedText, "Talka 测试文本。")
        XCTAssertEqual(viewModel.lastErrorMessage, "Open Accessibility guidance and try again.")
    }

    func testRecoveryActionCopiesFailedTextForPasteFailure() async {
        let copier = FakeRecoveryTextCopier()
        let viewModel = AppShellViewModel(
            client: FakeControlAPIClient(),
            textCopier: copier,
            nowProvider: Date.init
        )
        viewModel.setInjectionRecoveryForTesting(
            InjectionRecovery(
                diagnosticCode: "paste_failed",
                message: "Talka could not paste the final text into the active app.",
                failedText: "Talka 测试文本。",
                action: .copyFailedText
            )
        )

        await viewModel.performRecoveryAction()

        XCTAssertEqual(copier.copiedText, "Talka 测试文本。")
        XCTAssertEqual(viewModel.recoveryActionTitle, "Copy Failed Text")
    }

    func testStartPairingShowsServerPINAndExpiryCountdown() async throws {
        let now = ControlledNow(Date(timeIntervalSince1970: 1_700_000_000))
        let expiry = now.current.addingTimeInterval(120)
        let client = FakeControlAPIClient(
            statusResults: [.success(.fixture(state: "paired", pairingActive: true))],
            devicesResults: [.success([.fixture()])],
            configResults: [.success(.fixture())],
            pairingResults: [.success(ControlPairingSession(pairingID: "pairing-123", pin: "123456", expiresAt: expiry, expiresInSeconds: 120))]
        )
        let viewModel = AppShellViewModel(client: client, nowProvider: { now.current })

        await viewModel.startPairing()

        let pairing = try XCTUnwrap(viewModel.pairing)
        XCTAssertEqual(pairing.pin, "123456")
        XCTAssertEqual(pairing.secondsRemaining, 120)
        XCTAssertEqual(pairing.expiryText, "02:00")

        now.current = now.current.addingTimeInterval(45)
        pairing.refreshCountdown()

        XCTAssertEqual(pairing.secondsRemaining, 75)
        XCTAssertEqual(pairing.expiryText, "01:15")
    }

    func testEnsurePairingActiveStartsPINAndRefreshesAfterExpiry() async throws {
        let now = ControlledNow(Date(timeIntervalSince1970: 1_700_000_000))
        let firstExpiry = now.current.addingTimeInterval(120)
        let secondExpiry = now.current.addingTimeInterval(300)
        let client = FakeControlAPIClient(
            statusResults: [
                .success(.fixture(state: "running", pairingActive: true)),
                .success(.fixture(state: "running", pairingActive: true)),
            ],
            devicesResults: [
                .success([]),
                .success([]),
            ],
            configResults: [
                .success(.fixture()),
                .success(.fixture()),
            ],
            pairingResults: [
                .success(ControlPairingSession(pairingID: "pairing-1", pin: "123456", expiresAt: firstExpiry, expiresInSeconds: 120)),
                .success(ControlPairingSession(pairingID: "pairing-2", pin: "654321", expiresAt: secondExpiry, expiresInSeconds: 300)),
            ]
        )
        let viewModel = AppShellViewModel(client: client, nowProvider: { now.current })

        await viewModel.ensurePairingActive()

        XCTAssertEqual(viewModel.pairing?.pin, "123456")

        now.current = firstExpiry.addingTimeInterval(1)
        await viewModel.ensurePairingActive()

        XCTAssertEqual(viewModel.pairing?.pin, "654321")
    }

    func testRefreshRuntimeStateUpdatesDevicesWithoutReloadingConfig() async throws {
        let client = FakeControlAPIClient(
            statusResults: [
                .success(.fixture(state: "running")),
                .success(.fixture(state: "running")),
            ],
            devicesResults: [
                .success([]),
                .success([ControlDevice(id: "device-2", name: "Darluc's iPhone", paired: true, lastSeenAt: Date(timeIntervalSince1970: 1_700_000_300))]),
            ],
            configResults: [.success(.fixture())]
        )
        let viewModel = AppShellViewModel(client: client)

        await viewModel.refresh()
        viewModel.config.llm.baseURL = "http://editing.local:11434"

        await viewModel.refreshRuntimeState()

        XCTAssertEqual(viewModel.devices.map(\.name), ["Darluc's iPhone"])
        XCTAssertEqual(viewModel.serviceDisplayState, .paired)
        XCTAssertEqual(viewModel.config.llm.baseURL, "http://editing.local:11434")
        XCTAssertEqual(client.fetchConfigCalls, 1)
    }

    func testTickPairingCountdownPublishesRemainingTime() async throws {
        let now = ControlledNow(Date(timeIntervalSince1970: 1_700_000_000))
        let client = FakeControlAPIClient(
            statusResults: [.success(.fixture(state: "running"))],
            devicesResults: [.success([])],
            configResults: [.success(.fixture())],
            pairingResults: [.success(ControlPairingSession(pairingID: "pairing-1", pin: "123456", expiresAt: now.current.addingTimeInterval(90), expiresInSeconds: 90))]
        )
        let viewModel = AppShellViewModel(client: client, nowProvider: { now.current })

        await viewModel.ensurePairingActive()
        XCTAssertEqual(viewModel.pairingExpiryText, "01:30")

        now.current = now.current.addingTimeInterval(15)
        await viewModel.tickPairingCountdown()

        XCTAssertEqual(viewModel.pairingExpiryText, "01:15")
    }

    func testOverallHealthRequiresServiceAIAndASRReady() async {
        let healthy = ControlStatus.fixture(
            state: "running",
            asr: ControlASRStatus(provider: "funasr", runtimePath: "", sampleRate: 16000, mode: "2pass", ready: true, error: nil),
            ollama: ControlOllamaStatus(baseURL: "http://localhost:11434", model: "qwen3:8b", timeoutSeconds: 30, ready: true, error: nil)
        )
        let unhealthyAI = ControlStatus.fixture(
            state: "running",
            asr: healthy.asr,
            ollama: ControlOllamaStatus(baseURL: "http://localhost:11434", model: "qwen3:8b", timeoutSeconds: 30, ready: false, error: "offline")
        )
        let client = FakeControlAPIClient(
            statusResults: [.success(healthy), .success(unhealthyAI)],
            devicesResults: [.success([]), .success([])],
            configResults: [.success(.fixture())]
        )
        let viewModel = AppShellViewModel(
            client: client,
            accessibilityChecker: FakeAccessibilityPermissionChecker(statuses: [.granted, .granted, .granted])
        )

        await viewModel.refresh()
        XCTAssertTrue(viewModel.isEverythingHealthy)

        await viewModel.refreshRuntimeState()
        XCTAssertFalse(viewModel.isEverythingHealthy)
    }

    func testLiveControlAPIClientStartPairingDecodesFractionalSecondExpiry() async throws {
        let responseBody = #"""
        {
          "pairing_id": "pairing-123",
          "pin": "123456",
          "expires_at": "2026-04-30T11:21:34.390947+08:00",
          "expires_in_seconds": 120
        }
        """#

        let client = makeLiveClient { request in
            XCTAssertEqual(request.httpMethod, "POST")
            XCTAssertEqual(request.url?.path, "/v1/pairing/start")
            return try Self.httpResponse(body: responseBody)
        }

        let session = try await client.startPairing()

        XCTAssertEqual(session.pairingID, "pairing-123")
        XCTAssertEqual(session.pin, "123456")
        XCTAssertEqual(session.expiresInSeconds, 120)
        XCTAssertEqual(session.expiresAt.timeIntervalSince1970, 1_777_519_294.390_947, accuracy: 0.001)
    }

    func testLiveControlAPIClientFetchStatusDecodesRuntimeHealth() async throws {
        let responseBody = #"""
        {
          "service_name": "Talka",
          "state": "running",
          "config_path": "/tmp/talka.yaml",
          "uptime_seconds": 12,
          "device_count": 1,
          "pairing_active": true,
          "asr": {
            "provider": "funasr_external",
            "runtime_path": "",
            "sample_rate": 16000,
            "mode": "2pass",
            "ready": true
          },
          "ollama": {
            "base_url": "http://localhost:11434",
            "model": "qwen3:8b",
            "timeout_seconds": 30,
            "ready": false,
            "error": "ollama unavailable"
          },
          "permissions": {
            "accessibility": "unknown"
          }
        }
        """#

        let client = makeLiveClient { request in
            XCTAssertEqual(request.httpMethod, "GET")
            XCTAssertEqual(request.url?.path, "/v1/status")
            return try Self.httpResponse(body: responseBody)
        }

        let status = try await client.fetchStatus()

        XCTAssertEqual(status.asr?.provider, "funasr_external")
        XCTAssertEqual(status.asr?.ready, true)
        XCTAssertEqual(status.ollama?.baseURL, "http://localhost:11434")
        XCTAssertEqual(status.ollama?.ready, false)
        XCTAssertEqual(status.ollama?.error, "ollama unavailable")
        XCTAssertEqual(status.permissions?.accessibility, "unknown")
    }

    func testLiveControlAPIClientFetchDevicesDecodesFractionalSecondLastSeenAt() async throws {
        let responseBody = #"""
        {
          "devices": [
            {
              "id": "device-123",
              "name": "MacBook Pro",
              "paired": true,
              "last_seen_at": "2026-04-30T13:13:38.272792+08:00"
            }
          ]
        }
        """#

        let client = makeLiveClient { request in
            XCTAssertEqual(request.httpMethod, "GET")
            XCTAssertEqual(request.url?.path, "/v1/devices")
            return try Self.httpResponse(body: responseBody)
        }

        let devices = try await client.fetchDevices()

        XCTAssertEqual(devices.count, 1)
        XCTAssertEqual(devices.first?.id, "device-123")
        XCTAssertEqual(devices.first?.name, "MacBook Pro")
        XCTAssertEqual(devices.first?.paired, true)
        let lastSeenAt = try XCTUnwrap(devices.first?.lastSeenAt)
        XCTAssertEqual(lastSeenAt.timeIntervalSince1970, 1_777_526_018.272_792, accuracy: 0.001)
    }

    func testLiveControlAPIClientFetchDiagnosticsDecodesFractionalSecondLatencyDates() async throws {
        let responseBody = #"""
        {
          "latency_traces": [
            {
              "trace_id": "trace-abc",
              "device_id": "iphone-1",
              "accepted_at": "2026-05-11T21:40:30.123456+08:00",
              "audio_stop_received_at": "2026-05-11T21:40:31.123456+08:00",
              "completed_at": "2026-05-11T21:40:31.623456+08:00",
              "insert_completed_at": "2026-05-11T21:40:31.723456+08:00",
              "buffered_messages": 8,
              "frames": 6,
              "audio_ms_estimate": 120,
              "decrypt_decode_ms": 3,
              "asr_ms": 220,
              "llm_ms": 330,
              "response_write_ms": 4,
              "insert_ms": 5,
              "total_after_stop_ms": 557,
              "raw_transcript_chars": 6,
              "final_text_chars": 6
            }
          ]
        }
        """#

        let client = makeLiveClient { request in
            XCTAssertEqual(request.httpMethod, "GET")
            XCTAssertEqual(request.url?.path, "/v1/diagnostics/export")
            return try Self.httpResponse(body: responseBody)
        }

        let diagnostics = try await client.fetchDiagnostics()

        let trace = try XCTUnwrap(diagnostics.latencyTraces.first)
        let completedAt = try XCTUnwrap(trace.completedAt)
        XCTAssertEqual(trace.traceID, "trace-abc")
        XCTAssertEqual(trace.acceptedAt.timeIntervalSince1970, 1_778_506_830.123_456, accuracy: 0.001)
        XCTAssertEqual(completedAt.timeIntervalSince1970, 1_778_506_831.623_456, accuracy: 0.001)
        XCTAssertEqual(trace.totalAfterStopMS, 557)
    }

    func testSaveConfigPersistsEditedSettingsThroughClient() async {
        let client = FakeControlAPIClient(configResults: [.success(.fixture())])
        let viewModel = AppShellViewModel(client: client)

        await viewModel.refresh()
        viewModel.config.llm.baseURL = "http://localhost:11434"
        viewModel.config.llm.model = "qwen3:8b"
        viewModel.config.asr.host = "192.168.1.10"
        viewModel.config.asr.port = 10096
        viewModel.config.asr.mode = "2pass"
        viewModel.config.logging.captureAudio = true
        await viewModel.saveConfig()

        XCTAssertEqual(client.savedConfig?.llm.baseURL, "http://localhost:11434")
        XCTAssertEqual(client.savedConfig?.llm.model, "qwen3:8b")
        XCTAssertEqual(client.savedConfig?.asr.host, "192.168.1.10")
        XCTAssertEqual(client.savedConfig?.asr.port, 10096)
        XCTAssertEqual(client.savedConfig?.asr.mode, "2pass")
        XCTAssertEqual(client.savedConfig?.logging.captureAudio, true)
        XCTAssertNil(viewModel.lastErrorMessage)
    }

    func testForgetDeviceRemovesTrustedDeviceFromViewModel() async {
        let device = ControlDevice.fixture()
        let client = FakeControlAPIClient(
            statusResults: [.success(.fixture(state: "running", pairingActive: true))],
            devicesResults: [.success([device])],
            configResults: [.success(.fixture())]
        )
        let viewModel = AppShellViewModel(client: client)

        await viewModel.refresh()
        await viewModel.forgetDevice(id: device.id)

        XCTAssertEqual(client.forgottenDeviceIDs, [device.id])
        XCTAssertTrue(viewModel.devices.isEmpty)
        XCTAssertEqual(viewModel.status?.deviceCount, 0)
    }

    func testLiveControlAPIClientFetchConfigDecodesSnakeCaseFields() async throws {
        let expected = ControlConfig.fixture()
        let responseBody = #"""
        {
          "path": "/tmp/talka.yaml",
          "config": {
            "server": {
              "bind_host": "127.0.0.1",
              "port": 8080,
              "service_name": "Talka"
            },
            "asr": {
              "provider": "funasr",
              "runtime_path": "talka-asr-runtime",
              "host": "127.0.0.1",
              "port": 10095,
              "mode": "2pass",
              "sample_rate": 16000,
              "startup_timeout_seconds": 180,
              "container_image": "",
              "container_name": "",
              "download_dir": "",
              "hotword_path": "",
              "models": {
                "asr": "models/funasr/paraformer-zh-onnx",
                "online": "models/funasr/paraformer-zh-online-onnx",
                "vad": "models/funasr/fsmn-vad-onnx",
                "punc": "models/funasr/ct-punc-onnx",
                "itn": "models/funasr/itn-zh",
                "lm": ""
              }
            },
            "llm": {
              "provider": "ollama",
              "base_url": "http://localhost:11434",
              "model": "qwen3:8b",
              "timeout_seconds": 30
            },
            "injection": {
              "mode": "clipboard_paste",
              "restore_clipboard": true
            },
            "logging": {
              "level": "info",
              "capture_audio": false,
              "capture_transcript": false
            }
          }
        }
        """#

        let client = makeLiveClient { request in
            XCTAssertEqual(request.httpMethod, "GET")
            XCTAssertEqual(request.url?.path, "/v1/config")
            return try Self.httpResponse(body: responseBody)
        }

        let config = try await client.fetchConfig()

        XCTAssertEqual(config, expected)
    }

    func testLiveControlAPIClientSaveConfigEncodesSnakeCaseFields() async throws {
        let expected = ControlConfig.fixture()
        let responseBody = #"""
        {
          "path": "/tmp/talka.yaml",
          "config": {
            "server": {
              "bind_host": "127.0.0.1",
              "port": 8080,
              "service_name": "Talka"
            },
            "asr": {
              "provider": "funasr",
              "runtime_path": "talka-asr-runtime",
              "host": "127.0.0.1",
              "port": 10095,
              "mode": "2pass",
              "sample_rate": 16000,
              "startup_timeout_seconds": 180,
              "container_image": "",
              "container_name": "",
              "download_dir": "",
              "hotword_path": "",
              "models": {
                "asr": "models/funasr/paraformer-zh-onnx",
                "online": "models/funasr/paraformer-zh-online-onnx",
                "vad": "models/funasr/fsmn-vad-onnx",
                "punc": "models/funasr/ct-punc-onnx",
                "itn": "models/funasr/itn-zh",
                "lm": ""
              }
            },
            "llm": {
              "provider": "ollama",
              "base_url": "http://localhost:11434",
              "model": "qwen3:8b",
              "timeout_seconds": 30
            },
            "injection": {
              "mode": "clipboard_paste",
              "restore_clipboard": true
            },
            "logging": {
              "level": "info",
              "capture_audio": false,
              "capture_transcript": false
            }
          }
        }
        """#

        let client = makeLiveClient { request in
            XCTAssertEqual(request.httpMethod, "PUT")
            XCTAssertEqual(request.url?.path, "/v1/config")
            return try Self.httpResponse(body: responseBody)
        }

        let saved = try await client.saveConfig(expected)
        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        let body = try XCTUnwrap(request.talkaHTTPBodyData())
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: Any])
        let server = try XCTUnwrap(object["server"] as? [String: Any])
        let asr = try XCTUnwrap(object["asr"] as? [String: Any])
        let llm = try XCTUnwrap(object["llm"] as? [String: Any])
        let injection = try XCTUnwrap(object["injection"] as? [String: Any])
        let logging = try XCTUnwrap(object["logging"] as? [String: Any])

        XCTAssertEqual(saved, expected)
        XCTAssertEqual(server["bind_host"] as? String, expected.server.bindHost)
        XCTAssertEqual(server["service_name"] as? String, expected.server.serviceName)
        XCTAssertNil(server["bindHost"])
        XCTAssertNil(server["serviceName"])
        XCTAssertEqual(asr["container_image"] as? String, expected.asr.containerImage)
        XCTAssertEqual(asr["container_name"] as? String, expected.asr.containerName)
        XCTAssertEqual(asr["download_dir"] as? String, expected.asr.downloadDir)
        XCTAssertEqual(asr["hotword_path"] as? String, expected.asr.hotwordPath)
        XCTAssertEqual(asr["sample_rate"] as? Int, expected.asr.sampleRate)
        XCTAssertEqual(asr["startup_timeout_seconds"] as? Int, expected.asr.startupTimeoutSeconds)
        XCTAssertNil(asr["containerImage"])
        XCTAssertNil(asr["containerName"])
        XCTAssertNil(asr["downloadDir"])
        XCTAssertNil(asr["hotwordPath"])
        XCTAssertNil(asr["sampleRate"])
        XCTAssertNil(asr["startupTimeoutSeconds"])
        XCTAssertEqual(llm["base_url"] as? String, expected.llm.baseURL)
        XCTAssertEqual(llm["timeout_seconds"] as? Int, expected.llm.timeoutSeconds)
        XCTAssertNil(llm["baseURL"])
        XCTAssertNil(llm["timeoutSeconds"])
        XCTAssertEqual(injection["restore_clipboard"] as? Bool, expected.injection.restoreClipboard)
        XCTAssertNil(injection["restoreClipboard"])
        XCTAssertEqual(logging["capture_audio"] as? Bool, expected.logging.captureAudio)
        XCTAssertEqual(logging["capture_transcript"] as? Bool, expected.logging.captureTranscript)
        XCTAssertNil(logging["captureAudio"])
        XCTAssertNil(logging["captureTranscript"])
    }

    func testPortReuseActionReplacesExistingTalkaServerWhenPasteBrokerIsMissing() throws {
        let response = try Self.httpResponse(
            url: URL(string: "http://127.0.0.1:8080/v1/status")!,
            body: #"{"service_name":"Talka","state":"running"}"#
        )

        let action = try XCTUnwrap(
            ServerProcessManager.portReuseAction(
                data: response.1,
                response: response.0,
                currentPasteBrokerSocketPath: "/tmp/talka-paste-current.sock"
            )
        )

        XCTAssertEqual(action, .replaceExistingServer)
    }

    func testPortReuseActionReplacesExistingTalkaServerWhenPasteBrokerDiffers() throws {
        let response = try Self.httpResponse(
            url: URL(string: "http://127.0.0.1:8080/v1/status")!,
            body: #"{"service_name":"Talka","state":"running","injection":{"paste_broker_socket":"/tmp/talka-paste-old.sock"}}"#
        )

        let action = try XCTUnwrap(
            ServerProcessManager.portReuseAction(
                data: response.1,
                response: response.0,
                currentPasteBrokerSocketPath: "/tmp/talka-paste-current.sock"
            )
        )

        XCTAssertEqual(action, .replaceExistingServer)
    }

    func testPortReuseActionReusesExistingTalkaServerWhenPasteBrokerMatches() throws {
        let response = try Self.httpResponse(
            url: URL(string: "http://127.0.0.1:8080/v1/status")!,
            body: #"{"service_name":"Talka","state":"running","injection":{"paste_broker_socket":"/tmp/talka-paste-current.sock"}}"#
        )

        let action = try XCTUnwrap(
            ServerProcessManager.portReuseAction(
                data: response.1,
                response: response.0,
                currentPasteBrokerSocketPath: "/tmp/talka-paste-current.sock"
            )
        )

        XCTAssertEqual(action, .reuseExistingServer)
    }

    func testPortReuseActionRejectsNonTalkaServer() throws {
        let response = try Self.httpResponse(
            url: URL(string: "http://127.0.0.1:8080/v1/status")!,
            body: #"{"service_name":"SomethingElse"}"#
        )

        XCTAssertNil(
            ServerProcessManager.portReuseAction(
                data: response.1,
                response: response.0
            )
        )
    }

    func testAppLifecycleDelegateTerminatesManagedServer() {
        final class TerminationProbe {
            var calls = 0
        }

        let probe = TerminationProbe()
        TalkaMacLifecycleDelegate.terminateHandler = {
            probe.calls += 1
        }
        defer {
            TalkaMacLifecycleDelegate.terminateHandler = nil
        }

        TalkaMacLifecycleDelegate().applicationWillTerminate(Notification(name: NSApplication.willTerminateNotification))

        XCTAssertEqual(probe.calls, 1)
    }

    func testRuntimeConfigGeneratorPreservesExistingConfig() throws {
        let tempDir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        let resourcesDir = tempDir.appendingPathComponent("resources", isDirectory: true)
        let configURL = tempDir.appendingPathComponent("config.yaml")
        try FileManager.default.createDirectory(at: resourcesDir, withIntermediateDirectories: true)
        setenv("TALKA_CONFIG_PATH", configURL.path, 1)
        setenv("TALKA_RESOURCES_PATH", resourcesDir.path, 1)

        let generator = EmbeddedRuntimeConfigGenerator()
        let firstURL = try generator.generateConfig()
        XCTAssertEqual(firstURL, configURL)

        let externalConfig = """
        server:
          bind_host: 0.0.0.0
          port: 8080
          service_name: Talka

        asr:
          provider: funasr_external
          runtime_path: ""
          host: 127.0.0.1
          port: 10095
          mode: 2pass
          sample_rate: 16000
          startup_timeout_seconds: 180
          container_image: ""
          container_name: ""
          download_dir: ""
          hotword_path: ""
          models:
            asr: ""
            online: ""
            vad: ""
            punc: ""
            itn: ""
            lm: ""

        llm:
          provider: ollama
          base_url: http://localhost:11434
          model: qwen3:8b
          timeout_seconds: 30

        injection:
          mode: clipboard_paste
          restore_clipboard: true

        logging:
          level: info
        """
        try externalConfig.write(to: configURL, atomically: true, encoding: .utf8)

        let secondURL = try generator.generateConfig()
        XCTAssertEqual(secondURL, configURL)
        let contents = try String(contentsOf: configURL, encoding: .utf8)
        XCTAssertTrue(contents.contains("provider: funasr"))
        XCTAssertFalse(contents.contains("provider: funasr_external"))
    }

    func testDiagnosticsViewDoesNotRenderRecoverySection() throws {
        let sourceURL = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .appendingPathComponent("TalkaMacApp.swift")
        let source = try String(contentsOf: sourceURL, encoding: .utf8)
        guard let diagnosticsRange = source.range(of: "struct DiagnosticsView: View"),
              let headerRange = source.range(of: "struct DiagnosticHeaderCard: View") else {
            XCTFail("Could not locate DiagnosticsView source boundaries.")
            return
        }

        let diagnosticsSource = source[diagnosticsRange.lowerBound..<headerRange.lowerBound]
        XCTAssertFalse(diagnosticsSource.contains(#"DiagnosticSection(title: "Recovery")"#))
    }

    func testSettingsShellContainsGeneralAndDiagnosticsTabs() throws {
        let sourceURL = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .appendingPathComponent("TalkaMacApp.swift")
        let source = try String(contentsOf: sourceURL, encoding: .utf8)
        guard let settingsRange = source.range(of: "struct SettingsShellView: View"),
              let nextRange = source.range(of: "struct SettingsGeneralView: View") else {
            XCTFail("Could not locate SettingsShellView source boundaries.")
            return
        }

        let settingsSource = source[settingsRange.lowerBound..<nextRange.lowerBound]
        XCTAssertTrue(settingsSource.contains(#"Text("General").tag(SettingsTab.general)"#))
        XCTAssertTrue(settingsSource.contains(#"Text("Diagnostics").tag(SettingsTab.diagnostics)"#))
        XCTAssertTrue(settingsSource.contains("DiagnosticsView(viewModel: viewModel)"))
    }

    func testDiagnosticsWindowIsNotExposedSeparately() throws {
        let sourceURL = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .appendingPathComponent("TalkaMacApp.swift")
        let source = try String(contentsOf: sourceURL, encoding: .utf8)

        XCTAssertFalse(source.contains(#"Window("Diagnostics""#))
        XCTAssertFalse(source.contains(#"static let diagnostics = "diagnostics""#))
        XCTAssertFalse(source.contains(#"Button("Diagnostics""#))
    }

    func testRuntimeConfigGeneratorRefreshesStaleEmbeddedResourcePaths() throws {
        let tempDir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        let resourcesDir = tempDir.appendingPathComponent("resources", isDirectory: true)
        let modelsDir = resourcesDir.appendingPathComponent("models/funasr", isDirectory: true)
        let configURL = tempDir.appendingPathComponent("config.yaml")
        try FileManager.default.createDirectory(at: modelsDir, withIntermediateDirectories: true)
        setenv("TALKA_CONFIG_PATH", configURL.path, 1)
        setenv("TALKA_RESOURCES_PATH", resourcesDir.path, 1)

        let staleConfig = """
        server:
          bind_host: 0.0.0.0
          port: 8080
          service_name: Talka

        asr:
          provider: funasr
          runtime_path: /Applications/TalkaMac.app/Contents/Resources/talka-asr-runtime
          host: 127.0.0.1
          port: 10095
          mode: 2pass
          sample_rate: 16000
          startup_timeout_seconds: 180
          container_image: ""
          container_name: ""
          download_dir: ""
          hotword_path: /Applications/TalkaMac.app/Contents/Resources/hotwords.txt
          models:
            asr: /Applications/TalkaMac.app/Contents/Resources/models/funasr/paraformer-zh-onnx
            online: /Applications/TalkaMac.app/Contents/Resources/models/funasr/paraformer-zh-online-onnx
            vad: /Applications/TalkaMac.app/Contents/Resources/models/funasr/fsmn-vad-onnx
            punc: /Applications/TalkaMac.app/Contents/Resources/models/funasr/ct-punc-onnx
            itn: /Applications/TalkaMac.app/Contents/Resources/models/funasr/itn-zh
            lm: ""

        llm:
          provider: ollama
          base_url: http://localhost:11434
          model: custom-model
          timeout_seconds: 30

        injection:
          mode: clipboard_paste
          restore_clipboard: true

        logging:
          level: info
        """
        try staleConfig.write(to: configURL, atomically: true, encoding: .utf8)

        let generatedURL = try EmbeddedRuntimeConfigGenerator().generateConfig()

        XCTAssertEqual(generatedURL, configURL)
        let contents = try String(contentsOf: configURL, encoding: .utf8)
        XCTAssertTrue(contents.contains("asr:\n  provider: funasr"))
        XCTAssertTrue(contents.contains("runtime_path: \(resourcesDir.appendingPathComponent("talka-asr-runtime").path)"))
        XCTAssertTrue(contents.contains("hotword_path: \"\""))
        XCTAssertTrue(contents.contains("    asr: \(modelsDir.appendingPathComponent("paraformer-zh-onnx").path)"))
        XCTAssertTrue(contents.contains("model: custom-model"))
        XCTAssertFalse(contents.contains("/Applications/TalkaMac.app/Contents/Resources"))
    }

    func testRuntimeConfigGeneratorUsesSherpaWhenBundledAssetsExist() throws {
        let tempDir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        let resourcesDir = tempDir.appendingPathComponent("resources", isDirectory: true)
        let frameworksDir = tempDir.appendingPathComponent("Frameworks", isDirectory: true)
        let sherpaDir = resourcesDir.appendingPathComponent("models/sherpa-onnx/streaming-paraformer-trilingual-zh-cantonese-en", isDirectory: true)
        let configURL = tempDir.appendingPathComponent("config.yaml")
        try FileManager.default.createDirectory(at: sherpaDir, withIntermediateDirectories: true)
        try FileManager.default.createDirectory(at: frameworksDir, withIntermediateDirectories: true)
        for name in ["tokens.txt", "encoder.int8.onnx", "decoder.int8.onnx"] {
            try "asset".write(to: sherpaDir.appendingPathComponent(name), atomically: true, encoding: .utf8)
        }
        try "dylib".write(to: frameworksDir.appendingPathComponent("libsherpa-onnx-c-api.dylib"), atomically: true, encoding: .utf8)
        setenv("TALKA_CONFIG_PATH", configURL.path, 1)
        setenv("TALKA_RESOURCES_PATH", resourcesDir.path, 1)

        _ = try EmbeddedRuntimeConfigGenerator().generateConfig()

        let contents = try String(contentsOf: configURL, encoding: .utf8)
        XCTAssertTrue(contents.contains("asr:\n  provider: onnx"))
        XCTAssertTrue(contents.contains("model_type: paraformer"))
        XCTAssertTrue(contents.contains("precision: int8"))
        XCTAssertTrue(contents.contains("tokens_path: \(sherpaDir.appendingPathComponent("tokens.txt").path)"))
    }

    func testRuntimeConfigGeneratorDoesNotDuplicateProviderInSavedFourSpaceConfig() throws {
        let tempDir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        let resourcesDir = tempDir.appendingPathComponent("resources", isDirectory: true)
        let modelsDir = resourcesDir.appendingPathComponent("models/funasr", isDirectory: true)
        let configURL = tempDir.appendingPathComponent("config.yaml")
        try FileManager.default.createDirectory(at: modelsDir, withIntermediateDirectories: true)
        setenv("TALKA_CONFIG_PATH", configURL.path, 1)
        setenv("TALKA_RESOURCES_PATH", resourcesDir.path, 1)

        let savedConfig = """
        server:
            bind_host: 0.0.0.0
            port: 8080
            service_name: Talka
        asr:
            provider: funasr
            runtime_path: /Applications/TalkaMac.app/Contents/Resources/talka-asr-runtime
            funasr_binary_path: ""
            host: 127.0.0.1
            port: 10095
            mode: 2pass
            sample_rate: 16000
            startup_timeout_seconds: 180
            container_image: ""
            container_name: ""
            download_dir: ""
            hotword_path: /Applications/TalkaMac.app/Contents/Resources/hotwords.txt
            models:
                asr: /Applications/TalkaMac.app/Contents/Resources/models/funasr/paraformer-zh-onnx
                online: /Applications/TalkaMac.app/Contents/Resources/models/funasr/paraformer-zh-online-onnx
                vad: /Applications/TalkaMac.app/Contents/Resources/models/funasr/fsmn-vad-onnx
                punc: /Applications/TalkaMac.app/Contents/Resources/models/funasr/ct-punc-onnx
                itn: /Applications/TalkaMac.app/Contents/Resources/models/funasr/itn-zh
                lm: ""
        llm:
            provider: ollama
            base_url: http://localhost:11434
            model: custom-model
            timeout_seconds: 30
        injection:
            mode: clipboard_paste
            restore_clipboard: true
        logging:
            level: info
            capture_audio: false
            capture_transcript: false
        """
        try savedConfig.write(to: configURL, atomically: true, encoding: .utf8)

        _ = try EmbeddedRuntimeConfigGenerator().generateConfig()

        let contents = try String(contentsOf: configURL, encoding: .utf8)
        XCTAssertFalse(contents.contains("asr:\n  provider: funasr\n    provider: funasr"))
        XCTAssertEqual(contents.components(separatedBy: "provider: funasr").count - 1, 1)
        XCTAssertTrue(contents.contains("runtime_path: \(resourcesDir.appendingPathComponent("talka-asr-runtime").path)"))
        XCTAssertTrue(contents.contains("model: custom-model"))
    }

    func testRuntimeConfigGeneratorRepairsDuplicatedProviderFromPreviousRefreshBug() throws {
        let tempDir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        let resourcesDir = tempDir.appendingPathComponent("resources", isDirectory: true)
        let configURL = tempDir.appendingPathComponent("config.yaml")
        try FileManager.default.createDirectory(at: resourcesDir.appendingPathComponent("models/funasr", isDirectory: true), withIntermediateDirectories: true)
        setenv("TALKA_CONFIG_PATH", configURL.path, 1)
        setenv("TALKA_RESOURCES_PATH", resourcesDir.path, 1)

        let brokenConfig = """
        server:
            bind_host: 0.0.0.0
            port: 8080
            service_name: Talka
        asr:
          provider: funasr
            provider: funasr
            runtime_path: /Applications/TalkaMac.app/Contents/Resources/talka-asr-runtime
            host: 127.0.0.1
            port: 10095
            mode: 2pass
            sample_rate: 16000
            startup_timeout_seconds: 180
            hotword_path: /Applications/TalkaMac.app/Contents/Resources/hotwords.txt
            models:
                asr: /Applications/TalkaMac.app/Contents/Resources/models/funasr/paraformer-zh-onnx
        llm:
            provider: ollama
            base_url: http://localhost:11434
            model: custom-model
            timeout_seconds: 30
        injection:
            mode: clipboard_paste
            restore_clipboard: true
        logging:
            level: info
        """
        try brokenConfig.write(to: configURL, atomically: true, encoding: .utf8)

        _ = try EmbeddedRuntimeConfigGenerator().generateConfig()

        let contents = try String(contentsOf: configURL, encoding: .utf8)
        XCTAssertFalse(contents.contains("asr:\n  provider: funasr\n    provider: funasr"))
        XCTAssertEqual(contents.components(separatedBy: "provider: funasr").count - 1, 1)
        XCTAssertTrue(contents.contains("runtime_path: \(resourcesDir.appendingPathComponent("talka-asr-runtime").path)"))
    }

    func testRuntimeConfigGeneratorRepairsMalformedEmbeddedResourcePathLine() throws {
        let tempDir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        let resourcesDir = tempDir.appendingPathComponent("resources", isDirectory: true)
        let configURL = tempDir.appendingPathComponent("config.yaml")
        try FileManager.default.createDirectory(at: resourcesDir.appendingPathComponent("models/funasr", isDirectory: true), withIntermediateDirectories: true)
        setenv("TALKA_CONFIG_PATH", configURL.path, 1)
        setenv("TALKA_RESOURCES_PATH", resourcesDir.path, 1)

        let malformedConfig = """
        server:
          bind_host: 0.0.0.0
          port: 8080
          service_name: Talka

        asr:
          /Applications/TalkaMac.app/Contents/Resources/models/funasr/paraformer-zh-onnx
          provider: funasr
          runtime_path: /Applications/TalkaMac.app/Contents/Resources/talka-asr-runtime
          host: 127.0.0.1
          port: 10095
          mode: 2pass
          sample_rate: 16000
          startup_timeout_seconds: 180
          container_image: ""
          container_name: ""
          download_dir: ""
          hotword_path: /Applications/TalkaMac.app/Contents/Resources/hotwords.txt
          models:
            asr: /Applications/TalkaMac.app/Contents/Resources/models/funasr/paraformer-zh-onnx
            online: /Applications/TalkaMac.app/Contents/Resources/models/funasr/paraformer-zh-online-onnx
            vad: /Applications/TalkaMac.app/Contents/Resources/models/funasr/fsmn-vad-onnx
            punc: /Applications/TalkaMac.app/Contents/Resources/models/funasr/ct-punc-onnx
            itn: /Applications/TalkaMac.app/Contents/Resources/models/funasr/itn-zh
            lm: ""

        llm:
          provider: ollama
          base_url: http://localhost:11434
          model: qwen3:8b
          timeout_seconds: 30
        """
        try malformedConfig.write(to: configURL, atomically: true, encoding: .utf8)

        _ = try EmbeddedRuntimeConfigGenerator().generateConfig()

        let contents = try String(contentsOf: configURL, encoding: .utf8)
        XCTAssertFalse(contents.contains("  /Applications/TalkaMac.app/Contents/Resources/models/funasr/paraformer-zh-onnx"))
        XCTAssertTrue(contents.contains("asr:\n  provider: funasr"))
        XCTAssertTrue(contents.contains("hotword_path: \"\""))
    }

    func testRuntimeConfigGeneratorRestoresMissingEmbeddedProvider() throws {
        let tempDir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        let resourcesDir = tempDir.appendingPathComponent("resources", isDirectory: true)
        let configURL = tempDir.appendingPathComponent("config.yaml")
        try FileManager.default.createDirectory(at: resourcesDir.appendingPathComponent("models/funasr", isDirectory: true), withIntermediateDirectories: true)
        setenv("TALKA_CONFIG_PATH", configURL.path, 1)
        setenv("TALKA_RESOURCES_PATH", resourcesDir.path, 1)

        let missingProviderConfig = """
        server:
          bind_host: 0.0.0.0
          port: 8080
          service_name: Talka

        asr:
          runtime_path: /Applications/TalkaMac.app/Contents/Resources/talka-asr-runtime
          host: 127.0.0.1
          port: 10095
          mode: 2pass
          sample_rate: 16000
          startup_timeout_seconds: 180
          container_image: ""
          container_name: ""
          download_dir: ""
          hotword_path: ""
          models:
            asr: /Applications/TalkaMac.app/Contents/Resources/models/funasr/paraformer-zh-onnx
            online: /Applications/TalkaMac.app/Contents/Resources/models/funasr/paraformer-zh-online-onnx
            vad: /Applications/TalkaMac.app/Contents/Resources/models/funasr/fsmn-vad-onnx
            punc: /Applications/TalkaMac.app/Contents/Resources/models/funasr/ct-punc-onnx
            itn: /Applications/TalkaMac.app/Contents/Resources/models/funasr/itn-zh
            lm: ""

        llm:
          provider: ollama
          base_url: http://localhost:11434
          model: qwen3:8b
          timeout_seconds: 30
        """
        try missingProviderConfig.write(to: configURL, atomically: true, encoding: .utf8)

        _ = try EmbeddedRuntimeConfigGenerator().generateConfig()

        let contents = try String(contentsOf: configURL, encoding: .utf8)
        XCTAssertTrue(contents.contains("asr:\n  provider: funasr\n  runtime_path: \(resourcesDir.appendingPathComponent("talka-asr-runtime").path)"))
    }

    private func makeLiveClient(handler: @escaping @Sendable (URLRequest) throws -> (HTTPURLResponse, Data)) -> LiveControlAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubURLProtocol.self]
        let session = URLSession(configuration: configuration)
        StubURLProtocol.requestHandler = handler
        return LiveControlAPIClient(baseURL: URL(string: "http://127.0.0.1:8080")!, session: session)
    }

    nonisolated private static func httpResponse(body: String, statusCode: Int = 200) throws -> (HTTPURLResponse, Data) {
        try httpResponse(url: XCTUnwrap(URL(string: "http://127.0.0.1:8080/v1/config")), body: body, statusCode: statusCode)
    }

    nonisolated private static func httpResponse(url: URL, body: String, statusCode: Int = 200) throws -> (HTTPURLResponse, Data) {
        let response = try XCTUnwrap(HTTPURLResponse(url: url, statusCode: statusCode, httpVersion: nil, headerFields: ["Content-Type": "application/json"]))
        return (response, Data(body.utf8))
    }
}

private final class StubURLProtocol: URLProtocol, @unchecked Sendable {
    static var requestHandler: (@Sendable (URLRequest) throws -> (HTTPURLResponse, Data))?
    static var lastRequest: URLRequest?
    private static let lock = NSLock()

    static func reset() {
        lock.lock()
        requestHandler = nil
        lastRequest = nil
        lock.unlock()
    }

    override class func canInit(with request: URLRequest) -> Bool {
        true
    }

    override class func canonicalRequest(for request: URLRequest) -> URLRequest {
        request
    }

    override func startLoading() {
        StubURLProtocol.lock.lock()
        let handler = StubURLProtocol.requestHandler
        StubURLProtocol.lastRequest = request
        StubURLProtocol.lock.unlock()

        guard let handler else {
            client?.urlProtocol(self, didFailWithError: URLError(.badServerResponse))
            return
        }

        do {
            let (response, data) = try handler(request)
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    override func stopLoading() {}
}

private extension URLRequest {
    func talkaHTTPBodyData() -> Data? {
        if let httpBody {
            return httpBody
        }

        guard let stream = httpBodyStream else {
            return nil
        }

        stream.open()
        defer { stream.close() }

        let bufferSize = 1024
        var data = Data()
        let buffer = UnsafeMutablePointer<UInt8>.allocate(capacity: bufferSize)
        defer { buffer.deallocate() }

        while stream.hasBytesAvailable {
            let readCount = stream.read(buffer, maxLength: bufferSize)
            guard readCount >= 0 else {
                return nil
            }
            if readCount == 0 {
                break
            }
            data.append(buffer, count: readCount)
        }

        return data
    }
}

private final class FakeControlAPIClient: ControlAPIClient {
    let baseURL = URL(string: "http://127.0.0.1:8080")!
    private var statusResults: [Result<ControlStatus, Error>]
    private var devicesResults: [Result<[ControlDevice], Error>]
    private var configResults: [Result<ControlConfig, Error>]
    private var diagnosticsResults: [Result<ControlDiagnostics, Error>]
    private var pairingResults: [Result<ControlPairingSession, Error>]
    private var accessibilityResults: [Result<AccessibilityGuidance, Error>]
    private(set) var savedConfig: ControlConfig?
    private(set) var forgottenDeviceIDs: [String] = []
    private(set) var accessibilityOpenCalls = 0
    private(set) var fetchConfigCalls = 0

    init(
        statusResults: [Result<ControlStatus, Error>] = [.success(.fixture())],
        devicesResults: [Result<[ControlDevice], Error>] = [.success([])],
        configResults: [Result<ControlConfig, Error>] = [.success(.fixture())],
        diagnosticsResults: [Result<ControlDiagnostics, Error>] = [.success(ControlDiagnostics(latencyTraces: []))],
        pairingResults: [Result<ControlPairingSession, Error>] = [.success(.fixture())],
        accessibilityResults: [Result<AccessibilityGuidance, Error>] = [.success(.fixture())]
    ) {
        self.statusResults = statusResults
        self.devicesResults = devicesResults
        self.configResults = configResults
        self.diagnosticsResults = diagnosticsResults
        self.pairingResults = pairingResults
        self.accessibilityResults = accessibilityResults
    }

    func fetchStatus() async throws -> ControlStatus {
        try next(from: &statusResults)
    }

    func fetchDevices() async throws -> [ControlDevice] {
        try next(from: &devicesResults)
    }

    func startPairing() async throws -> ControlPairingSession {
        try next(from: &pairingResults)
    }

    func fetchConfig() async throws -> ControlConfig {
        fetchConfigCalls += 1
        return try next(from: &configResults)
    }

    func fetchDiagnostics() async throws -> ControlDiagnostics {
        try next(from: &diagnosticsResults)
    }

    func saveConfig(_ config: ControlConfig) async throws -> ControlConfig {
        savedConfig = config
        return config
    }

    func forgetDevice(id: String) async throws {
        forgottenDeviceIDs.append(id)
    }

    func openAccessibilitySettings() async throws -> AccessibilityGuidance {
        accessibilityOpenCalls += 1
        return try next(from: &accessibilityResults)
    }

    private func next<T>(from queue: inout [Result<T, Error>]) throws -> T {
        let result = queue.isEmpty ? .failure(ControlAPIClientError.serviceUnavailable) : queue.removeFirst()
        return try result.get()
    }
}

private final class FakeRecoveryTextCopier: RecoveryTextCopying {
    private(set) var copiedText: String?

    func copy(_ text: String) {
        copiedText = text
    }
}

private final class FakeAccessibilityPermissionChecker: AccessibilityPermissionChecking {
    private var statuses: [AccessibilityPermissionStatus]
    private let requestStatus: AccessibilityPermissionStatus
    private(set) var requestAccessCalls = 0

    init(statuses: [AccessibilityPermissionStatus], requestStatus: AccessibilityPermissionStatus = .missing) {
        self.statuses = statuses
        self.requestStatus = requestStatus
    }

    func status() -> AccessibilityPermissionStatus {
        statuses.isEmpty ? requestStatus : statuses.removeFirst()
    }

    func requestAccess() -> AccessibilityPermissionStatus {
        requestAccessCalls += 1
        return requestStatus
    }
}

private final class ControlledNow {
    var current: Date

    init(_ current: Date) {
        self.current = current
    }
}

private extension ControlStatus {
    static func fixture(
        state: String = "running",
        pairingActive: Bool = false,
        asr: ControlASRStatus? = nil,
        ollama: ControlOllamaStatus? = nil,
        permissions: ControlPermissionsStatus? = nil
    ) -> ControlStatus {
        ControlStatus(
            serviceName: "Talka",
            state: state,
            configPath: "/tmp/talka.yaml",
            uptimeSeconds: 12,
            deviceCount: pairingActive ? 1 : 0,
            pairingActive: pairingActive,
            asr: asr,
            ollama: ollama,
            permissions: permissions
        )
    }
}

private extension ControlDevice {
    static func fixture() -> ControlDevice {
        ControlDevice(id: "device-1", name: "iPhone", paired: true, lastSeenAt: Date(timeIntervalSince1970: 1_700_000_000))
    }
}

private extension ControlConfig {
    static func fixture() -> ControlConfig {
        ControlConfig(
            path: "/tmp/talka.yaml",
            server: ControlServerConfig(bindHost: "127.0.0.1", port: 8080, serviceName: "Talka"),
            asr: ControlASRConfig(
                provider: "funasr",
                runtimePath: "talka-asr-runtime",
                host: "127.0.0.1",
                port: 10095,
                mode: "2pass",
                sampleRate: 16_000,
                startupTimeoutSeconds: 180,
                containerImage: "",
                containerName: "",
                downloadDir: "",
                hotwordPath: "",
                models: ControlASRModelsConfig(
                    asr: "models/funasr/paraformer-zh-onnx",
                    online: "models/funasr/paraformer-zh-online-onnx",
                    vad: "models/funasr/fsmn-vad-onnx",
                    punc: "models/funasr/ct-punc-onnx",
                    itn: "models/funasr/itn-zh",
                    lm: ""
                )
            ),
            llm: ControlLLMConfig(provider: "ollama", baseURL: "http://localhost:11434", model: "qwen3:8b", timeoutSeconds: 30),
            injection: ControlInjectionConfig(mode: "clipboard_paste", restoreClipboard: true),
            logging: ControlLoggingConfig(level: "info", captureAudio: false, captureTranscript: false)
        )
    }
}

private extension ControlPairingSession {
    static func fixture() -> ControlPairingSession {
        ControlPairingSession(
            pairingID: "pairing-1",
            pin: "654321",
            expiresAt: Date(timeIntervalSince1970: 1_700_000_120),
            expiresInSeconds: 120
        )
    }
}

private extension AccessibilityGuidance {
    static func fixture() -> AccessibilityGuidance {
        AccessibilityGuidance(
            permission: "accessibility",
            opened: false,
            settingsURL: "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility",
            message: "Open System Settings → Privacy & Security → Accessibility"
        )
    }
}
