import AVFoundation
import CryptoKit
import SwiftUI
import XCTest
@testable import TalkaIOS

@MainActor
final class TalkaIOSTests: XCTestCase {
    func testAVAudioConverterProducesSixteenKilohertzMonoPCM() throws {
        let inputFormat = try XCTUnwrap(AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: 48_000, channels: 1, interleaved: false))
        let inputBuffer = try XCTUnwrap(AVAudioPCMBuffer(pcmFormat: inputFormat, frameCapacity: 960))
        inputBuffer.frameLength = 960
        let samples = try XCTUnwrap(inputBuffer.floatChannelData?[0])
        for index in 0..<Int(inputBuffer.frameLength) {
            samples[index] = 0.25
        }

        let converter = AudioPCMConverter()
        let pcm = try converter.convert(inputBuffer)

        XCTAssertEqual(pcm.count, TalkaAudioFormat.frameByteCount)
    }

    func testMicrophoneCaptureUsesHardwareInputFormatForTap() throws {
        let inputFormat = try XCTUnwrap(AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: 44_100, channels: 2, interleaved: false))
        let source = MicrophonePCMSource(engine: FakeAudioEngine(inputFormat: inputFormat), converter: AudioPCMConverter())

        try source.start { _ in }

        XCTAssertEqual(source.installedTapFormat?.sampleRate, 44_100)
        XCTAssertEqual(source.installedTapFormat?.channelCount, 2)
        source.stop()
    }

    func testMicrophoneCaptureConfiguresAudioSessionBeforeEngineStart() throws {
        let inputFormat = try XCTUnwrap(AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: 44_100, channels: 1, interleaved: false))
        var events: [String] = []
        let engine = FakeAudioEngine(inputFormat: inputFormat, events: { events.append($0) })
        let audioSession = RecordingAudioSession(events: { events.append($0) })
        let source = MicrophonePCMSource(engine: engine, converter: AudioPCMConverter(), audioSession: audioSession)

        try source.start { _ in }

        XCTAssertEqual(events, ["configure-session", "install-tap", "start-engine"])
        XCTAssertTrue(audioSession.didConfigureForRecording)
        source.stop()
    }

    func testPCMFramerEmitsOnlyCompleteTwentyMillisecondFrames() {
        var framer = PCMFrameAccumulator(frameByteCount: TalkaAudioFormat.frameByteCount)

        let first = framer.append(Data(repeating: 1, count: 639))
        XCTAssertTrue(first.isEmpty)
        XCTAssertEqual(framer.bufferedByteCount, 639)

        let second = framer.append(Data(repeating: 2, count: 641))
        XCTAssertEqual(second.map(\.count), [640, 640])
        XCTAssertEqual(framer.bufferedByteCount, 0)
    }

    func testAudioStreamClientSendsStartFramesAndStopWithStrictSequences() async {
        let streamClient = RecordingAudioStreamClient()
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: FakeDiscoveryBrowser(),
            sessionClient: FakeRemoteSessionClient(),
            identityStore: FakePairedIdentityStore(),
            audioStreamClient: streamClient
        )

        await viewModel.streamPCMChunksForTesting([
            Data(repeating: 1, count: TalkaAudioFormat.frameByteCount),
            Data(repeating: 2, count: TalkaAudioFormat.frameByteCount)
        ])

        XCTAssertEqual(viewModel.recordingState, .idle)
        XCTAssertEqual(streamClient.events, [
            .start(.talkaDefault),
            .frame(sequence: 1, byteCount: TalkaAudioFormat.frameByteCount),
            .frame(sequence: 2, byteCount: TalkaAudioFormat.frameByteCount),
            .stop(lastSequence: 2)
        ])
    }

    func testAudioStreamCancelSendsCancelAndClearsRecordingState() async {
        let streamClient = RecordingAudioStreamClient()
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: FakeDiscoveryBrowser(),
            sessionClient: FakeRemoteSessionClient(),
            identityStore: FakePairedIdentityStore(),
            audioStreamClient: streamClient
        )

        await viewModel.cancelRecording(reason: "user_cancelled")

        XCTAssertEqual(viewModel.recordingState, .idle)
        XCTAssertEqual(streamClient.events, [.cancel(reason: "user_cancelled")])
    }

    func testAudioBackpressureStopsRecordingWithRecoverableError() async {
        let streamClient = RecordingAudioStreamClient()
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: FakeDiscoveryBrowser(),
            sessionClient: FakeRemoteSessionClient(),
            identityStore: FakePairedIdentityStore(),
            audioStreamClient: streamClient
        )

        await viewModel.streamPCMChunksForTesting([
            Data(repeating: 1, count: TalkaAudioFormat.frameByteCount * 2)
        ], maxQueuedFrames: 1)

        XCTAssertEqual(viewModel.recordingState, .failed)
        XCTAssertEqual(viewModel.lastErrorMessage, "Audio streaming stalled. Recording stopped safely.")
        XCTAssertEqual(streamClient.events, [
            .start(.talkaDefault),
            .cancel(reason: "backpressure")
        ])
    }

    func testProductionRecordingUnavailableDoesNotStartMicrophoneCapture() async {
        let microphoneSource = FakeMicrophoneSource()
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: FakeDiscoveryBrowser(),
            sessionClient: FakeRemoteSessionClient(),
            identityStore: FakePairedIdentityStore(),
            audioStreamClient: UnavailableAudioStreamClient(),
            microphoneSource: microphoneSource
        )

        await viewModel.startRecording()

        XCTAssertEqual(viewModel.recordingState, .failed)
        XCTAssertFalse(microphoneSource.didStart)
        XCTAssertEqual(viewModel.lastErrorMessage, UnavailableAudioStreamClient.unavailableMessage)
    }

    func testStartRecordingReportsMissingSecureSessionBeforeWebSocketStart() async {
        let microphoneSource = FakeMicrophoneSource()
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: FakeDiscoveryBrowser(),
            sessionClient: FakeRemoteSessionClient(),
            identityStore: FakePairedIdentityStore(),
            audioStreamClient: SecureAudioStreamClient(sessionStore: SecureAudioSessionStore()),
            microphoneSource: microphoneSource
        )

        await viewModel.startRecording()

        XCTAssertEqual(viewModel.recordingState, .failed)
        XCTAssertFalse(microphoneSource.didStart)
        XCTAssertEqual(viewModel.lastAudioDiagnostic, "no active secure audio session before websocket start")
    }

    func testStartRecordingReportsWebSocketBootstrapFailure() async {
        let microphoneSource = FakeMicrophoneSource()
        let streamClient = FailingAudioStreamClient(startError: RemoteMicFlowError.recordingFailed("bootstrap exploded"))
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: FakeDiscoveryBrowser(),
            sessionClient: FakeRemoteSessionClient(),
            identityStore: FakePairedIdentityStore(),
            audioStreamClient: streamClient,
            microphoneSource: microphoneSource
        )

        await viewModel.startRecording()

        XCTAssertEqual(viewModel.recordingState, .failed)
        XCTAssertFalse(microphoneSource.didStart)
        XCTAssertEqual(viewModel.lastAudioDiagnostic, "WebSocket bootstrap/sendAudioStart failed: bootstrap exploded")
    }

    func testStartRecordingReportsSendAudioFrameFailuresFromPCMCallback() async {
        let microphoneSource = FakeMicrophoneSource()
        let streamClient = FailingAudioStreamClient(frameError: RemoteMicFlowError.recordingFailed("frame send exploded"))
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: FakeDiscoveryBrowser(),
            sessionClient: FakeRemoteSessionClient(),
            identityStore: FakePairedIdentityStore(),
            audioStreamClient: streamClient,
            microphoneSource: microphoneSource
        )

        await viewModel.startRecording()
        microphoneSource.emit(Data(repeating: 1, count: TalkaAudioFormat.frameByteCount))
        try? await Task.sleep(nanoseconds: 50_000_000)

        XCTAssertEqual(viewModel.recordingState, .failed)
        XCTAssertEqual(viewModel.lastAudioDiagnostic, "sendPCMChunk/sendAudioFrame failed: frame send exploded")
    }

    func testStartRecordingReportsWaitingForMicrophoneFramesAfterSuccessfulBootstrap() async {
        let microphoneSource = FakeMicrophoneSource()
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: FakeDiscoveryBrowser(),
            sessionClient: FakeRemoteSessionClient(),
            identityStore: FakePairedIdentityStore(),
            audioStreamClient: RecordingAudioStreamClient(),
            microphoneSource: microphoneSource
        )

        await viewModel.startRecording()

        XCTAssertEqual(viewModel.recordingState, RemoteMicRecordingState.recording)
        XCTAssertTrue(microphoneSource.didStart)
        XCTAssertEqual(viewModel.lastAudioDiagnostic, "recording started; waiting for microphone frames")
    }

    func testStartRecordingReportsNoMicrophoneFramesAfterTimeout() async {
        let microphoneSource = FakeMicrophoneSource()
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: FakeDiscoveryBrowser(),
            sessionClient: FakeRemoteSessionClient(),
            identityStore: FakePairedIdentityStore(),
            audioStreamClient: RecordingAudioStreamClient(),
            microphoneSource: microphoneSource,
            firstFrameTimeoutNanoseconds: 1_000_000
        )

        await viewModel.startRecording()
        try? await Task.sleep(nanoseconds: 50_000_000)

        XCTAssertEqual(viewModel.recordingState, RemoteMicRecordingState.recording)
        XCTAssertEqual(viewModel.lastAudioDiagnostic, "no microphone frames received after audio session start")
    }

    func testStartRecordingReportsAudioFramesFlowingAfterFirstPCMFrame() async {
        let microphoneSource = FakeMicrophoneSource()
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: FakeDiscoveryBrowser(),
            sessionClient: FakeRemoteSessionClient(),
            identityStore: FakePairedIdentityStore(),
            audioStreamClient: RecordingAudioStreamClient(),
            microphoneSource: microphoneSource
        )

        await viewModel.startRecording()
        microphoneSource.emit(Data(repeating: 1, count: TalkaAudioFormat.frameByteCount))
        try? await Task.sleep(nanoseconds: 50_000_000)

        XCTAssertEqual(viewModel.recordingState, RemoteMicRecordingState.recording)
        XCTAssertEqual(viewModel.lastAudioDiagnostic, "audio frames flowing; seq=1")
        XCTAssertEqual(viewModel.audioLevel, 1)
    }

    func testMicrophoneConversionFailuresSurfaceARecordingDiagnostic() async throws {
        let inputFormat = try XCTUnwrap(AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: 44_100, channels: 1, interleaved: false))
        let engine = FakeAudioEngine(inputFormat: inputFormat)
        let microphoneSource = MicrophonePCMSource(engine: engine, converter: ThrowingAudioPCMConverter(error: RemoteMicFlowError.recordingFailed("converter exploded")))
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: FakeDiscoveryBrowser(),
            sessionClient: FakeRemoteSessionClient(),
            identityStore: FakePairedIdentityStore(),
            audioStreamClient: RecordingAudioStreamClient(),
            microphoneSource: microphoneSource
        )
        let buffer = try XCTUnwrap(AVAudioPCMBuffer(pcmFormat: inputFormat, frameCapacity: 1))
        buffer.frameLength = 1

        await viewModel.startRecording()

        engine.emit(buffer)
        await Task.yield()

        XCTAssertEqual(viewModel.recordingState, .failed)
        XCTAssertEqual(viewModel.lastAudioDiagnostic, "Microphone conversion failed: converter exploded")
    }

    func testMicrophoneSectionRendersLastAudioDiagnosticSeparatelyFromGenericError() async throws {
        let streamClient = FailingAudioStreamClient(startError: RemoteMicFlowError.recordingFailed("bootstrap exploded"))
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: FakeDiscoveryBrowser(),
            sessionClient: FakeRemoteSessionClient(),
            identityStore: FakePairedIdentityStore(),
            audioStreamClient: streamClient,
            microphoneSource: FakeMicrophoneSource()
        )

        await viewModel.startRecording()

        XCTAssertEqual(viewModel.lastErrorMessage, "bootstrap exploded")
        XCTAssertEqual(viewModel.lastAudioDiagnostic, "WebSocket bootstrap/sendAudioStart failed: bootstrap exploded")

        let renderedStrings = renderedViewStrings(in: StatusMessageStack(viewModel: viewModel).body)

        XCTAssertTrue(renderedStrings.contains("bootstrap exploded"), renderedStrings.joined(separator: "\n"))
        XCTAssertTrue(renderedStrings.contains("Audio diagnostic: WebSocket bootstrap/sendAudioStart failed: bootstrap exploded"), renderedStrings.joined(separator: "\n"))
    }

    func testRemoteControlShellAvoidsInstructionalMainScreenLabels() {
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: FakeDiscoveryBrowser(),
            sessionClient: FakeRemoteSessionClient(),
            identityStore: FakePairedIdentityStore(initialIdentity: PairedMacIdentity(deviceID: "mac-1", deviceName: "Darluc's MacBook Pro"))
        )

        let renderedStrings = renderedViewStrings(in: RemoteMicControlSurface(
            viewModel: viewModel,
            isPressingMicrophone: false,
            showConnectionPanel: {},
            togglePower: {},
            startRecording: {},
            stopRecording: {}
        ).body)

        XCTAssertFalse(renderedStrings.contains("Talka"), renderedStrings.joined(separator: "\n"))
        XCTAssertFalse(renderedStrings.contains("Connected"), renderedStrings.joined(separator: "\n"))
        XCTAssertFalse(renderedStrings.contains("Hold to Talk"), renderedStrings.joined(separator: "\n"))
    }

    func testPowerButtonDisconnectsCurrentMacButKeepsRememberedPairing() async {
        let identity = PairedMacIdentity(deviceID: "mac-1", deviceName: "Darluc's MacBook Pro")
        var clearedSecureSessions = 0
        let store = FakePairedIdentityStore(initialIdentity: identity)
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: FakeDiscoveryBrowser(),
            sessionClient: FakeRemoteSessionClient(),
            identityStore: store,
            disconnectSecureSession: {
                clearedSecureSessions += 1
            }
        )
        await viewModel.reconnectToKnownMac()

        await viewModel.toggleConnectionPower()

        XCTAssertEqual(viewModel.connectionState, .idle)
        XCTAssertNil(viewModel.currentMacName)
        XCTAssertEqual(viewModel.knownMacName, "Darluc's MacBook Pro")
        XCTAssertEqual(store.identity, identity)
        XCTAssertEqual(clearedSecureSessions, 1)
    }

    func testPowerButtonReconnectsRememberedMacWhenDisconnected() async {
        let identity = PairedMacIdentity(deviceID: "mac-1", deviceName: "Darluc's MacBook Pro")
        let sessionClient = FakeRemoteSessionClient(reconnectResults: [.success(identity)])
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: FakeDiscoveryBrowser(),
            sessionClient: sessionClient,
            identityStore: FakePairedIdentityStore(initialIdentity: identity)
        )

        await viewModel.toggleConnectionPower()

        XCTAssertEqual(viewModel.connectionState, .paired)
        XCTAssertEqual(viewModel.currentMacName, "Darluc's MacBook Pro")
        XCTAssertEqual(sessionClient.reconnectCalls, 1)
    }

    func testPairedMacIdentityEncodingDropsReusableSessionMaterial() throws {
        let unsafeStoredIdentity = Data("""
        {
            "deviceID": "mac-1",
            "deviceName": "Darluc's MacBook Pro",
            "hostName": "127.0.0.1",
            "port": 12345,
            "sessionID": "unsafe-session",
            "clientToServerKey": "unsafe-client-key",
            "serverToClientKey": "unsafe-server-key",
            "audioWebSocketURL": "ws://127.0.0.1:12345/v1/session/audio"
        }
        """.utf8)

        let identity = try JSONDecoder().decode(PairedMacIdentity.self, from: unsafeStoredIdentity)
        let encoded = try JSONEncoder().encode(identity)
        let body = String(decoding: encoded, as: UTF8.self)

        XCTAssertEqual(identity, PairedMacIdentity(deviceID: "mac-1", deviceName: "Darluc's MacBook Pro", hostName: "127.0.0.1", port: 12345))
        XCTAssertFalse(body.contains("sessionID"), body)
        XCTAssertFalse(body.contains("clientToServerKey"), body)
        XCTAssertFalse(body.contains("serverToClientKey"), body)
        XCTAssertFalse(body.contains("audioWebSocketURL"), body)
    }

    func testStopRecordingStopsCaptureAndSendsStop() async {
        let streamClient = RecordingAudioStreamClient()
        let microphoneSource = FakeMicrophoneSource()
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: FakeDiscoveryBrowser(),
            sessionClient: FakeRemoteSessionClient(),
            identityStore: FakePairedIdentityStore(),
            audioStreamClient: streamClient,
            microphoneSource: microphoneSource
        )

        await viewModel.startRecording()
        microphoneSource.emit(Data(repeating: 3, count: TalkaAudioFormat.frameByteCount))
        await Task.yield()
        await viewModel.stopRecording()

        XCTAssertTrue(microphoneSource.didStart)
        XCTAssertTrue(microphoneSource.didStop)
        XCTAssertEqual(Array(streamClient.events.prefix(2)), [
            .start(.talkaDefault),
            .frame(sequence: 1, byteCount: TalkaAudioFormat.frameByteCount)
        ])
        XCTAssertEqual(streamClient.events.count, 3)
        guard case .stop = streamClient.events[2] else {
            XCTFail("events[2] = \(streamClient.events[2]), want stop")
            return
        }
        XCTAssertEqual(viewModel.recordingState, .idle)
    }

    func testLatePCMFrameAfterStopDoesNotFlipSuccessfulRecordingIntoFailure() async {
        let streamClient = BlockingStopAudioStreamClient()
        let microphoneSource = FakeMicrophoneSource()
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: FakeDiscoveryBrowser(),
            sessionClient: FakeRemoteSessionClient(),
            identityStore: FakePairedIdentityStore(),
            audioStreamClient: streamClient,
            microphoneSource: microphoneSource
        )

        await viewModel.startRecording()
        microphoneSource.emit(Data(repeating: 3, count: TalkaAudioFormat.frameByteCount))
        await Task.yield()

        let stopTask = Task { @MainActor in
            await viewModel.stopRecording()
        }

        while !streamClient.stopStarted {
            await Task.yield()
        }

        microphoneSource.emit(Data(repeating: 4, count: TalkaAudioFormat.frameByteCount))
        try? await Task.sleep(nanoseconds: 50_000_000)
        XCTAssertFalse(streamClient.lateFrameAttempted)
        streamClient.finishStop()
        await stopTask.value

        XCTAssertEqual(Array(streamClient.events.prefix(2)), [
            .start(.talkaDefault),
            .frame(sequence: 1, byteCount: TalkaAudioFormat.frameByteCount)
        ])
        XCTAssertEqual(streamClient.events.count, 3)
        guard case .stop = streamClient.events[2] else {
            XCTFail("events[2] = \(streamClient.events[2]), want stop")
            return
        }
        XCTAssertEqual(viewModel.recordingState, .idle)
        XCTAssertNil(viewModel.lastErrorMessage)
    }

    func testInFlightAudioFrameFailureAfterStopDoesNotFlipSuccessfulRecordingIntoFailure() async {
        let streamClient = BlockingFrameAudioStreamClient()
        let microphoneSource = FakeMicrophoneSource()
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: FakeDiscoveryBrowser(),
            sessionClient: FakeRemoteSessionClient(),
            identityStore: FakePairedIdentityStore(),
            audioStreamClient: streamClient,
            microphoneSource: microphoneSource
        )

        await viewModel.startRecording()
        microphoneSource.emit(Data(repeating: 3, count: TalkaAudioFormat.frameByteCount))

        while !streamClient.frameStarted {
            await Task.yield()
        }

        let stopTask = Task { @MainActor in
            await viewModel.stopRecording()
        }

        while !streamClient.stopStarted {
            await Task.yield()
        }

        streamClient.failBlockedFrameAfterStop()
        await stopTask.value
        try? await Task.sleep(nanoseconds: 50_000_000)

        XCTAssertEqual(streamClient.stopLastSequence, 0)
        XCTAssertEqual(viewModel.recordingState, .idle)
        XCTAssertNil(viewModel.lastErrorMessage)
        XCTAssertNil(viewModel.lastAudioDiagnostic)
    }

    func testProductionEnvironmentUsesBonjourDiscoveryAndSecureTransportDefaults() {
        let environment = RemoteMicShellEnvironment.production()

        XCTAssertTrue(environment.discoveryBrowser is BonjourRemoteMacDiscoveryBrowser)
        XCTAssertTrue(environment.sessionClient is SecureRemotePairingSessionClient)
        XCTAssertTrue(environment.audioStreamClient is SecureAudioStreamClient)
        XCTAssertTrue(environment.identityStore is KeychainPairedIdentityStore)
    }

    func testSecureAudioStreamClientKeepsEncryptedSequenceMonotonicAcrossRecordingsInSameSession() async throws {
        let sessionStore = SecureAudioSessionStore()
        sessionStore.save(makeSecureAudioSession(id: "session-a", url: "ws://127.0.0.1/session-a"))
        let connector = RecordingSecureAudioWebSocketConnector()
        let client = SecureAudioStreamClient(
            sessionStore: sessionStore,
            webSocketConnector: connector
        )

        try await client.sendAudioStart(metadata: .talkaDefault)
        try await client.sendAudioFrame(sequence: 1, payload: Data([1, 2, 3]))
        try await client.sendAudioStop(lastSequence: 1)
        try await client.sendAudioStart(metadata: .talkaDefault)
        try await client.sendAudioFrame(sequence: 1, payload: Data([4, 5, 6]))
        try await client.sendAudioStop(lastSequence: 1)

        XCTAssertEqual(recordedEncryptedSequences(in: connector), [1, 2, 3, 4, 5, 6])
        XCTAssertEqual(connector.createdTasks.count, 2)
    }

    func testSecureAudioStreamClientResetsEncryptedSequenceWhenSecureSessionChanges() async throws {
        let sessionStore = SecureAudioSessionStore()
        sessionStore.save(makeSecureAudioSession(id: "session-a", url: "ws://127.0.0.1/session-a"))
        let connector = RecordingSecureAudioWebSocketConnector()
        let client = SecureAudioStreamClient(
            sessionStore: sessionStore,
            webSocketConnector: connector
        )

        try await client.sendAudioStart(metadata: .talkaDefault)
        try await client.sendAudioStop(lastSequence: 0)

        sessionStore.save(makeSecureAudioSession(id: "session-b", url: "ws://127.0.0.1/session-b"))

        try await client.sendAudioStart(metadata: .talkaDefault)
        try await client.sendAudioStop(lastSequence: 0)

        XCTAssertEqual(recordedEncryptedSequences(in: connector), [1, 2, 1, 2])
        XCTAssertEqual(recordedEncryptedSessionIDs(in: connector), ["c2Vzc2lvbi1h", "c2Vzc2lvbi1h", "c2Vzc2lvbi1i", "c2Vzc2lvbi1i"])
    }

    func testSecureAudioStreamClientPinsCurrentRecordingToItsStartingSecureSession() async throws {
        let sessionStore = SecureAudioSessionStore()
        sessionStore.save(makeSecureAudioSession(id: "session-a", url: "ws://127.0.0.1/session-a"))
        let connector = RecordingSecureAudioWebSocketConnector()
        let client = SecureAudioStreamClient(
            sessionStore: sessionStore,
            webSocketConnector: connector
        )

        try await client.sendAudioStart(metadata: .talkaDefault)
        sessionStore.save(makeSecureAudioSession(id: "session-b", url: "ws://127.0.0.1/session-b"))
        try await client.sendAudioFrame(sequence: 1, payload: Data([1, 2, 3]))
        try await client.sendAudioStop(lastSequence: 1)

        XCTAssertEqual(recordedEncryptedSessionIDs(in: connector), ["c2Vzc2lvbi1h", "c2Vzc2lvbi1h", "c2Vzc2lvbi1h"])
        XCTAssertEqual(connector.createdTasks.map(\.url.absoluteString), ["ws://127.0.0.1/session-a"])
    }

    func testSecureAudioStreamClientWaitsForServerAcknowledgementBeforeClosingSocket() async throws {
        let sessionStore = SecureAudioSessionStore()
        sessionStore.save(makeSecureAudioSession(id: "session-a", url: "ws://127.0.0.1/session-a"))
        let connector = RecordingSecureAudioWebSocketConnector()
        let client = SecureAudioStreamClient(
            sessionStore: sessionStore,
            webSocketConnector: connector
        )

        try await client.sendAudioStart(metadata: .talkaDefault)
        let task = try XCTUnwrap(connector.createdTasks.first)
        task.queuedReceiveResults = [.success(#"{"ok":true,"final_text":"你好，世界"}"#)]

        try await client.sendAudioStop(lastSequence: 0)

        XCTAssertEqual(task.receiveCalls, 1)
        XCTAssertEqual(task.eventLog, ["resume", "send", "send", "receive", "cancel"])
    }

    func testSecureAudioStreamClientThrowsServerAudioStopError() async throws {
        let sessionStore = SecureAudioSessionStore()
        sessionStore.save(makeSecureAudioSession(id: "session-a", url: "ws://127.0.0.1/session-a"))
        let connector = RecordingSecureAudioWebSocketConnector()
        let client = SecureAudioStreamClient(
            sessionStore: sessionStore,
            webSocketConnector: connector
        )

        try await client.sendAudioStart(metadata: .talkaDefault)
        let task = try XCTUnwrap(connector.createdTasks.first)
        task.queuedReceiveResults = [.success(#"{"ok":false,"error":{"code":"accessibility_missing","message":"Talka needs Accessibility or Automation permission before it can paste into other apps."}}"#)]

        do {
            try await client.sendAudioStop(lastSequence: 0)
            XCTFail("sendAudioStop(lastSequence:) error = nil, want server error")
        } catch let error as RemoteMicDetailedError {
            XCTAssertEqual(error.code, "accessibility_missing")
            XCTAssertEqual(
                error.userMessage,
                "Talka needs Accessibility or Automation permission before it can paste into other apps."
            )
        } catch {
            XCTFail("sendAudioStop(lastSequence:) error = \(error), want RemoteMicDetailedError")
        }

        XCTAssertEqual(task.receiveCalls, 1)
        XCTAssertEqual(task.cancelCalls, 1)
    }

    func testSecureAudioStreamClientPreservesServerDiagnosticOnAudioStopError() async throws {
        let sessionStore = SecureAudioSessionStore()
        sessionStore.save(makeSecureAudioSession(id: "session-a", url: "ws://127.0.0.1/session-a"))
        let connector = RecordingSecureAudioWebSocketConnector()
        let client = SecureAudioStreamClient(
            sessionStore: sessionStore,
            webSocketConnector: connector
        )

        try await client.sendAudioStart(metadata: .talkaDefault)
        let task = try XCTUnwrap(connector.createdTasks.first)
        task.queuedReceiveResults = [.success(#"{"ok":false,"error":{"code":"asr_runtime_unavailable","message":"The Mac ASR runtime could not start.","diagnostic":"missing language model bundle: /Applications/TalkaMac.app/Contents/Resources/models/funasr/speech_ngram_lm_zh-cn-ai-wesp-fst/TLG.fst"}}"#)]

        do {
            try await client.sendAudioStop(lastSequence: 0)
            XCTFail("sendAudioStop(lastSequence:) error = nil, want server error")
        } catch let error as RemoteMicDetailedError {
            XCTAssertEqual(error.userMessage, "The Mac ASR runtime could not start.")
            XCTAssertEqual(
                error.diagnostic,
                "missing language model bundle: /Applications/TalkaMac.app/Contents/Resources/models/funasr/speech_ngram_lm_zh-cn-ai-wesp-fst/TLG.fst"
            )
        } catch {
            XCTFail("sendAudioStop(lastSequence:) error = \(error), want RemoteMicDetailedError")
        }
    }

    func testProductionEnvironmentDoesNotStartBonjourBrowsingDuringCreation() {
        let browser = FakeBonjourServiceBrowser()
        let environment = RemoteMicShellEnvironment.production(
            bonjourBrowser: browser,
            identityStore: FakePairedIdentityStore()
        )

        _ = environment.makeViewModel()

        XCTAssertEqual(browser.searchCalls, 0)
    }

    func testProductionBonjourDiscoveryStartsSearchOnlyAfterExplicitDiscoverMacsCall() async throws {
        let browser = FakeBonjourServiceBrowser()
        let discoveryBrowser = BonjourRemoteMacDiscoveryBrowser(
            browser: browser,
            discoveryTimeout: 0.01,
            resolutionTimeout: 0.01
        )

        XCTAssertEqual(browser.searchCalls, 0)
        XCTAssertNil(browser.lastSearchType)
        XCTAssertNil(browser.lastSearchDomain)

        _ = try await discoveryBrowser.discoverMacs()

        XCTAssertEqual(browser.searchCalls, 1)
        XCTAssertEqual(browser.lastSearchType, "_talka._tcp.")
        XCTAssertEqual(browser.lastSearchDomain, "local.")
    }

    func testBonjourDiscoveryRetainsResolvedServiceUntilDiscoveryFinishes() async throws {
        let browser = FakeBonjourServiceBrowser()
        let discoveryBrowser = BonjourRemoteMacDiscoveryBrowser(
            browser: browser,
            discoveryTimeout: 0.05,
            resolutionTimeout: 0.01
        )
        let discoveryTask = Task {
            try await discoveryBrowser.discoverMacs()
        }

        var service: TrackingNetService? = TrackingNetService(name: "Darluc's MacBook Pro", type: "_talka._tcp.", domain: "local.")
        let weakService = WeakReferenceBox(service)

        try await Task.sleep(nanoseconds: 5_000_000)
        browser.triggerDidFind(service: try XCTUnwrap(service), moreComing: false)
        service = nil

        XCTAssertNotNil(weakService.value)

        _ = try await discoveryTask.value

        XCTAssertNil(weakService.value)
    }

    func testUnavailablePairingClientShowsRecoverableError() async {
        let browser = FakeDiscoveryBrowser(results: [
            .success([DiscoveredMac(id: "mac-1", name: "Darluc's MacBook Pro")])
        ])
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: browser,
            sessionClient: UnavailableRemotePairingSessionClient(),
            identityStore: FakePairedIdentityStore()
        )

        await viewModel.discover()
        viewModel.pin = "123456"
        await viewModel.pairSelectedMac()

        XCTAssertEqual(viewModel.connectionState, .failedPairing)
        XCTAssertEqual(viewModel.lastErrorMessage, UnavailableRemotePairingSessionClient.pairingUnavailableMessage)
    }

    func testViewModelDoesNotStartDiscoveryDuringInitialization() {
        let browser = FakeDiscoveryBrowser()
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: browser,
            sessionClient: FakeRemoteSessionClient(),
            identityStore: FakePairedIdentityStore()
        )

        XCTAssertEqual(browser.discoverCalls, 0)
        XCTAssertEqual(viewModel.connectionState, .idle)
        XCTAssertTrue(viewModel.discoveredMacs.isEmpty)
        XCTAssertNil(viewModel.currentMacName)
    }

    func testInitializationLoadsKnownPairedMacWithoutStartingDiscovery() {
        let browser = FakeDiscoveryBrowser()
        let identity = PairedMacIdentity(deviceID: "mac-1", deviceName: "Darluc's MacBook Pro")
        let store = FakePairedIdentityStore(initialIdentity: identity)

        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: browser,
            sessionClient: FakeRemoteSessionClient(),
            identityStore: store
        )

        XCTAssertEqual(browser.discoverCalls, 0)
        XCTAssertEqual(viewModel.currentMacName, "Darluc's MacBook Pro")
        XCTAssertEqual(viewModel.knownMacName, "Darluc's MacBook Pro")
        XCTAssertTrue(viewModel.canReconnect)
    }

    func testDiscoverAfterExplicitActionShowsAvailableMacs() async {
        let browser = FakeDiscoveryBrowser(results: [
            .success([
                DiscoveredMac(id: "mac-2", name: "Studio Mac"),
                DiscoveredMac(id: "mac-1", name: "Darluc's MacBook Pro")
            ])
        ])
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: browser,
            sessionClient: FakeRemoteSessionClient(),
            identityStore: FakePairedIdentityStore()
        )

        await viewModel.discover()

        XCTAssertEqual(browser.discoverCalls, 1)
        XCTAssertEqual(viewModel.connectionState, .discovered)
        XCTAssertEqual(viewModel.discoveredMacs.map(\.name), ["Darluc's MacBook Pro", "Studio Mac"])
        XCTAssertEqual(viewModel.selectedMacName, "Darluc's MacBook Pro")
        XCTAssertNil(viewModel.lastErrorMessage)
    }

    func testDiscoverHandlesLocalNetworkDenied() async {
        let browser = FakeDiscoveryBrowser(results: [.failure(RemoteMicFlowError.localNetworkDenied)])
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: browser,
            sessionClient: FakeRemoteSessionClient(),
            identityStore: FakePairedIdentityStore()
        )

        await viewModel.discover()

        XCTAssertEqual(viewModel.connectionState, .localNetworkDenied)
        XCTAssertTrue(viewModel.discoveredMacs.isEmpty)
        XCTAssertEqual(viewModel.lastErrorMessage, "Local network access was denied. Tap Discover again after allowing Talka in Settings.")
    }

    func testPairSelectedMacWithCorrectPINStoresIdentity() async throws {
        let browser = FakeDiscoveryBrowser(results: [
            .success([DiscoveredMac(id: "mac-1", name: "Darluc's MacBook Pro")])
        ])
        let sessionClient = FakeRemoteSessionClient(pairResults: [
            .success(PairedMacIdentity(deviceID: "mac-1", deviceName: "Darluc's MacBook Pro"))
        ])
        let store = FakePairedIdentityStore()
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: browser,
            sessionClient: sessionClient,
            identityStore: store
        )

        await viewModel.discover()
        viewModel.pin = "123456"
        await viewModel.pairSelectedMac()

        XCTAssertEqual(viewModel.connectionState, .paired)
        XCTAssertEqual(viewModel.currentMacName, "Darluc's MacBook Pro")
        XCTAssertEqual(viewModel.knownMacName, "Darluc's MacBook Pro")
        XCTAssertEqual(store.identity, PairedMacIdentity(deviceID: "mac-1", deviceName: "Darluc's MacBook Pro"))
        XCTAssertEqual(sessionClient.pairedPins, ["123456"])
        XCTAssertNil(viewModel.lastErrorMessage)
    }

    func testPairSelectedMacShowsWrongPINError() async {
        let browser = FakeDiscoveryBrowser(results: [
            .success([DiscoveredMac(id: "mac-1", name: "Darluc's MacBook Pro")])
        ])
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: browser,
            sessionClient: FakeRemoteSessionClient(pairResults: [.failure(RemoteMicFlowError.wrongPIN)]),
            identityStore: FakePairedIdentityStore()
        )

        await viewModel.discover()
        viewModel.pin = "999999"
        await viewModel.pairSelectedMac()

        XCTAssertEqual(viewModel.connectionState, .failedPairing)
        XCTAssertEqual(viewModel.lastErrorMessage, "The PIN was incorrect. Check the Mac and try again.")
    }

    func testPairSelectedMacShowsExpiredPINError() async {
        let browser = FakeDiscoveryBrowser(results: [
            .success([DiscoveredMac(id: "mac-1", name: "Darluc's MacBook Pro")])
        ])
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: browser,
            sessionClient: FakeRemoteSessionClient(pairResults: [.failure(RemoteMicFlowError.expiredPIN)]),
            identityStore: FakePairedIdentityStore()
        )

        await viewModel.discover()
        viewModel.pin = "123456"
        await viewModel.pairSelectedMac()

        XCTAssertEqual(viewModel.connectionState, .failedPairing)
        XCTAssertEqual(viewModel.lastErrorMessage, "This PIN expired. Ask the Mac for a fresh code and reconnect.")
    }

    func testReconnectKnownMacAfterServerRestartUsesStoredIdentity() async {
        let identity = PairedMacIdentity(deviceID: "mac-1", deviceName: "Darluc's MacBook Pro")
        let sessionClient = FakeRemoteSessionClient(reconnectResults: [.success(identity)])
        let store = FakePairedIdentityStore(initialIdentity: identity)
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: FakeDiscoveryBrowser(),
            sessionClient: sessionClient,
            identityStore: store
        )

        await viewModel.reconnectToKnownMac()

        XCTAssertEqual(viewModel.connectionState, .paired)
        XCTAssertEqual(viewModel.currentMacName, "Darluc's MacBook Pro")
        XCTAssertEqual(sessionClient.reconnectCalls, 1)
    }

    func testForgetDeviceClearsStoredIdentityAndResetsShell() async {
        let identity = PairedMacIdentity(deviceID: "mac-1", deviceName: "Darluc's MacBook Pro")
        let browser = FakeDiscoveryBrowser(results: [
            .success([DiscoveredMac(id: "mac-1", name: "Darluc's MacBook Pro")])
        ])
        let store = FakePairedIdentityStore(initialIdentity: identity)
        var clearedSecureSessions = 0
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: browser,
            sessionClient: FakeRemoteSessionClient(),
            identityStore: store,
            disconnectSecureSession: {
                clearedSecureSessions += 1
            }
        )

        await viewModel.discover()
        await viewModel.forgetKnownMac()

        XCTAssertEqual(viewModel.connectionState, .forgotten)
        XCTAssertNil(viewModel.currentMacName)
        XCTAssertNil(viewModel.knownMacName)
        XCTAssertNil(store.identity)
        XCTAssertNil(viewModel.selectedMacName)
        XCTAssertTrue(viewModel.discoveredMacs.isEmpty)
        XCTAssertEqual(clearedSecureSessions, 1)
    }

    func testPairSelectedMacShowsGenericFailureMessage() async {
        let browser = FakeDiscoveryBrowser(results: [
            .success([DiscoveredMac(id: "mac-1", name: "Darluc's MacBook Pro")])
        ])
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: browser,
            sessionClient: FakeRemoteSessionClient(pairResults: [.failure(RemoteMicFlowError.pairingFailed("Server unavailable"))]),
            identityStore: FakePairedIdentityStore()
        )

        await viewModel.discover()
        viewModel.pin = "123456"
        await viewModel.pairSelectedMac()

        XCTAssertEqual(viewModel.connectionState, .failedPairing)
        XCTAssertEqual(viewModel.lastErrorMessage, "Server unavailable")
    }
}

