import AVFoundation
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

        let renderedStrings = renderedViewStrings(in: ContentView(viewModel: viewModel).body)

        XCTAssertTrue(renderedStrings.contains("bootstrap exploded"), renderedStrings.joined(separator: "\n"))
        XCTAssertTrue(renderedStrings.contains("Audio diagnostic: WebSocket bootstrap/sendAudioStart failed: bootstrap exploded"), renderedStrings.joined(separator: "\n"))
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

    func testStopRecordingStopsCaptureAndSendsLastSequence() async {
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
        XCTAssertEqual(streamClient.events, [
            .start(.talkaDefault),
            .frame(sequence: 1, byteCount: TalkaAudioFormat.frameByteCount),
            .stop(lastSequence: 1)
        ])
        XCTAssertEqual(viewModel.recordingState, .idle)
    }

    func testProductionEnvironmentUsesBonjourDiscoveryAndSecureTransportDefaults() {
        let environment = RemoteMicShellEnvironment.production()

        XCTAssertTrue(environment.discoveryBrowser is BonjourRemoteMacDiscoveryBrowser)
        XCTAssertTrue(environment.sessionClient is SecureRemotePairingSessionClient)
        XCTAssertTrue(environment.audioStreamClient is SecureAudioStreamClient)
        XCTAssertTrue(environment.identityStore is KeychainPairedIdentityStore)
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
        let viewModel = RemoteMicShellViewModel(
            discoveryBrowser: browser,
            sessionClient: FakeRemoteSessionClient(),
            identityStore: store
        )

        await viewModel.discover()
        await viewModel.forgetKnownMac()

        XCTAssertEqual(viewModel.connectionState, .forgotten)
        XCTAssertNil(viewModel.currentMacName)
        XCTAssertNil(viewModel.knownMacName)
        XCTAssertNil(store.identity)
        XCTAssertNil(viewModel.selectedMacName)
        XCTAssertTrue(viewModel.discoveredMacs.isEmpty)
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
        private(set) var events: [AudioStreamEvent] = []

    func sendAudioStart(metadata: AudioStreamMetadata) async throws {
        events.append(.start(metadata))
    }

    func sendAudioFrame(sequence: Int, payload: Data) async throws {
        events.append(.frame(sequence: sequence, byteCount: payload.count))
    }

    func sendAudioStop(lastSequence: Int) async throws {
        events.append(.stop(lastSequence: lastSequence))
    }

        func sendAudioCancel(reason: String) async throws {
            events.append(.cancel(reason: reason))
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