private final class FakeDiscoveryBrowser: RemoteMacDiscovering {
    private var results: [Result<[DiscoveredMac], Error>]
    private(set) var discoverCalls = 0

    init(results: [Result<[DiscoveredMac], Error>] = [.success([])]) {
        self.results = results
    }

    func discoverMacs() async throws -> [DiscoveredMac] {
        discoverCalls += 1
        return try next()
    }

    private func next() throws -> [DiscoveredMac] {
        let result = queueResult()
        return try result.get()
    }

    private func queueResult() -> Result<[DiscoveredMac], Error> {
        if results.isEmpty {
            return .success([])
        }
        return results.removeFirst()
    }
}

private func renderedViewStrings(in value: Any) -> [String] {
    collectStrings(in: value, depth: 0)
}

private func collectStrings(in value: Any, depth: Int) -> [String] {
    guard depth < 80 else { return [] }

    if let string = value as? String {
        return [string]
    }

    let mirror = Mirror(reflecting: value)
    if mirror.displayStyle == .class {
        return []
    }

    return mirror.children.flatMap { child in
        collectStrings(in: child.value, depth: depth + 1)
    }
}

private final class FakeRemoteSessionClient: RemotePairingSessioning {
    private var pairResults: [Result<PairedMacIdentity, Error>]
    private var reconnectResults: [Result<PairedMacIdentity, Error>]
    private(set) var reconnectCalls = 0
    private(set) var pairedPins: [String] = []

    init(
        pairResults: [Result<PairedMacIdentity, Error>] = [.success(PairedMacIdentity(deviceID: "mac-1", deviceName: "Darluc's MacBook Pro"))],
        reconnectResults: [Result<PairedMacIdentity, Error>] = [.success(PairedMacIdentity(deviceID: "mac-1", deviceName: "Darluc's MacBook Pro"))]
    ) {
        self.pairResults = pairResults
        self.reconnectResults = reconnectResults
    }

    func pair(with mac: DiscoveredMac, pin: String) async throws -> PairedMacIdentity {
        _ = mac
        pairedPins.append(pin)
        return try nextPairResult().get()
    }

    func reconnect(using identity: PairedMacIdentity) async throws -> PairedMacIdentity {
        _ = identity
        reconnectCalls += 1
        return try nextReconnectResult().get()
    }

    private func nextPairResult() -> Result<PairedMacIdentity, Error> {
        if pairResults.isEmpty {
            return .failure(RemoteMicFlowError.pairingFailed("Missing fake pair result"))
        }
        return pairResults.removeFirst()
    }

    private func nextReconnectResult() -> Result<PairedMacIdentity, Error> {
        if reconnectResults.isEmpty {
            return .failure(RemoteMicFlowError.pairingFailed("Missing fake reconnect result"))
        }
        return reconnectResults.removeFirst()
    }
}

private final class FakePairedIdentityStore: PairedIdentityStoring {
    private(set) var identity: PairedMacIdentity?

    init(initialIdentity: PairedMacIdentity? = nil) {
        identity = initialIdentity
    }

    func loadPairedIdentity() throws -> PairedMacIdentity? {
        identity
    }

    func savePairedIdentity(_ identity: PairedMacIdentity) throws {
        self.identity = identity
    }

    func clearPairedIdentity() throws {
        identity = nil
    }
}

    private final class RecordingAudioStreamClient: AudioStreamClient {
        private let lock = NSLock()
        private var recordedEvents: [AudioStreamEvent] = []

        var events: [AudioStreamEvent] {
            lock.withLock { recordedEvents }
        }

        func sendAudioStart(metadata: AudioStreamMetadata) async throws {
            lock.withLock {
                recordedEvents.append(.start(metadata))
            }
        }

        func sendAudioFrame(sequence: Int, payload: Data) async throws {
            lock.withLock {
                recordedEvents.append(.frame(sequence: sequence, byteCount: payload.count))
            }
        }

        func sendAudioStop(lastSequence: Int) async throws {
            lock.withLock {
                recordedEvents.append(.stop(lastSequence: lastSequence))
            }
        }

        func sendAudioCancel(reason: String) async throws {
            lock.withLock {
                recordedEvents.append(.cancel(reason: reason))
            }
        }
    }

    private final class FailingAudioStreamClient: AudioStreamClient {
        private let startError: Error?
        private let frameError: Error?

        init(startError: Error? = nil, frameError: Error? = nil) {
            self.startError = startError
            self.frameError = frameError
        }

        func sendAudioStart(metadata: AudioStreamMetadata) async throws {
            _ = metadata
            if let startError {
                throw startError
            }
        }

        func sendAudioFrame(sequence: Int, payload: Data) async throws {
            _ = sequence
            _ = payload
            if let frameError {
                throw frameError
            }
        }

        func sendAudioStop(lastSequence: Int) async throws {
            _ = lastSequence
        }

        func sendAudioCancel(reason: String) async throws {
            _ = reason
        }
    }

    private final class BlockingStopAudioStreamClient: AudioStreamClient {
        private let lock = NSLock()
        private var recordedEvents: [AudioStreamEvent] = []
        private var recordedStopStarted = false
        private var recordedLateFrameAttempted = false
        private var stopContinuation: CheckedContinuation<Void, Never>?

        var events: [AudioStreamEvent] {
            lock.withLock { recordedEvents }
        }

        var stopStarted: Bool {
            lock.withLock { recordedStopStarted }
        }

        var lateFrameAttempted: Bool {
            lock.withLock { recordedLateFrameAttempted }
        }

        func sendAudioStart(metadata: AudioStreamMetadata) async throws {
            lock.withLock {
                recordedEvents.append(.start(metadata))
            }
        }

        func sendAudioFrame(sequence: Int, payload: Data) async throws {
            let shouldRejectFrame = lock.withLock {
                if recordedStopStarted {
                    recordedLateFrameAttempted = true
                    return true
                }
                recordedEvents.append(.frame(sequence: sequence, byteCount: payload.count))
                return false
            }
            if shouldRejectFrame {
                throw RemoteMicFlowError.recordingFailed("audio session could not be processed")
            }
        }

        func sendAudioStop(lastSequence: Int) async throws {
            await withCheckedContinuation { continuation in
                lock.withLock {
                    recordedEvents.append(.stop(lastSequence: lastSequence))
                    recordedStopStarted = true
                    stopContinuation = continuation
                }
            }
        }

        func sendAudioCancel(reason: String) async throws {
            _ = reason
        }

        func finishStop() {
            let continuation = lock.withLock {
                let continuation = stopContinuation
                stopContinuation = nil
                return continuation
            }
            continuation?.resume()
        }
    }

    private final class BlockingFrameAudioStreamClient: AudioStreamClient {
        private(set) var frameStarted = false
        private(set) var stopStarted = false
        private(set) var stopLastSequence: Int?
        private var frameContinuation: CheckedContinuation<Void, Error>?

        func sendAudioStart(metadata: AudioStreamMetadata) async throws {
            _ = metadata
        }

        func sendAudioFrame(sequence: Int, payload: Data) async throws {
            _ = sequence
            _ = payload
            frameStarted = true
            try await withCheckedThrowingContinuation { continuation in
                frameContinuation = continuation
            }
        }

        func sendAudioStop(lastSequence: Int) async throws {
            stopLastSequence = lastSequence
            stopStarted = true
        }

        func sendAudioCancel(reason: String) async throws {
            _ = reason
        }

        func failBlockedFrameAfterStop() {
            frameContinuation?.resume(throwing: RemoteMicFlowError.recordingFailed("audio session could not be processed"))
            frameContinuation = nil
        }
    }

    private struct ThrowingAudioPCMConverter: AudioPCMConverting {
        let error: Error

        func convert(_ inputBuffer: AVAudioPCMBuffer) throws -> Data {
            _ = inputBuffer
            throw error
        }
    }

    private final class RecordingAudioSession: AudioSessionControlling {
        private let events: (String) -> Void
        private(set) var didConfigureForRecording = false

        init(events: @escaping (String) -> Void = { _ in }) {
            self.events = events
        }

        func configureForRecording() throws {
            didConfigureForRecording = true
            events("configure-session")
        }

        func diagnosticInfo() -> String {
            "permission=granted, inputAvailable=true, sampleRate=44100.0, inputChannels=1, inputs=[FakeMicrophone[builtInMic]], outputs=[FakeOutput[receiver]]"
        }
    }

    private final class FakeAudioEngine: AudioEngineControlling {
        let inputFormat: AVAudioFormat
        private let events: (String) -> Void
        private(set) var installedTapFormat: AVAudioFormat?
        private(set) var didStart = false
        private(set) var didStop = false
        private var tapBlock: AVAudioNodeTapBlock?

    init(inputFormat: AVAudioFormat, events: @escaping (String) -> Void = { _ in }) {
        self.inputFormat = inputFormat
        self.events = events
    }

        func installInputTap(bufferSize: AVAudioFrameCount, format: AVAudioFormat, block: @escaping AVAudioNodeTapBlock) {
            _ = bufferSize
            events("install-tap")
            installedTapFormat = format
            tapBlock = block
        }

    func removeInputTap() {
        installedTapFormat = nil
    }

    func start() throws {
        events("start-engine")
        didStart = true
    }

        func stop() {
            didStop = true
        }

        func emit(_ buffer: AVAudioPCMBuffer) {
            tapBlock?(buffer, AVAudioTime(hostTime: 0))
        }
    }

    private final class FakeMicrophoneSource: AudioCaptureSourcing {
        private var onPCM: ((Data) -> Void)?
        private(set) var didStart = false
        private(set) var didStop = false

    func start(onPCM: @escaping (Data) -> Void) throws {
        self.onPCM = onPCM
        didStart = true
    }

    func stop() {
        didStop = true
    }

    func emit(_ pcm: Data) {
        onPCM?(pcm)
    }
}

private extension MicrophonePCMSource {
    var installedTapFormat: AVAudioFormat? {
        (engine as? FakeAudioEngine)?.installedTapFormat
    }
}

private enum AudioStreamEvent: Equatable {
    case start(AudioStreamMetadata)
    case frame(sequence: Int, byteCount: Int)
    case stop(lastSequence: Int)
    case cancel(reason: String)
}

private final class RecordingSecureAudioWebSocketConnector: SecureAudioWebSocketConnecting {
    private(set) var createdTasks: [RecordingSecureAudioWebSocketTask] = []

    func makeWebSocketTask(url: URL) -> SecureAudioWebSocketTasking {
        let task = RecordingSecureAudioWebSocketTask(url: url)
        createdTasks.append(task)
        return task
    }
}

private final class RecordingSecureAudioWebSocketTask: SecureAudioWebSocketTasking {
    let url: URL
    private(set) var didResume = false
    private(set) var sentTexts: [String] = []
    private(set) var cancelCalls = 0
    private(set) var receiveCalls = 0
    private(set) var eventLog: [String] = []
    var queuedReceiveResults: [Result<String, Error>] = []

    init(url: URL) {
        self.url = url
    }

    func resume() {
        didResume = true
        eventLog.append("resume")
    }

    func send(_ text: String) async throws {
        sentTexts.append(text)
        eventLog.append("send")
    }

    func receive() async throws -> String {
        receiveCalls += 1
        eventLog.append("receive")
        if queuedReceiveResults.isEmpty {
            return #"{"ok":true}"#
        }
        return try queuedReceiveResults.removeFirst().get()
    }

    func cancel(with closeCode: URLSessionWebSocketTask.CloseCode, reason: Data?) {
        _ = closeCode
        _ = reason
        cancelCalls += 1
        eventLog.append("cancel")
    }
}

private func makeSecureAudioSession(id: String, url: String) -> SecureAudioSessionKeys {
    SecureAudioSessionKeys(
        sessionID: Data(id.utf8),
        clientToServerKey: SymmetricKey(data: Data(repeating: 7, count: 32)),
        audioWebSocketURL: URL(string: url)!
    )
}

private func recordedEncryptedSequences(in connector: RecordingSecureAudioWebSocketConnector) -> [UInt64] {
    connector.createdTasks
        .flatMap(\.sentTexts)
        .compactMap(decodeEncryptedAudioMessage)
        .map(\.seq)
}

private func recordedEncryptedSessionIDs(in connector: RecordingSecureAudioWebSocketConnector) -> [String] {
    connector.createdTasks
        .flatMap(\.sentTexts)
        .compactMap(decodeEncryptedAudioMessage)
        .map(\.sessionID)
}

private func decodeEncryptedAudioMessage(_ text: String) -> SecureEncryptedAudioMessage? {
    guard
        let payload = try? JSONSerialization.jsonObject(with: Data(text.utf8)) as? [String: Any],
        let versionNumber = payload["version"] as? NSNumber,
        let sessionID = payload["session_id"] as? String,
        let seqNumber = payload["seq"] as? NSNumber,
        let type = payload["type"] as? String,
        let nonce = payload["nonce"] as? String,
        let ciphertext = payload["ciphertext"] as? String,
        let tag = payload["tag"] as? String
    else {
        return nil
    }
    return SecureEncryptedAudioMessage(
        version: versionNumber.uint8Value,
        sessionID: sessionID,
        seq: seqNumber.uint64Value,
        type: type,
        nonce: nonce,
        ciphertext: ciphertext,
        tag: tag
    )
}

private final class FakeBonjourServiceBrowser: BonjourNetServiceBrowsing {
    weak var delegate: NetServiceBrowserDelegate?
    private(set) var searchCalls = 0
    private(set) var lastSearchType: String?
    private(set) var lastSearchDomain: String?

    func searchForServices(ofType type: String, inDomain domainString: String) {
        searchCalls += 1
        lastSearchType = type
        lastSearchDomain = domainString
    }

    func stop() {}

    func triggerDidFind(service: NetService, moreComing: Bool) {
        let browser = NetServiceBrowser()
        delegate?.netServiceBrowser?(browser, didFind: service, moreComing: moreComing)
    }
}

private final class TrackingNetService: NetService {
    init(name: String, type: String, domain: String) {
        super.init(domain: domain, type: type, name: name, port: 0)
    }
}

private final class WeakReferenceBox<Object: AnyObject> {
    weak var value: Object?

    init(_ value: Object?) {
        self.value = value
    }
}
