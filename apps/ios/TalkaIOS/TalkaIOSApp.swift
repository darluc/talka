import AVFoundation
import CryptoKit
import Foundation
import Security
import SwiftUI
import UIKit

enum RemoteMicShellMetrics {
    static let panelSpacing: CGFloat = 18
    static let sectionSpacing: CGFloat = 12
    static let contentPadding: CGFloat = 20
    static let cornerRadius: CGFloat = 12
}

enum RemoteMicConnectionState: String, Equatable {
    case idle
    case discovering
    case discovered
    case pairing
    case paired
    case reconnecting
    case failedPairing
    case localNetworkDenied
    case forgotten

    var title: String {
        switch self {
        case .idle:
            return "Idle"
        case .discovering:
            return "Discovering"
        case .discovered:
            return "Ready to Pair"
        case .pairing:
            return "Pairing"
        case .paired:
            return "Connected"
        case .reconnecting:
            return "Reconnecting"
        case .failedPairing:
            return "Pairing Failed"
        case .localNetworkDenied:
            return "Local Network Denied"
        case .forgotten:
            return "Forgotten"
        }
    }

    var detail: String {
        switch self {
        case .idle:
            return "Tap Discover Macs before iOS asks for local network access."
        case .discovering:
            return "Looking for nearby Macs advertising Talka over Bonjour."
        case .discovered:
            return "Choose a Mac, enter its six-digit PIN, and connect deliberately."
        case .pairing:
            return "Confirming this iPhone with the selected Mac."
        case .paired:
            return "The remembered Mac is available for the next microphone task."
        case .reconnecting:
            return "Trying the saved Mac again after a restart or app relaunch."
        case .failedPairing:
            return "Check the selected Mac and try pairing again with a fresh PIN if needed."
        case .localNetworkDenied:
            return "Allow local network access for Talka, then tap Discover Macs again."
        case .forgotten:
            return "The saved Mac was removed from this iPhone."
        }
    }

    var symbolName: String {
        switch self {
        case .idle:
            return "dot.radiowaves.left.and.right"
        case .discovering:
            return "magnifyingglass"
        case .discovered:
            return "desktopcomputer"
        case .pairing:
            return "key.horizontal"
        case .paired:
            return "checkmark.shield.fill"
        case .reconnecting:
            return "arrow.clockwise.circle"
        case .failedPairing:
            return "exclamationmark.triangle.fill"
        case .localNetworkDenied:
            return "wifi.exclamationmark"
        case .forgotten:
            return "trash"
        }
    }

    var tint: Color {
        switch self {
        case .idle, .discovering, .discovered, .reconnecting:
            return .blue
        case .pairing:
            return .orange
        case .paired:
            return .green
        case .failedPairing, .localNetworkDenied:
            return .red
        case .forgotten:
            return .secondary
        }
    }
}

enum RemoteMicRecordingState: String, Equatable {
    case idle
    case recording
    case stopping
    case failed
}

enum ReturnKeyModifier: String, CaseIterable, Hashable {
    case command
    case alt
    case shift

    var title: String {
        switch self {
        case .command:
            return "Cmd"
        case .alt:
            return "Alt"
        case .shift:
            return "Shift"
        }
    }

    var symbol: String {
        switch self {
        case .command:
            return "⌘"
        case .alt:
            return "⌥"
        case .shift:
            return "⇧"
        }
    }

    var wireValue: String {
        switch self {
        case .command:
            return "cmd"
        case .alt:
            return "alt"
        case .shift:
            return "shift"
        }
    }
}

enum ReturnKeySendState: Equatable {
    case idle
    case sending
    case sent
    case failed
}

enum RemoteMicPowerToggleResult: Equatable {
    case none
    case showPairingPanel
}

struct DiscoveredMac: Identifiable, Equatable {
    var id: String
    var name: String
    var hostName: String? = nil
    var port: Int? = nil
    var serviceType: String? = nil
}

struct PairedMacIdentity: Codable, Equatable {
    var deviceID: String
    var deviceName: String
    var hostName: String? = nil
    var port: Int? = nil
    var serverDeviceID: String? = nil
    var serverDeviceName: String? = nil
    var clientIdentityPrivateKey: String? = nil
    var clientIdentityPublicKey: String? = nil
    var serverIdentityPublicKey: String? = nil
}

extension PairedMacIdentity {
    var macDisplayName: String? {
        guard let serverDeviceName, !serverDeviceName.isEmpty else {
            return nil
        }
        return serverDeviceName
    }
}

enum TalkaAudioFormat {
    static let sampleRate = 16_000
    static let channels = 1
    static let encoding = "pcm_s16le"
    static let frameDurationMS = 20
    static let bytesPerSample = 2
    static let frameByteCount = sampleRate * frameDurationMS / 1_000 * channels * bytesPerSample
    static let captureTapBufferSize: AVAudioFrameCount = 256
}

struct AudioStreamMetadata: Equatable {
    var sampleRate: Int
    var channels: Int
    var encoding: String
    var frameDurationMS: Int
    var language: String

    static let talkaDefault = AudioStreamMetadata(
        sampleRate: TalkaAudioFormat.sampleRate,
        channels: TalkaAudioFormat.channels,
        encoding: TalkaAudioFormat.encoding,
        frameDurationMS: TalkaAudioFormat.frameDurationMS,
        language: "zh-CN"
    )
}

enum RemoteMicFlowError: LocalizedError, Equatable {
    case localNetworkDenied
    case wrongPIN
    case expiredPIN
    case pairingFailed(String)
    case reconnectFailed(String)
    case recordingFailed(String)
    case audioBackpressure
    case noSelectedMac
    case noKnownMac

    var errorDescription: String? {
        switch self {
        case .localNetworkDenied:
            return "Local network access was denied. Tap Discover again after allowing Talka in Settings."
        case .wrongPIN:
            return "The PIN was incorrect. Check the Mac and try again."
        case .expiredPIN:
            return "This PIN expired. Ask the Mac for a fresh code and reconnect."
        case let .pairingFailed(message):
            return message
        case let .reconnectFailed(message):
            return message
        case let .recordingFailed(message):
            return message
        case .audioBackpressure:
            return "Audio streaming stalled. Recording stopped safely."
        case .noSelectedMac:
            return "Choose a Mac before entering its PIN."
        case .noKnownMac:
            return "There is no remembered Mac to reconnect."
        }
    }
}

struct RemoteMicDetailedError: LocalizedError, Equatable {
    let code: String
    let userMessage: String
    let diagnostic: String?

    var errorDescription: String? {
        userMessage
    }
}

protocol RemoteMacDiscovering {
    func discoverMacs() async throws -> [DiscoveredMac]
}

protocol RemotePairingSessioning {
    func pair(with mac: DiscoveredMac, pin: String) async throws -> PairedMacIdentity
    func reconnect(using identity: PairedMacIdentity) async throws -> PairedMacIdentity
}

protocol PairedIdentityStoring {
    func loadPairedIdentity() throws -> PairedMacIdentity?
    func savePairedIdentity(_ identity: PairedMacIdentity) throws
    func clearPairedIdentity() throws
}

protocol AudioStreamClient {
    func sendAudioStart(metadata: AudioStreamMetadata) async throws
    func sendAudioFrame(sequence: Int, payload: Data) async throws
    func sendAudioStop(lastSequence: Int) async throws
    func sendAudioCancel(reason: String) async throws
    func sendKeyPress(key: String, modifiers: [ReturnKeyModifier]) async throws
}

protocol MacConnectionMonitoring {
    func waitUntilDisconnected() async
}

struct NoopMacConnectionMonitor: MacConnectionMonitoring {
    func waitUntilDisconnected() async {
        while !Task.isCancelled {
            do {
                try await Task.sleep(nanoseconds: 60_000_000_000)
            } catch {
                return
            }
        }
    }
}

protocol AudioCaptureSourcing: AnyObject {
    func start(onPCM: @escaping (Data) -> Void) throws
    func stop()
}

struct PCMFrameAccumulator {
    let frameByteCount: Int
    private var buffer = Data()

    init(frameByteCount: Int) {
        self.frameByteCount = frameByteCount
    }

    var bufferedByteCount: Int { buffer.count }

    mutating func append(_ data: Data) -> [Data] {
        buffer.append(data)
        var frames: [Data] = []

        while buffer.count >= frameByteCount {
            frames.append(buffer.prefix(frameByteCount))
            buffer.removeFirst(frameByteCount)
        }

        return frames
    }

    mutating func flushPaddedFrame() -> Data? {
        guard !buffer.isEmpty else {
            return nil
        }

        var frame = buffer
        buffer.removeAll(keepingCapacity: true)
        if frame.count < frameByteCount {
            frame.append(Data(repeating: 0, count: frameByteCount - frame.count))
        }
        return frame
    }
}

enum PCMVolumeLevel {
    static func level(for pcm: Data) -> Double {
        guard pcm.count >= MemoryLayout<Int16>.size else {
            return 0
        }

        var squaredSum = 0.0
        var sampleCount = 0
        var byteIndex = 0

        let bytes = [UInt8](pcm)

        while byteIndex + 1 < bytes.count {
            let low = UInt16(bytes[byteIndex])
            let high = UInt16(bytes[byteIndex + 1]) << 8
            let sample = Int16(bitPattern: high | low)
            let normalizedSample = Double(sample) / Double(Int16.max)
            squaredSum += normalizedSample * normalizedSample
            sampleCount += 1
            byteIndex += MemoryLayout<Int16>.size
        }

        guard sampleCount > 0 else {
            return 0
        }

        return min(max(sqrt(squaredSum / Double(sampleCount)), 0), 1)
    }
}

struct BoundedAudioFrameQueue {
    let maxFrames: Int
    private var frames: [Data] = []

    init(maxFrames: Int) {
        self.maxFrames = maxFrames
    }

    mutating func enqueue(_ newFrames: [Data]) throws {
        guard frames.count + newFrames.count <= maxFrames else {
            throw RemoteMicFlowError.audioBackpressure
        }
        frames.append(contentsOf: newFrames)
    }

    mutating func drain() -> [Data] {
        let drained = frames
        frames.removeAll(keepingCapacity: true)
        return drained
    }
}

final class PendingPCMChunkBuffer {
    private let lock = NSLock()
    private var chunks: [Data] = []

    func append(_ chunk: Data) {
        lock.withLock {
            chunks.append(chunk)
        }
    }

    func drain() -> [Data] {
        lock.withLock {
            let drained = chunks
            chunks.removeAll(keepingCapacity: true)
            return drained
        }
    }

    func removeAll() {
        lock.withLock {
            chunks.removeAll(keepingCapacity: true)
        }
    }
}

protocol AudioEngineControlling: AnyObject {
    var inputFormat: AVAudioFormat { get }
    func installInputTap(bufferSize: AVAudioFrameCount, format: AVAudioFormat, block: @escaping AVAudioNodeTapBlock)
    func removeInputTap()
    func start() throws
    func stop()
}

protocol AudioPCMConverting {
    func convert(_ inputBuffer: AVAudioPCMBuffer) throws -> Data
}

protocol AudioSessionControlling {
    func configureForRecording() throws
    func diagnosticInfo() -> String
}

struct SystemAudioSession: AudioSessionControlling {
    func configureForRecording() throws {
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.record, mode: .measurement, options: [])
        try session.setActive(true)
    }

    func diagnosticInfo() -> String {
        let session = AVAudioSession.sharedInstance()
        let permission: String
        switch session.recordPermission {
        case .granted: permission = "granted"
        case .denied: permission = "denied"
        case .undetermined: permission = "undetermined"
        @unknown default: permission = "unknown"
        }
        let inputAvailable = session.isInputAvailable
        let currentRoute = session.currentRoute
        let inputs = currentRoute.inputs.map { "\($0.portName)[\($0.portType.rawValue)]" }.joined(separator: ", ")
        let outputs = currentRoute.outputs.map { "\($0.portName)[\($0.portType.rawValue)]" }.joined(separator: ", ")
        let sampleRate = session.sampleRate
        let inputNumberOfChannels = session.inputNumberOfChannels
        return "permission=\(permission), inputAvailable=\(inputAvailable), sampleRate=\(sampleRate), inputChannels=\(inputNumberOfChannels), inputs=[\(inputs)], outputs=[\(outputs)]"
    }
}

final class SystemAudioEngine: AudioEngineControlling {
    private let engine = AVAudioEngine()

    var inputFormat: AVAudioFormat {
        engine.inputNode.inputFormat(forBus: 0)
    }

    func installInputTap(bufferSize: AVAudioFrameCount, format: AVAudioFormat, block: @escaping AVAudioNodeTapBlock) {
        engine.inputNode.installTap(onBus: 0, bufferSize: bufferSize, format: format, block: block)
    }

    func removeInputTap() {
        engine.inputNode.removeTap(onBus: 0)
    }

    func start() throws {
        try engine.start()
    }

    func stop() {
        engine.stop()
    }
}

struct AudioPCMConverter: AudioPCMConverting {
    private let outputFormat: AVAudioFormat

    init() {
        outputFormat = AVAudioFormat(
            commonFormat: .pcmFormatInt16,
            sampleRate: Double(TalkaAudioFormat.sampleRate),
            channels: AVAudioChannelCount(TalkaAudioFormat.channels),
            interleaved: true
        )!
    }

    func convert(_ inputBuffer: AVAudioPCMBuffer) throws -> Data {
        guard let converter = AVAudioConverter(from: inputBuffer.format, to: outputFormat) else {
            throw RemoteMicFlowError.recordingFailed("Audio converter could not be created.")
        }

        let ratio = outputFormat.sampleRate / inputBuffer.format.sampleRate
        let outputCapacity = AVAudioFrameCount(max(1, Int(Double(inputBuffer.frameLength) * ratio.rounded(.up)) + 8))
        guard let outputBuffer = AVAudioPCMBuffer(pcmFormat: outputFormat, frameCapacity: outputCapacity) else {
            throw RemoteMicFlowError.recordingFailed("Audio output buffer could not be allocated.")
        }

        var didProvideInput = false
        var conversionError: NSError?
        let status = converter.convert(to: outputBuffer, error: &conversionError) { _, outStatus in
            if didProvideInput {
                outStatus.pointee = .endOfStream
                return nil
            }
            didProvideInput = true
            outStatus.pointee = .haveData
            return inputBuffer
        }

        if let conversionError {
            throw conversionError
        }
        guard status != AVAudioConverterOutputStatus.error else {
            throw RemoteMicFlowError.recordingFailed("Audio conversion failed.")
        }

        let audioBuffer = outputBuffer.audioBufferList.pointee.mBuffers
        guard let dataPointer = audioBuffer.mData else {
            return Data()
        }
        return Data(bytes: dataPointer, count: Int(audioBuffer.mDataByteSize))
    }
}

final class MicrophonePCMSource: AudioCaptureSourcing {
    let engine: AudioEngineControlling
    private let converter: AudioPCMConverting
    private let audioSession: AudioSessionControlling
    var diagnosticHandler: ((String) -> Void)?

    init(
        engine: AudioEngineControlling = SystemAudioEngine(),
        converter: AudioPCMConverting = AudioPCMConverter(),
        audioSession: AudioSessionControlling = SystemAudioSession()
    ) {
        self.engine = engine
        self.converter = converter
        self.audioSession = audioSession
    }

    func start(onPCM: @escaping (Data) -> Void) throws {
        try audioSession.configureForRecording()
        let hardwareFormat = engine.inputFormat
        print("[Talka] MicrophonePCMSource.start: inputFormat=\(hardwareFormat)")
        engine.installInputTap(bufferSize: TalkaAudioFormat.captureTapBufferSize, format: hardwareFormat) { [converter] buffer, _ in
            print("[Talka] tap callback fired, buffer frameLength=\(buffer.frameLength)")
            do {
                let pcm = try converter.convert(buffer)
                print("[Talka] converted pcm size=\(pcm.count)")
                if !pcm.isEmpty {
                    onPCM(pcm)
                } else {
                    print("[Talka] pcm is empty, skipping onPCM")
                }
            } catch {
                print("[Talka] conversion error: \(error.localizedDescription)")
                self.diagnosticHandler?("Microphone conversion failed: \(error.localizedDescription)")
            }
        }
        try engine.start()
        print("[Talka] engine started successfully")
    }

    func sessionDiagnosticInfo() -> String {
        audioSession.diagnosticInfo()
    }

    func stop() {
        engine.removeInputTap()
        engine.stop()
    }
}

final class UnavailableMicrophoneSource: AudioCaptureSourcing {
    static let unavailableMessage = "Microphone capture is available only from the production iOS environment."

    func start(onPCM: @escaping (Data) -> Void) throws {
        _ = onPCM
        throw RemoteMicFlowError.recordingFailed(Self.unavailableMessage)
    }

    func stop() {}
}

struct UnavailableAudioStreamClient: AudioStreamClient {
    static let unavailableMessage = "Audio streaming transport is not available until the end-to-end session is implemented."

    func sendAudioStart(metadata: AudioStreamMetadata) async throws {
        _ = metadata
        throw RemoteMicFlowError.recordingFailed(Self.unavailableMessage)
    }

    func sendAudioFrame(sequence: Int, payload: Data) async throws {
        _ = sequence
        _ = payload
        throw RemoteMicFlowError.recordingFailed(Self.unavailableMessage)
    }

    func sendAudioStop(lastSequence: Int) async throws {
        _ = lastSequence
        throw RemoteMicFlowError.recordingFailed(Self.unavailableMessage)
    }

    func sendAudioCancel(reason: String) async throws {
        _ = reason
    }

    func sendKeyPress(key: String, modifiers: [ReturnKeyModifier]) async throws {
        _ = key
        _ = modifiers
        throw RemoteMicFlowError.recordingFailed(Self.unavailableMessage)
    }
}

protocol BonjourNetServiceBrowsing: AnyObject {
    var delegate: NetServiceBrowserDelegate? { get set }
    func searchForServices(ofType type: String, inDomain domainString: String)
    func stop()
}

struct RemoteMicShellEnvironment {
    let discoveryBrowser: RemoteMacDiscovering
    let sessionClient: RemotePairingSessioning
    let identityStore: PairedIdentityStoring
    let audioStreamClient: AudioStreamClient
    let microphoneSource: AudioCaptureSourcing
    let disconnectSecureSession: () -> Void
    let macConnectionMonitor: MacConnectionMonitoring

    static func production(
        bonjourBrowser: BonjourNetServiceBrowsing = SystemBonjourServiceBrowser(),
        identityStore: PairedIdentityStoring = KeychainPairedIdentityStore(),
        sessionClient: RemotePairingSessioning? = nil,
        audioStreamClient: AudioStreamClient? = nil,
        microphoneSource: AudioCaptureSourcing = MicrophonePCMSource()
    ) -> RemoteMicShellEnvironment {
        let secureSessionStore = SecureAudioSessionStore()
        return RemoteMicShellEnvironment(
            discoveryBrowser: BonjourRemoteMacDiscoveryBrowser(browser: bonjourBrowser),
            sessionClient: sessionClient ?? SecureRemotePairingSessionClient(sessionStore: secureSessionStore),
            identityStore: identityStore,
            audioStreamClient: audioStreamClient ?? SecureAudioStreamClient(sessionStore: secureSessionStore),
            microphoneSource: microphoneSource,
            disconnectSecureSession: {
                secureSessionStore.clear()
            },
            macConnectionMonitor: SecureSessionMacConnectionMonitor(sessionStore: secureSessionStore)
        )
    }

    static func uiTesting() -> RemoteMicShellEnvironment {
        let identity = PairedMacIdentity(
            deviceID: "ios-ui-test",
            deviceName: "Talka UI Test iPhone",
            serverDeviceID: "mac-ui-test",
            serverDeviceName: "Darluc's MacBook Pro"
        )
        let secureSessionStore = SecureAudioSessionStore()
        return RemoteMicShellEnvironment(
            discoveryBrowser: UITestingDiscoveryBrowser(),
            sessionClient: UITestingPairingSessionClient(identity: identity),
            identityStore: UITestingPairedIdentityStore(identity: identity),
            audioStreamClient: UITestingAudioStreamClient(),
            microphoneSource: UITestingMicrophoneSource(),
            disconnectSecureSession: {
                secureSessionStore.clear()
            },
            macConnectionMonitor: NoopMacConnectionMonitor()
        )
    }

    @MainActor
    func makeViewModel() -> RemoteMicShellViewModel {
        RemoteMicShellViewModel(
            discoveryBrowser: discoveryBrowser,
            sessionClient: sessionClient,
            identityStore: identityStore,
            audioStreamClient: audioStreamClient,
            microphoneSource: microphoneSource,
            disconnectSecureSession: disconnectSecureSession,
            macConnectionMonitor: macConnectionMonitor
        )
    }
}

@MainActor
final class RemoteMicShellViewModel: ObservableObject {
    @Published private(set) var connectionState: RemoteMicConnectionState = .idle
    @Published private(set) var discoveredMacs: [DiscoveredMac] = []
    @Published private(set) var currentMacName: String?
    @Published private(set) var lastErrorMessage: String?
    @Published private(set) var lastAudioDiagnostic: String?
    @Published private(set) var isBusy = false
    @Published private(set) var recordingState: RemoteMicRecordingState = .idle
    @Published private(set) var audioLevel: Double = 0
    @Published private(set) var returnKeyModifiers: Set<ReturnKeyModifier> = []
    @Published private(set) var returnKeySendState: ReturnKeySendState = .idle
    @Published var selectedMacID: String?
    @Published var pin = ""

    private let discoveryBrowser: RemoteMacDiscovering
    private let sessionClient: RemotePairingSessioning
    private let identityStore: PairedIdentityStoring
    private let audioStreamClient: AudioStreamClient
    private let microphoneSource: AudioCaptureSourcing
    private let disconnectSecureSession: () -> Void
    private let macConnectionMonitor: MacConnectionMonitoring
    private let firstFrameTimeoutNanoseconds: UInt64
    private let stopTailCaptureNanoseconds: UInt64
    private let audioOperationTimeoutNanoseconds: UInt64
    private var knownIdentity: PairedMacIdentity?
    private var lastAudioSequence = 0
    private var frameAccumulator = PCMFrameAccumulator(frameByteCount: TalkaAudioFormat.frameByteCount)
    private var frameQueue = BoundedAudioFrameQueue(maxFrames: 8)
    private let pendingPCMChunks = PendingPCMChunkBuffer()
    private var pcmDrainTask: Task<Void, Never>?
    private var firstFrameTimeoutTask: Task<Void, Never>?
    private var macConnectionMonitorTask: Task<Void, Never>?
    private var returnKeyFeedbackTask: Task<Void, Never>?

    init(
        discoveryBrowser: RemoteMacDiscovering,
        sessionClient: RemotePairingSessioning,
        identityStore: PairedIdentityStoring,
        audioStreamClient: AudioStreamClient = UnavailableAudioStreamClient(),
        microphoneSource: AudioCaptureSourcing = UnavailableMicrophoneSource(),
        disconnectSecureSession: @escaping () -> Void = {},
        macConnectionMonitor: MacConnectionMonitoring = NoopMacConnectionMonitor(),
        firstFrameTimeoutNanoseconds: UInt64 = 3_000_000_000,
        stopTailCaptureNanoseconds: UInt64 = 250_000_000,
        audioOperationTimeoutNanoseconds: UInt64 = 8_000_000_000
    ) {
        self.discoveryBrowser = discoveryBrowser
        self.sessionClient = sessionClient
        self.identityStore = identityStore
        self.audioStreamClient = audioStreamClient
        self.microphoneSource = microphoneSource
        self.disconnectSecureSession = disconnectSecureSession
        self.macConnectionMonitor = macConnectionMonitor
        self.firstFrameTimeoutNanoseconds = firstFrameTimeoutNanoseconds
        self.stopTailCaptureNanoseconds = stopTailCaptureNanoseconds
        self.audioOperationTimeoutNanoseconds = audioOperationTimeoutNanoseconds

        if let microphoneSource = microphoneSource as? MicrophonePCMSource {
            microphoneSource.diagnosticHandler = { [weak self] message in
                Task { @MainActor in
                    await self?.handleMicrophoneDiagnostic(message)
                }
            }
        }

        do {
            let identity = try identityStore.loadPairedIdentity()
            knownIdentity = identity
            currentMacName = identity?.macDisplayName
        } catch {
            lastErrorMessage = "The saved Mac could not be loaded. Pair again if needed."
        }
    }

    deinit {
        macConnectionMonitorTask?.cancel()
        returnKeyFeedbackTask?.cancel()
    }

    var knownMacName: String? {
        knownIdentity?.macDisplayName
    }

    var selectedMacName: String? {
        discoveredMacs.first { $0.id == selectedMacID }?.name
    }

    var canReconnect: Bool {
        knownIdentity != nil
    }

    var displayMacName: String? {
        currentMacName ?? knownMacName
    }

    var isConnectionActive: Bool {
        connectionState == .paired
    }

    var powerTint: Color {
        switch connectionState {
        case .paired:
            return .green
        case .pairing, .reconnecting, .discovering:
            return .orange
        case .failedPairing, .localNetworkDenied:
            return .red
        default:
            return .blue
        }
    }

    var returnKeyComboTitle: String {
        let selectedSymbols = ReturnKeyModifier.allCases
            .filter { returnKeyModifiers.contains($0) }
            .map(\.symbol)
        return (selectedSymbols + ["↵"]).joined(separator: " ")
    }

    func selectMac(id: String) {
        selectedMacID = id
    }

    func discover() async {
        isBusy = true
        connectionState = .discovering
        lastErrorMessage = nil
        defer { isBusy = false }

        do {
            let macs = try await discoveryBrowser.discoverMacs()
                .sorted { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }
            discoveredMacs = macs
            selectedMacID = macs.first?.id
            connectionState = .discovered
        } catch let error as RemoteMicFlowError {
            discoveredMacs = []
            selectedMacID = nil
            connectionState = error == .localNetworkDenied ? .localNetworkDenied : .failedPairing
            lastErrorMessage = error.errorDescription
        } catch {
            discoveredMacs = []
            selectedMacID = nil
            connectionState = .failedPairing
            lastErrorMessage = error.localizedDescription
        }
    }

    func pairSelectedMac() async {
        guard let selectedMac = discoveredMacs.first(where: { $0.id == selectedMacID }) else {
            connectionState = .failedPairing
            lastErrorMessage = RemoteMicFlowError.noSelectedMac.errorDescription
            return
        }

        isBusy = true
        connectionState = .pairing
        lastErrorMessage = nil
        defer { isBusy = false }

        do {
            let identity = try await sessionClient.pair(with: selectedMac, pin: pin.trimmingCharacters(in: .whitespacesAndNewlines))
            try identityStore.savePairedIdentity(identity)
            knownIdentity = identity
            currentMacName = identity.macDisplayName
            pin = ""
            connectionState = .paired
            startMacConnectionMonitor()
        } catch let error as RemoteMicFlowError {
            connectionState = .failedPairing
            lastErrorMessage = error.errorDescription
        } catch {
            connectionState = .failedPairing
            lastErrorMessage = error.localizedDescription
        }
    }

    func reconnectToKnownMac() async {
        let storedIdentity = knownIdentity ?? (try? identityStore.loadPairedIdentity())

        guard let identity = storedIdentity else {
            connectionState = .forgotten
            lastErrorMessage = RemoteMicFlowError.noKnownMac.errorDescription
            return
        }

        isBusy = true
        connectionState = .reconnecting
        lastErrorMessage = nil
        defer { isBusy = false }

        do {
            let refreshedIdentity = try await sessionClient.reconnect(using: identity)
            try identityStore.savePairedIdentity(refreshedIdentity)
            knownIdentity = refreshedIdentity
            currentMacName = refreshedIdentity.macDisplayName
            connectionState = .paired
            startMacConnectionMonitor()
        } catch let error as RemoteMicFlowError {
            connectionState = .failedPairing
            lastErrorMessage = error.errorDescription
        } catch {
            connectionState = .failedPairing
            lastErrorMessage = error.localizedDescription
        }
    }

    @discardableResult
    func toggleConnectionPower() async -> RemoteMicPowerToggleResult {
        if isConnectionActive {
            await disconnectFromCurrentMac()
            return .none
        }

        let storedIdentity = knownIdentity ?? (try? identityStore.loadPairedIdentity())
        guard storedIdentity != nil else {
            connectionState = .idle
            lastErrorMessage = RemoteMicFlowError.noKnownMac.errorDescription
            return .showPairingPanel
        }

        await reconnectToKnownMac()
        return isConnectionActive ? .none : .showPairingPanel
    }

    func disconnectFromCurrentMac() async {
        stopMacConnectionMonitor()
        if recordingState == .recording || recordingState == .stopping {
            await cancelRecording(reason: "connection_power_off")
        }
        disconnectSecureSession()
        currentMacName = nil
        connectionState = .idle
        lastErrorMessage = nil
        lastAudioDiagnostic = nil
        audioLevel = 0
    }

    func toggleReturnKeyModifier(_ modifier: ReturnKeyModifier) {
        if returnKeyModifiers.contains(modifier) {
            returnKeyModifiers.remove(modifier)
        } else {
            returnKeyModifiers.insert(modifier)
        }
    }

    func sendReturnKey() async {
        guard isConnectionActive else {
            setReturnKeySendState(.failed)
            lastErrorMessage = "Connect to a Mac before sending Return."
            return
        }
        guard returnKeySendState != .sending else {
            return
        }

        setReturnKeySendState(.sending)
        do {
            try await audioStreamClient.sendKeyPress(
                key: "enter",
                modifiers: ReturnKeyModifier.allCases.filter { returnKeyModifiers.contains($0) }
            )
            setReturnKeySendState(.sent)
            lastErrorMessage = nil
        } catch let error as RemoteMicFlowError {
            setReturnKeySendState(.failed)
            lastErrorMessage = error.errorDescription
        } catch {
            setReturnKeySendState(.failed)
            lastErrorMessage = error.localizedDescription
        }
    }

    private func setReturnKeySendState(_ state: ReturnKeySendState) {
        returnKeyFeedbackTask?.cancel()
        returnKeySendState = state
        guard state == .sent || state == .failed else {
            return
        }

        returnKeyFeedbackTask = Task { [weak self] in
            try? await Task.sleep(nanoseconds: 900_000_000)
            await MainActor.run {
                guard let self, self.returnKeySendState == state else {
                    return
                }
                self.returnKeySendState = .idle
                self.returnKeyFeedbackTask = nil
            }
        }
    }

    func forgetKnownMac() async {
        do {
            stopMacConnectionMonitor()
            disconnectSecureSession()
            try identityStore.clearPairedIdentity()
            knownIdentity = nil
            currentMacName = nil
            discoveredMacs = []
            selectedMacID = nil
            pin = ""
            lastErrorMessage = nil
            connectionState = .forgotten
        } catch {
            connectionState = .failedPairing
            lastErrorMessage = "The saved Mac could not be removed from secure storage."
        }
    }

    private func startMacConnectionMonitor() {
        stopMacConnectionMonitor()
        let monitor = macConnectionMonitor
        macConnectionMonitorTask = Task { [weak self] in
            await monitor.waitUntilDisconnected()
            guard !Task.isCancelled else { return }
            await self?.handleMonitoredMacDisconnect()
        }
    }

    private func stopMacConnectionMonitor() {
        macConnectionMonitorTask?.cancel()
        macConnectionMonitorTask = nil
    }

    private func handleMonitoredMacDisconnect() async {
        guard connectionState == .paired else {
            return
        }
        if recordingState == .recording || recordingState == .stopping {
            await cancelRecording(reason: "mac_disconnected")
        }
        disconnectSecureSession()
        currentMacName = nil
        connectionState = .idle
        lastErrorMessage = "The Mac disconnected. Start Talka on the Mac and reconnect."
        lastAudioDiagnostic = nil
        audioLevel = 0
        macConnectionMonitorTask = nil
    }

    func streamPCMChunksForTesting(_ chunks: [Data], maxQueuedFrames: Int = 8) async {
        recordingState = .recording
        lastErrorMessage = nil
        lastAudioDiagnostic = nil
        lastAudioSequence = 0
        audioLevel = 0

        frameAccumulator = PCMFrameAccumulator(frameByteCount: TalkaAudioFormat.frameByteCount)
        frameQueue = BoundedAudioFrameQueue(maxFrames: maxQueuedFrames)

        do {
            try await withAudioOperationTimeout {
                try await self.audioStreamClient.sendAudioStart(metadata: .talkaDefault)
            }

            for chunk in chunks {
                try await sendPCMChunk(chunk)
            }

            try await flushPendingPCMFrame()
            recordingState = .stopping
            try await withAudioOperationTimeout {
                try await self.audioStreamClient.sendAudioStop(lastSequence: self.lastAudioSequence)
            }
            recordingState = .idle
            audioLevel = 0
            lastAudioDiagnostic = nil
        } catch let error as RemoteMicFlowError {
            await stopAfterRecordingError(error)
        } catch {
            applyDetailedRecordingDiagnostic(from: error)
            await stopAfterRecordingError(recordingFailure(from: error))
        }
    }

    func startRecording() async {
        recordingState = .recording
        lastErrorMessage = nil
        lastAudioDiagnostic = nil
        lastAudioSequence = 0
        audioLevel = 0
        frameAccumulator = PCMFrameAccumulator(frameByteCount: TalkaAudioFormat.frameByteCount)
        frameQueue = BoundedAudioFrameQueue(maxFrames: 8)
        pendingPCMChunks.removeAll()
        pcmDrainTask?.cancel()
        pcmDrainTask = nil

        do {
            try await withAudioOperationTimeout {
                try await self.audioStreamClient.sendAudioStart(metadata: .talkaDefault)
            }
            let pendingPCMChunks = pendingPCMChunks
            try microphoneSource.start { [weak self, pendingPCMChunks] pcm in
                pendingPCMChunks.append(pcm)
                Task { @MainActor in
                    guard let self else { return }
                    self.schedulePendingPCMDrain()
                }
            }
            var sessionInfo = ""
            if let micSource = microphoneSource as? MicrophonePCMSource {
                sessionInfo = micSource.sessionDiagnosticInfo()
            }
            lastAudioDiagnostic = sessionInfo.isEmpty
                ? "recording started; waiting for microphone frames"
                : "recording started; waiting for microphone frames (session: \(sessionInfo))"
            scheduleFirstFrameTimeout()
        } catch let error as RemoteMicFlowError {
            lastAudioDiagnostic = recordingStartDiagnostic(for: error)
            await stopAfterRecordingError(error)
        } catch {
            let message = error.localizedDescription
            applyDetailedRecordingDiagnostic(
                from: error,
                fallback: message.contains("Secure audio session is not established")
                    ? "no active secure audio session before websocket start"
                    : "WebSocket bootstrap/sendAudioStart failed: \(message)"
            )
            await stopAfterRecordingError(recordingFailure(from: error))
        }
    }

    func stopRecording() async {
        cancelFirstFrameTimeout()
        do {
            if recordingState == .recording, stopTailCaptureNanoseconds > 0 {
                try await Task.sleep(nanoseconds: stopTailCaptureNanoseconds)
            }
            microphoneSource.stop()
            let shouldStop = recordingState == .recording
            if shouldStop {
                recordingState = .stopping
            }
            if let pcmDrainTask {
                try await withAudioOperationTimeout {
                    await pcmDrainTask.value
                }
            }
            try await drainPendingPCMChunks()
            await Task.yield()
            if let pcmDrainTask {
                try await withAudioOperationTimeout {
                    await pcmDrainTask.value
                }
            }
            try await drainPendingPCMChunks()
            guard shouldStop else {
                return
            }
            try await flushPendingPCMFrame()
            try await withAudioOperationTimeout {
                try await self.audioStreamClient.sendAudioStop(lastSequence: self.lastAudioSequence)
            }
            recordingState = .idle
            audioLevel = 0
            lastErrorMessage = nil
            lastAudioDiagnostic = nil
        } catch let error as RemoteMicFlowError {
            await stopAfterRecordingError(error)
        } catch {
            applyDetailedRecordingDiagnostic(from: error)
            await stopAfterRecordingError(recordingFailure(from: error))
        }
    }

    func cancelRecording(reason: String) async {
        microphoneSource.stop()
        cancelFirstFrameTimeout()
        do {
            try await withAudioOperationTimeout {
                try await self.audioStreamClient.sendAudioCancel(reason: reason)
            }
            recordingState = .idle
            audioLevel = 0
            lastAudioDiagnostic = nil
        } catch let error as RemoteMicFlowError {
            await stopAfterRecordingError(error)
        } catch {
            applyDetailedRecordingDiagnostic(from: error)
            await stopAfterRecordingError(recordingFailure(from: error))
        }
    }

    private func schedulePendingPCMDrain() {
        guard recordingState == .recording, pcmDrainTask == nil else {
            return
        }
        pcmDrainTask = Task { @MainActor [weak self] in
            guard let self else { return }
            defer {
                self.pcmDrainTask = nil
            }
            do {
                try await self.drainPendingPCMChunks()
            } catch {
                guard self.recordingState == .recording else { return }
                self.applyDetailedRecordingDiagnostic(from: error, fallback: "sendPCMChunk/sendAudioFrame failed: \(error.localizedDescription)")
                await self.stopAfterRecordingError(self.recordingFailure(from: error))
            }
        }
    }

    private func drainPendingPCMChunks() async throws {
        while recordingState == .recording || recordingState == .stopping {
            let chunks = pendingPCMChunks.drain()
            guard !chunks.isEmpty else {
                return
            }
            for chunk in chunks {
                try await sendPCMChunk(chunk)
            }
        }
    }

    private func sendPCMChunk(_ chunk: Data) async throws {
        guard recordingState == .recording || recordingState == .stopping else {
            return
        }
        print("[Talka] sendPCMChunk called with chunk size=\(chunk.count)")
        if !chunk.isEmpty {
            cancelFirstFrameTimeout()
            if lastAudioSequence == 0 {
                lastAudioDiagnostic = "audio frames flowing; seq=1"
            }
        }

        let frames = frameAccumulator.append(chunk)
        print("[Talka] frameAccumulator produced \(frames.count) frames")
        try frameQueue.enqueue(frames)

        for frame in frameQueue.drain() {
            try await sendPCMFrame(frame)
        }
    }

    private func flushPendingPCMFrame() async throws {
        guard recordingState == .recording || recordingState == .stopping else {
            return
        }
        guard let tailFrame = frameAccumulator.flushPaddedFrame() else {
            return
        }
        try await sendPCMFrame(tailFrame)
    }

    private func sendPCMFrame(_ frame: Data) async throws {
        guard recordingState == .recording else {
            return
        }
        let sequence = lastAudioSequence + 1
        audioLevel = PCMVolumeLevel.level(for: frame)
        print("[Talka] sending audio frame seq=\(sequence), size=\(frame.count)")
        try await withAudioOperationTimeout {
            try await self.audioStreamClient.sendAudioFrame(sequence: sequence, payload: frame)
        }
        guard recordingState == .recording || recordingState == .stopping else {
            return
        }
        lastAudioSequence = sequence
        print("[Talka] audio frame sent successfully")
    }

    private func withAudioOperationTimeout<T>(_ operation: @escaping () async throws -> T) async throws -> T {
        try await withThrowingTaskGroup(of: T.self) { group in
            group.addTask {
                try await operation()
            }
            group.addTask { [audioOperationTimeoutNanoseconds] in
                try await Task.sleep(nanoseconds: audioOperationTimeoutNanoseconds)
                throw RemoteMicFlowError.recordingFailed("Audio streaming timed out. Reconnect the Mac and try again.")
            }

            guard let result = try await group.next() else {
                throw RemoteMicFlowError.recordingFailed("Audio streaming timed out. Reconnect the Mac and try again.")
            }
            group.cancelAll()
            return result
        }
    }

    private func stopAfterRecordingError(_ error: RemoteMicFlowError) async {
        microphoneSource.stop()
        cancelFirstFrameTimeout()
        if case .audioBackpressure = error {
            try? await audioStreamClient.sendAudioCancel(reason: "backpressure")
        }
        recordingState = .failed
        audioLevel = 0
        lastErrorMessage = error.errorDescription
    }

    private func scheduleFirstFrameTimeout() {
        cancelFirstFrameTimeout()
        print("[Talka] scheduleFirstFrameTimeout started, waiting \(firstFrameTimeoutNanoseconds) ns")
        firstFrameTimeoutTask = Task { [weak self] in
            guard let self else { return }
            print("[Talka] timeout task running")
            try? await Task.sleep(nanoseconds: firstFrameTimeoutNanoseconds)
            print("[Talka] timeout task woke up, cancelled=\(Task.isCancelled)")
            guard !Task.isCancelled else { return }

            await MainActor.run {
                print("[Talka] timeout on MainActor, recordingState=\(self.recordingState), lastAudioSequence=\(self.lastAudioSequence)")
                guard self.recordingState == .recording, self.lastAudioSequence == 0 else { return }
                self.lastAudioDiagnostic = "no microphone frames received after audio session start"
                print("[Talka] updated lastAudioDiagnostic to no-frame timeout")
            }
        }
    }

    private func cancelFirstFrameTimeout() {
        firstFrameTimeoutTask?.cancel()
        firstFrameTimeoutTask = nil
    }

    private func handleMicrophoneDiagnostic(_ message: String) async {
        guard recordingState == .recording else {
            return
        }
        lastAudioDiagnostic = message
        await stopAfterRecordingError(.recordingFailed(message))
    }

    private func recordingStartDiagnostic(for error: RemoteMicFlowError) -> String {
        let message = error.errorDescription ?? error.localizedDescription
        if message.contains("Secure audio session is not established") {
            return "no active secure audio session before websocket start"
        }
        return "WebSocket bootstrap/sendAudioStart failed: \(message)"
    }

    private func recordingFailure(from error: Error) -> RemoteMicFlowError {
        if let flow = error as? RemoteMicFlowError {
            return flow
        }
        if let detailed = error as? RemoteMicDetailedError {
            return .recordingFailed(detailed.userMessage)
        }
        return .recordingFailed(error.localizedDescription)
    }

    private func applyDetailedRecordingDiagnostic(from error: Error, fallback: String? = nil) {
        if let detailed = error as? RemoteMicDetailedError {
            if let diagnostic = detailed.diagnostic, !diagnostic.isEmpty {
                lastAudioDiagnostic = "server \(detailed.code): \(diagnostic)"
            } else {
                lastAudioDiagnostic = "server \(detailed.code): \(detailed.userMessage)"
            }
            return
        }
        if let fallback {
            lastAudioDiagnostic = fallback
        }
    }
}

struct KeychainPairedIdentityStore: PairedIdentityStoring {
    private let service = "dev.talka.ios.paired-identity"
    private let account = "default"

    func loadPairedIdentity() throws -> PairedMacIdentity? {
        var query = baseQuery()
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne

        var item: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &item)

        if status == errSecItemNotFound {
            return nil
        }

        guard status == errSecSuccess else {
            throw KeychainStoreError.unexpectedStatus(status)
        }

        guard let data = item as? Data else {
            throw KeychainStoreError.invalidData
        }

        return try JSONDecoder().decode(PairedMacIdentity.self, from: data)
    }

    func savePairedIdentity(_ identity: PairedMacIdentity) throws {
        let data = try JSONEncoder().encode(identity)
        let deleteStatus = SecItemDelete(baseQuery() as CFDictionary)
        guard deleteStatus == errSecSuccess || deleteStatus == errSecItemNotFound else {
            throw KeychainStoreError.unexpectedStatus(deleteStatus)
        }

        var query = baseQuery()
        query[kSecValueData as String] = data
        query[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly

        let status = SecItemAdd(query as CFDictionary, nil)
        guard status == errSecSuccess else {
            throw KeychainStoreError.unexpectedStatus(status)
        }
    }

    func clearPairedIdentity() throws {
        let status = SecItemDelete(baseQuery() as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else {
            throw KeychainStoreError.unexpectedStatus(status)
        }
    }

    private func baseQuery() -> [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account
        ]
    }
}

struct SecurePairingChallengeResponse: Decodable {
    let pairingID: String
    let serverDeviceID: String
    let serverDeviceName: String
    let serverIdentityPublicKey: String
    let serverEphemeralPublicKey: String

    enum CodingKeys: String, CodingKey {
        case pairingID = "pairing_id"
        case serverDeviceID = "server_device_id"
        case serverDeviceName = "server_device_name"
        case serverIdentityPublicKey = "server_identity_public_key"
        case serverEphemeralPublicKey = "server_ephemeral_public_key"
    }
}

struct SecurePairingCompleteRequest: Encodable {
    let pairingID: String
    let deviceID: String
    let deviceName: String
    let clientIdentityPublicKey: String
    let clientEphemeralPublicKey: String
    let clientConfirmation: String

    enum CodingKeys: String, CodingKey {
        case pairingID = "pairing_id"
        case deviceID = "device_id"
        case deviceName = "device_name"
        case clientIdentityPublicKey = "client_identity_public_key"
        case clientEphemeralPublicKey = "client_ephemeral_public_key"
        case clientConfirmation = "client_confirmation"
    }
}

struct SecureResumeRequest: Encodable {
    let pairingID: String
    let deviceID: String
    let deviceName: String
    let clientIdentityPublicKey: String
    let clientEphemeralPublicKey: String

    enum CodingKeys: String, CodingKey {
        case pairingID = "pairing_id"
        case deviceID = "device_id"
        case deviceName = "device_name"
        case clientIdentityPublicKey = "client_identity_public_key"
        case clientEphemeralPublicKey = "client_ephemeral_public_key"
    }
}

struct SecurePairingResponse: Decodable {
    let deviceID: String
    let deviceName: String
    let serverDeviceID: String
    let serverDeviceName: String
    let serverIdentityPublicKey: String
    let serverEphemeralPublicKey: String
    let serverConfirmation: String
    let sessionID: String
    let audioWebSocketURL: String

    enum CodingKeys: String, CodingKey {
        case deviceID = "device_id"
        case deviceName = "device_name"
        case serverDeviceID = "server_device_id"
        case serverDeviceName = "server_device_name"
        case serverIdentityPublicKey = "server_identity_public_key"
        case serverEphemeralPublicKey = "server_ephemeral_public_key"
        case serverConfirmation = "server_confirmation"
        case sessionID = "session_id"
        case audioWebSocketURL = "audio_websocket_url"
    }
}

struct SecureEncryptedAudioMessage: Encodable {
    let version: UInt8
    let sessionID: String
    let seq: UInt64
    let type: String
    let nonce: String
    let ciphertext: String
    let tag: String

    enum CodingKeys: String, CodingKey {
        case version
        case sessionID = "session_id"
        case seq
        case type
        case nonce
        case ciphertext
        case tag
    }
}

struct SecureAudioWebSocketServerError: Decodable {
    let code: String
    let message: String
    let diagnostic: String?
}

struct SecureAudioWebSocketServerResponse: Decodable {
    let ok: Bool
    let error: SecureAudioWebSocketServerError?
    let finalText: String?

    enum CodingKeys: String, CodingKey {
        case ok
        case error
        case finalText = "final_text"
    }
}

struct SecureAudioSessionKeys {
    let sessionID: Data
    let clientToServerKey: SymmetricKey
    let audioWebSocketURL: URL
}

protocol SecureAudioWebSocketTasking: AnyObject {
    func resume()
    func send(_ text: String) async throws
    func receive() async throws -> String
    func cancel(with closeCode: URLSessionWebSocketTask.CloseCode, reason: Data?)
}

protocol SecureAudioWebSocketConnecting {
    func makeWebSocketTask(url: URL) -> SecureAudioWebSocketTasking
}

final class URLSessionSecureAudioWebSocketTask: SecureAudioWebSocketTasking {
    private let task: URLSessionWebSocketTask

    init(task: URLSessionWebSocketTask) {
        self.task = task
    }

    func resume() {
        task.resume()
    }

    func send(_ text: String) async throws {
        try await task.send(.string(text))
    }

    func receive() async throws -> String {
        let message = try await task.receive()
        switch message {
        case let .string(text):
            return text
        case let .data(data):
            guard let text = String(data: data, encoding: .utf8) else {
                throw RemoteMicFlowError.recordingFailed("The Mac returned an invalid audio response.")
            }
            return text
        @unknown default:
            throw RemoteMicFlowError.recordingFailed("The Mac returned an unsupported audio response.")
        }
    }

    func cancel(with closeCode: URLSessionWebSocketTask.CloseCode, reason: Data?) {
        task.cancel(with: closeCode, reason: reason)
    }
}

struct URLSessionSecureAudioWebSocketConnector: SecureAudioWebSocketConnecting {
    let urlSession: URLSession

    func makeWebSocketTask(url: URL) -> SecureAudioWebSocketTasking {
        URLSessionSecureAudioWebSocketTask(task: urlSession.webSocketTask(with: url))
    }
}

final class SecureAudioSessionStore {
    private let lock = NSLock()
    private var session: SecureAudioSessionKeys?

    func save(_ session: SecureAudioSessionKeys) {
        lock.lock()
        self.session = session
        lock.unlock()
    }

    func load() -> SecureAudioSessionKeys? {
        lock.lock()
        let session = session
        lock.unlock()
        return session
    }

    func clear() {
        lock.lock()
        session = nil
        lock.unlock()
    }
}

struct SecureSessionMacConnectionMonitor: MacConnectionMonitoring {
    let sessionStore: SecureAudioSessionStore
    var urlSession: URLSession = .shared
    var pollNanoseconds: UInt64 = 2_000_000_000
    var requestTimeout: TimeInterval = 1.5

    func waitUntilDisconnected() async {
        while !Task.isCancelled {
            guard let session = sessionStore.load(),
                  let statusURL = Self.statusURL(from: session.audioWebSocketURL)
            else {
                return
            }
            if !(await isReachable(statusURL)) {
                return
            }
            do {
                try await Task.sleep(nanoseconds: pollNanoseconds)
            } catch {
                return
            }
        }
    }

    private func isReachable(_ statusURL: URL) async -> Bool {
        var request = URLRequest(url: statusURL)
        request.timeoutInterval = requestTimeout
        do {
            let (data, response) = try await urlSession.data(for: request)
            guard let httpResponse = response as? HTTPURLResponse, 200..<300 ~= httpResponse.statusCode else {
                return false
            }
            guard let status = try? JSONDecoder().decode(MacServiceStatus.self, from: data) else {
                return false
            }
            return !status.serviceName.isEmpty
        } catch {
            return false
        }
    }

    private static func statusURL(from audioWebSocketURL: URL) -> URL? {
        var components = URLComponents(url: audioWebSocketURL, resolvingAgainstBaseURL: false)
        switch components?.scheme {
        case "ws":
            components?.scheme = "http"
        case "wss":
            components?.scheme = "https"
        default:
            return nil
        }
        components?.path = "/v1/status"
        components?.query = nil
        components?.fragment = nil
        return components?.url
    }

    private struct MacServiceStatus: Decodable {
        let serviceName: String

        enum CodingKeys: String, CodingKey {
            case serviceName = "service_name"
        }
    }
}

struct SecureRemotePairingSessionClient: RemotePairingSessioning {
    let sessionStore: SecureAudioSessionStore
    var urlSession: URLSession = .shared

    func pair(with mac: DiscoveredMac, pin: String) async throws -> PairedMacIdentity {
        let baseURL = try httpBaseURL(for: mac)
        let challenge: SecurePairingChallengeResponse = try await getJSON(baseURL.appendingPathComponent("v1/ios/pairing/challenge"))
        let clientIdentity = Curve25519.KeyAgreement.PrivateKey()
        let clientEphemeral = Curve25519.KeyAgreement.PrivateKey()
        let confirmation = try TalkaSecureTransport.pairingConfirmation(
            pin: pin,
            pairingID: challenge.pairingID,
            clientDeviceID: UIDevice.current.identifierForVendor?.uuidString ?? "ios-device",
            clientDeviceName: UIDevice.current.name,
            server: challenge,
            clientIdentity: clientIdentity,
            clientEphemeral: clientEphemeral
        )
        let request = SecurePairingCompleteRequest(
            pairingID: challenge.pairingID,
            deviceID: UIDevice.current.identifierForVendor?.uuidString ?? "ios-device",
            deviceName: UIDevice.current.name,
            clientIdentityPublicKey: clientIdentity.publicKey.rawRepresentation.base64EncodedString(),
            clientEphemeralPublicKey: clientEphemeral.publicKey.rawRepresentation.base64EncodedString(),
            clientConfirmation: confirmation.base64EncodedString()
        )
        let response: SecurePairingResponse = try await postJSON(baseURL.appendingPathComponent("v1/ios/pair"), body: request)
        let keys = try TalkaSecureTransport.sessionKeys(flow: "pairing", pairingID: challenge.pairingID, response: response, clientIdentity: clientIdentity, clientEphemeral: clientEphemeral)
        guard let audioURL = URL(string: response.audioWebSocketURL) else {
            throw RemoteMicFlowError.pairingFailed("The Mac returned an invalid audio URL.")
        }
        sessionStore.save(SecureAudioSessionKeys(sessionID: keys.sessionID, clientToServerKey: keys.clientToServerKey, audioWebSocketURL: audioURL))
        return PairedMacIdentity(deviceID: response.deviceID, deviceName: response.deviceName, hostName: mac.hostName, port: mac.port, serverDeviceID: response.serverDeviceID, serverDeviceName: response.serverDeviceName, clientIdentityPrivateKey: clientIdentity.rawRepresentation.base64EncodedString(), clientIdentityPublicKey: clientIdentity.publicKey.rawRepresentation.base64EncodedString(), serverIdentityPublicKey: response.serverIdentityPublicKey)
    }

    func reconnect(using identity: PairedMacIdentity) async throws -> PairedMacIdentity {
        let baseURL = try httpBaseURL(for: DiscoveredMac(id: identity.deviceID, name: identity.serverDeviceName ?? "", hostName: identity.hostName, port: identity.port))
        guard let privateKey = try? TalkaSecureTransport.privateKey(from: identity.clientIdentityPrivateKey) else {
            throw RemoteMicFlowError.reconnectFailed("The saved iPhone identity is incomplete. Pair again.")
        }
        let ephemeral = Curve25519.KeyAgreement.PrivateKey()
        let pairingID = UUID().uuidString
        let request = SecureResumeRequest(pairingID: pairingID, deviceID: identity.deviceID, deviceName: identity.deviceName, clientIdentityPublicKey: privateKey.publicKey.rawRepresentation.base64EncodedString(), clientEphemeralPublicKey: ephemeral.publicKey.rawRepresentation.base64EncodedString())
        let response: SecurePairingResponse = try await postJSON(baseURL.appendingPathComponent("v1/ios/resume"), body: request)
        let keys = try TalkaSecureTransport.sessionKeys(flow: "resume", pairingID: pairingID, response: response, clientIdentity: privateKey, clientEphemeral: ephemeral)
        guard let audioURL = URL(string: response.audioWebSocketURL) else {
            throw RemoteMicFlowError.reconnectFailed("The Mac returned an invalid audio URL.")
        }
        sessionStore.save(SecureAudioSessionKeys(sessionID: keys.sessionID, clientToServerKey: keys.clientToServerKey, audioWebSocketURL: audioURL))
        return PairedMacIdentity(deviceID: response.deviceID, deviceName: response.deviceName, hostName: identity.hostName, port: identity.port, serverDeviceID: response.serverDeviceID, serverDeviceName: response.serverDeviceName, clientIdentityPrivateKey: privateKey.rawRepresentation.base64EncodedString(), clientIdentityPublicKey: privateKey.publicKey.rawRepresentation.base64EncodedString(), serverIdentityPublicKey: response.serverIdentityPublicKey)
    }

    private func httpBaseURL(for mac: DiscoveredMac) throws -> URL {
        guard let hostName = mac.hostName, let port = mac.port, let url = URL(string: "http://\(hostName):\(port)/") else {
            throw RemoteMicFlowError.pairingFailed("The selected Mac does not have a resolved network address.")
        }
        return url
    }

    private func getJSON<T: Decodable>(_ url: URL) async throws -> T {
        let (data, response) = try await urlSession.data(from: url)
        try validateHTTPResponse(response)
        return try JSONDecoder().decode(T.self, from: data)
    }

    private func postJSON<T: Decodable, B: Encodable>(_ url: URL, body: B) async throws -> T {
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(body)
        let (data, response) = try await urlSession.data(for: request)
        try validateHTTPResponse(response)
        return try JSONDecoder().decode(T.self, from: data)
    }

    private func validateHTTPResponse(_ response: URLResponse) throws {
        guard let httpResponse = response as? HTTPURLResponse, 200..<300 ~= httpResponse.statusCode else {
            throw RemoteMicFlowError.pairingFailed("The Mac rejected the secure pairing request.")
        }
    }
}

final class SecureAudioStreamClient: AudioStreamClient {
    let sessionStore: SecureAudioSessionStore
    var urlSession: URLSession = .shared
    private var streamID = UUID().uuidString
    private var nextSequence: UInt64 = 1
    private var webSocket: SecureAudioWebSocketTasking?
    private var currentStreamSession: SecureAudioSessionKeys?
    private var encryptedSessionID: Data?
    private let encoder = JSONEncoder()
    private let decoder = JSONDecoder()
    private let webSocketConnector: SecureAudioWebSocketConnecting

    init(
        sessionStore: SecureAudioSessionStore,
        urlSession: URLSession = .shared,
        webSocketConnector: SecureAudioWebSocketConnecting? = nil
    ) {
        self.sessionStore = sessionStore
        self.urlSession = urlSession
        self.webSocketConnector = webSocketConnector ?? URLSessionSecureAudioWebSocketConnector(urlSession: urlSession)
    }

    func sendAudioStart(metadata: AudioStreamMetadata) async throws {
        streamID = UUID().uuidString
        let session = try prepareStreamSession()
        let task = webSocketConnector.makeWebSocketTask(url: session.audioWebSocketURL)
        webSocket = task
        currentStreamSession = session
        task.resume()
        try await send(type: "audio_start", payload: ["version": "v1alpha1", "type": "audio_start", "session_id": session.sessionID.base64EncodedString(), "stream_id": streamID, "metadata": ["sample_rate": metadata.sampleRate, "channels": metadata.channels, "encoding": metadata.encoding, "frame_duration_ms": metadata.frameDurationMS, "language": metadata.language]] as [String: Any], session: session)
    }

    func sendAudioFrame(sequence: Int, payload: Data) async throws {
        let session = try streamSession()
        try await send(type: "audio_frame", payload: ["version": "v1alpha1", "type": "audio_frame", "session_id": session.sessionID.base64EncodedString(), "stream_id": streamID, "sequence": sequence, "payload_b64": payload.base64EncodedString()] as [String: Any], session: session)
    }

    func sendAudioStop(lastSequence: Int) async throws {
        let session = try streamSession()
        let task = try activeWebSocket()
        defer { closeCurrentStream() }
        try await send(type: "audio_stop", payload: ["version": "v1alpha1", "type": "audio_stop", "session_id": session.sessionID.base64EncodedString(), "stream_id": streamID, "last_sequence": lastSequence] as [String: Any], session: session)
        let responseText: String
        do {
            responseText = try await task.receive()
        } catch {
            throw RemoteMicFlowError.recordingFailed("The Mac closed the audio session before returning a result.")
        }
        let response: SecureAudioWebSocketServerResponse
        do {
            response = try decoder.decode(SecureAudioWebSocketServerResponse.self, from: Data(responseText.utf8))
        } catch {
            throw RemoteMicFlowError.recordingFailed("The Mac returned an invalid audio response.")
        }
        guard response.ok else {
            let error = response.error
            if error?.code == "replayed_sequence" || error?.code == "out_of_order_sequence" {
                clearSecureSession()
            }
            throw RemoteMicDetailedError(
                code: error?.code ?? "processing_failed",
                userMessage: error?.message ?? "The Mac could not finish processing this recording.",
                diagnostic: error?.diagnostic
            )
        }
    }

    func sendAudioCancel(reason: String) async throws {
        _ = reason
        webSocket?.cancel(with: .normalClosure, reason: nil)
        webSocket = nil
        currentStreamSession = nil
    }

    func sendKeyPress(key: String, modifiers: [ReturnKeyModifier]) async throws {
        let session = try prepareStreamSession()
        let task = webSocketConnector.makeWebSocketTask(url: session.audioWebSocketURL)
        webSocket = task
        task.resume()
        defer { closeCurrentStream() }
        try await send(type: "key_press", payload: ["version": "v1alpha1", "type": "key_press", "session_id": session.sessionID.base64EncodedString(), "key": key, "modifiers": modifiers.map(\.wireValue)] as [String: Any], session: session)
        let responseText: String
        do {
            responseText = try await task.receive()
        } catch {
            throw RemoteMicFlowError.recordingFailed("The Mac closed the key press session before returning a result.")
        }
        let response: SecureAudioWebSocketServerResponse
        do {
            response = try decoder.decode(SecureAudioWebSocketServerResponse.self, from: Data(responseText.utf8))
        } catch {
            throw RemoteMicFlowError.recordingFailed("The Mac returned an invalid key press response.")
        }
        guard response.ok else {
            throw RemoteMicDetailedError(
                code: response.error?.code ?? "key_press_failed",
                userMessage: response.error?.message ?? "The Mac could not send the key press.",
                diagnostic: response.error?.diagnostic
            )
        }
    }

    private func send(type: String, payload: [String: Any], session: SecureAudioSessionKeys) async throws {
        let plaintext = try JSONSerialization.data(withJSONObject: payload)
        print("[Talka] encrypting type=\(type) with seq=\(nextSequence)")
        let sequence = nextSequence
        let encrypted = try TalkaSecureTransport.encrypt(type: type, plaintext: plaintext, session: session, seq: sequence)
        let data = try encoder.encode(encrypted)
        try await (webSocket ?? webSocketConnector.makeWebSocketTask(url: session.audioWebSocketURL)).send(String(decoding: data, as: UTF8.self))
        nextSequence = sequence + 1
    }

    private func activeWebSocket() throws -> SecureAudioWebSocketTasking {
        guard let webSocket else {
            throw RemoteMicFlowError.recordingFailed("Secure audio session is not established. Pair or reconnect first.")
        }
        return webSocket
    }

    private func activeSession() throws -> SecureAudioSessionKeys {
        guard let session = sessionStore.load() else {
            throw RemoteMicFlowError.recordingFailed("Secure audio session is not established. Pair or reconnect first.")
        }
        return session
    }

    private func prepareStreamSession() throws -> SecureAudioSessionKeys {
        let session = try activeSession()
        if encryptedSessionID != session.sessionID {
            encryptedSessionID = session.sessionID
            nextSequence = 1
        }
        return session
    }

    private func streamSession() throws -> SecureAudioSessionKeys {
        guard let session = currentStreamSession else {
            throw RemoteMicFlowError.recordingFailed("Secure audio session is not established. Pair or reconnect first.")
        }
        return session
    }

    private func closeCurrentStream() {
        webSocket?.cancel(with: .normalClosure, reason: nil)
        webSocket = nil
        currentStreamSession = nil
    }

    private func clearSecureSession() {
        sessionStore.clear()
        encryptedSessionID = nil
        nextSequence = 1
    }
}

enum TalkaSecureTransport {
    static func privateKey(from value: String?) throws -> Curve25519.KeyAgreement.PrivateKey {
        guard let value, let data = Data(base64Encoded: value) else {
            throw RemoteMicFlowError.reconnectFailed("The saved iPhone identity is incomplete. Pair again.")
        }
        return try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: data)
    }

    static func pairingConfirmation(pin: String, pairingID: String, clientDeviceID: String, clientDeviceName: String, server: SecurePairingChallengeResponse, clientIdentity: Curve25519.KeyAgreement.PrivateKey, clientEphemeral: Curve25519.KeyAgreement.PrivateKey) throws -> Data {
        guard let serverEphemeralPublicKey = Data(base64Encoded: server.serverEphemeralPublicKey), let serverIdentityPublicKey = Data(base64Encoded: server.serverIdentityPublicKey) else {
            throw RemoteMicFlowError.reconnectFailed("Invalid server public key")
        }
        let transcript = try marshalTranscript(flow: "pairing", pairingID: pairingID, clientDeviceID: clientDeviceID, clientDeviceName: clientDeviceName, serverDeviceID: server.serverDeviceID, serverDeviceName: server.serverDeviceName, clientEphemeralPublicKey: clientEphemeral.publicKey.rawRepresentation, serverEphemeralPublicKey: serverEphemeralPublicKey, clientIdentityPublicKey: clientIdentity.publicKey.rawRepresentation, serverIdentityPublicKey: serverIdentityPublicKey)
        let secrets = try joinedSecrets(clientEphemeral.sharedSecretFromKeyAgreement(with: Curve25519.KeyAgreement.PublicKey(rawRepresentation: serverEphemeralPublicKey)).withUnsafeBytes { Data($0) }, clientIdentity.sharedSecretFromKeyAgreement(with: Curve25519.KeyAgreement.PublicKey(rawRepresentation: serverIdentityPublicKey)).withUnsafeBytes { Data($0) })
        let key = HKDF<SHA256>.deriveKey(inputKeyMaterial: SymmetricKey(data: secrets), salt: Data(pin.utf8), info: Data("talka/pairing-confirmation-key/v1".utf8), outputByteCount: 32)
        return Data(HMAC<SHA256>.authenticationCode(for: transcript, using: key))
    }

    static func sessionKeys(flow: String, pairingID: String, response: SecurePairingResponse, clientIdentity: Curve25519.KeyAgreement.PrivateKey, clientEphemeral: Curve25519.KeyAgreement.PrivateKey) throws -> SecureAudioSessionKeys {
        guard let serverIdentityPublicKey = Data(base64Encoded: response.serverIdentityPublicKey), let serverEphemeralPublicKey = Data(base64Encoded: response.serverEphemeralPublicKey), let sessionID = Data(base64Encoded: response.sessionID) else {
            throw RemoteMicFlowError.reconnectFailed("Invalid server public key")
        }
        let serverIdentity = try Curve25519.KeyAgreement.PublicKey(rawRepresentation: serverIdentityPublicKey)
        let serverEphemeral = try Curve25519.KeyAgreement.PublicKey(rawRepresentation: serverEphemeralPublicKey)
        let ee = try clientEphemeral.sharedSecretFromKeyAgreement(with: serverEphemeral).withUnsafeBytes { Data($0) }
        let ss = try clientIdentity.sharedSecretFromKeyAgreement(with: serverIdentity).withUnsafeBytes { Data($0) }
        let transcript = try marshalTranscript(flow: flow, pairingID: pairingID, clientDeviceID: response.deviceID, clientDeviceName: response.deviceName, serverDeviceID: response.serverDeviceID, serverDeviceName: response.serverDeviceName, clientEphemeralPublicKey: clientEphemeral.publicKey.rawRepresentation, serverEphemeralPublicKey: serverEphemeral.rawRepresentation, clientIdentityPublicKey: clientIdentity.publicKey.rawRepresentation, serverIdentityPublicKey: serverIdentity.rawRepresentation)
        let transcriptHash = Data(SHA256.hash(data: transcript))
        let material = HKDF<SHA256>.deriveKey(inputKeyMaterial: SymmetricKey(data: try joinedSecrets(ee, ss)), salt: sessionID + transcriptHash, info: Data("talka/session-keys/v1".utf8), outputByteCount: 64)
        let bytes = material.withUnsafeBytes { Data($0) }
        guard let audioURL = URL(string: response.audioWebSocketURL) else {
            throw RemoteMicFlowError.pairingFailed("The Mac returned an invalid audio URL.")
        }
        return SecureAudioSessionKeys(sessionID: sessionID, clientToServerKey: SymmetricKey(data: bytes.prefix(32)), audioWebSocketURL: audioURL)
    }

    static func encrypt(type: String, plaintext: Data, session: SecureAudioSessionKeys, seq: UInt64) throws -> SecureEncryptedAudioMessage {
        let nonceData = Data((0..<12).map { _ in UInt8.random(in: 0...255) })
        let aad = associatedData(version: 1, seq: seq, sessionID: session.sessionID, type: type)
        let sealed = try ChaChaPoly.seal(plaintext, using: session.clientToServerKey, nonce: ChaChaPoly.Nonce(data: nonceData), authenticating: aad)
        return SecureEncryptedAudioMessage(version: 1, sessionID: session.sessionID.base64EncodedString(), seq: seq, type: type, nonce: nonceData.base64EncodedString(), ciphertext: sealed.ciphertext.base64EncodedString(), tag: sealed.tag.base64EncodedString())
    }

    private static func joinedSecrets(_ values: Data...) throws -> Data {
        values.reduce(into: Data()) { out, value in
            appendBytes(value, to: &out)
        }
    }

    private static func marshalTranscript(flow: String, pairingID: String, clientDeviceID: String, clientDeviceName: String, serverDeviceID: String, serverDeviceName: String, clientEphemeralPublicKey: Data, serverEphemeralPublicKey: Data, clientIdentityPublicKey: Data, serverIdentityPublicKey: Data) throws -> Data {
        var out = Data()
        appendString("v1alpha1", to: &out)
        appendString(flow, to: &out)
        appendString(pairingID, to: &out)
        appendString(clientDeviceID, to: &out)
        appendString(clientDeviceName, to: &out)
        appendString(serverDeviceID, to: &out)
        appendString(serverDeviceName, to: &out)
        appendBytes(clientEphemeralPublicKey, to: &out)
        appendBytes(serverEphemeralPublicKey, to: &out)
        appendBytes(clientIdentityPublicKey, to: &out)
        appendBytes(serverIdentityPublicKey, to: &out)
        return out
    }

    private static func associatedData(version: UInt8, seq: UInt64, sessionID: Data, type: String) -> Data {
        var out = Data([version])
        var seqBE = seq.bigEndian
        out.append(Data(bytes: &seqBE, count: MemoryLayout<UInt64>.size))
        appendBytes(sessionID, to: &out)
        appendString(type, to: &out)
        return out
    }

    private static func appendString(_ value: String, to data: inout Data) {
        appendBytes(Data(value.utf8), to: &data)
    }

    private static func appendBytes(_ value: Data, to data: inout Data) {
        var length = UInt32(value.count).bigEndian
        data.append(Data(bytes: &length, count: MemoryLayout<UInt32>.size))
        data.append(value)
    }
}

enum KeychainStoreError: LocalizedError {
    case invalidData
    case unexpectedStatus(OSStatus)

    var errorDescription: String? {
        switch self {
        case .invalidData:
            return "The saved Mac identity was malformed."
        case let .unexpectedStatus(status):
            return "Keychain request failed with status \(status)."
        }
    }
}

final class SystemBonjourServiceBrowser: BonjourNetServiceBrowsing {
    private let browser = NetServiceBrowser()

    var delegate: NetServiceBrowserDelegate? {
        get { browser.delegate }
        set { browser.delegate = newValue }
    }

    func searchForServices(ofType type: String, inDomain domainString: String) {
        browser.searchForServices(ofType: type, inDomain: domainString)
    }

    func stop() {
        browser.stop()
    }
}

final class BonjourRemoteMacDiscoveryBrowser: NSObject, RemoteMacDiscovering {
    private static let searchType = "_talka._tcp."
    private static let searchDomain = "local."

    private let browser: BonjourNetServiceBrowsing
    private let discoveryTimeout: TimeInterval
    private let resolutionTimeout: TimeInterval
    private var continuation: CheckedContinuation<[DiscoveredMac], Error>?
    private var timeoutWorkItem: DispatchWorkItem?
    private var discoveredMacsByID: [String: DiscoveredMac] = [:]
    private var resolvingServicesByID: [String: NetService] = [:]

    init(
        browser: BonjourNetServiceBrowsing = SystemBonjourServiceBrowser(),
        discoveryTimeout: TimeInterval = 1.5,
        resolutionTimeout: TimeInterval = 1.0
    ) {
        self.browser = browser
        self.discoveryTimeout = discoveryTimeout
        self.resolutionTimeout = resolutionTimeout
        super.init()
    }

    func discoverMacs() async throws -> [DiscoveredMac] {
        browser.stop()
        timeoutWorkItem?.cancel()
        discoveredMacsByID = [:]
        resolvingServicesByID = [:]

        return try await withCheckedThrowingContinuation { continuation in
            self.continuation = continuation
            browser.delegate = self
            browser.searchForServices(ofType: Self.searchType, inDomain: Self.searchDomain)

            let workItem = DispatchWorkItem { [weak self] in
                self?.finishDiscovery(with: .success(self?.sortedDiscoveredMacs ?? []))
            }
            timeoutWorkItem = workItem
            DispatchQueue.main.asyncAfter(deadline: .now() + discoveryTimeout, execute: workItem)
        }
    }

    private var sortedDiscoveredMacs: [DiscoveredMac] {
        discoveredMacsByID.values.sorted { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }
    }

    private func finishDiscovery(with result: Result<[DiscoveredMac], Error>) {
        guard let continuation else {
            return
        }

        browser.stop()
        browser.delegate = nil
        timeoutWorkItem?.cancel()
        timeoutWorkItem = nil
        resolvingServicesByID = [:]
        self.continuation = nil

        switch result {
        case let .success(macs):
            continuation.resume(returning: macs)
        case let .failure(error):
            continuation.resume(throwing: error)
        }
    }

    private func serviceID(for service: NetService) -> String {
        [service.name, service.type, service.domain].joined(separator: "|")
    }
}

extension BonjourRemoteMacDiscoveryBrowser: NetServiceBrowserDelegate {
    func netServiceBrowser(_ browser: NetServiceBrowser, didFind service: NetService, moreComing: Bool) {
        _ = browser
        _ = moreComing
        resolvingServicesByID[serviceID(for: service)] = service
        service.delegate = self
        service.resolve(withTimeout: resolutionTimeout)
    }

    func netServiceBrowser(_ browser: NetServiceBrowser, didNotSearch errorDict: [String: NSNumber]) {
        _ = browser
        _ = errorDict
        finishDiscovery(with: .failure(RemoteMicFlowError.localNetworkDenied))
    }
}

extension BonjourRemoteMacDiscoveryBrowser: NetServiceDelegate {
    func netServiceDidResolveAddress(_ sender: NetService) {
        let discoveredMac = DiscoveredMac(
            id: serviceID(for: sender),
            name: sender.name,
            hostName: sender.hostName,
            port: sender.port > 0 ? sender.port : nil,
            serviceType: sender.type
        )
        discoveredMacsByID[discoveredMac.id] = discoveredMac
    }
}

struct UnavailableRemotePairingSessionClient: RemotePairingSessioning {
    static let pairingUnavailableMessage = "Pairing with the Mac service is not available in this build yet."
    static let reconnectUnavailableMessage = "Reconnect is not available until the iPhone pairing transport is implemented."

    func pair(with mac: DiscoveredMac, pin: String) async throws -> PairedMacIdentity {
        _ = mac
        _ = pin
        throw RemoteMicFlowError.pairingFailed(Self.pairingUnavailableMessage)
    }

    func reconnect(using identity: PairedMacIdentity) async throws -> PairedMacIdentity {
        _ = identity
        throw RemoteMicFlowError.reconnectFailed(Self.reconnectUnavailableMessage)
    }
}

private struct UITestingDiscoveryBrowser: RemoteMacDiscovering {
    func discoverMacs() async throws -> [DiscoveredMac] {
        [
            DiscoveredMac(id: "mac-ui-test", name: "Darluc's MacBook Pro")
        ]
    }
}

private final class UITestingPairingSessionClient: RemotePairingSessioning {
    private let identity: PairedMacIdentity

    init(identity: PairedMacIdentity) {
        self.identity = identity
    }

    func pair(with mac: DiscoveredMac, pin: String) async throws -> PairedMacIdentity {
        _ = mac
        _ = pin
        return identity
    }

    func reconnect(using identity: PairedMacIdentity) async throws -> PairedMacIdentity {
        _ = identity
        return self.identity
    }
}

private final class UITestingPairedIdentityStore: PairedIdentityStoring {
    private var identity: PairedMacIdentity?

    init(identity: PairedMacIdentity) {
        self.identity = identity
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

private final class UITestingAudioStreamClient: AudioStreamClient {
    func sendAudioStart(metadata: AudioStreamMetadata) async throws {
        _ = metadata
    }

    func sendAudioFrame(sequence: Int, payload: Data) async throws {
        _ = sequence
        _ = payload
    }

    func sendAudioStop(lastSequence: Int) async throws {
        _ = lastSequence
        try? await Task.sleep(nanoseconds: 1_000_000_000)
    }

    func sendAudioCancel(reason: String) async throws {
        _ = reason
    }

    func sendKeyPress(key: String, modifiers: [ReturnKeyModifier]) async throws {
        _ = key
        _ = modifiers
    }
}

private final class UITestingMicrophoneSource: AudioCaptureSourcing {
    func start(onPCM: @escaping (Data) -> Void) throws {
        _ = onPCM
    }

    func stop() {}
}

@main
struct TalkaIOSApp: App {
    var body: some Scene {
        WindowGroup {
            if ProcessInfo.processInfo.arguments.contains("--ui-testing") {
                UITestingRootView()
            } else {
                ContentView()
            }
        }
    }
}

private struct UITestingRootView: View {
    @StateObject private var viewModel = RemoteMicShellEnvironment.uiTesting().makeViewModel()

    var body: some View {
        ContentView(viewModel: viewModel)
            .task {
                await viewModel.reconnectToKnownMac()
            }
    }
}

struct ContentView: View {
    @StateObject private var viewModel: RemoteMicShellViewModel
    @State private var isConnectionPanelPresented = false
    @State private var isPairingPanelPresented = false
    @State private var isPressingMicrophone = false

    @MainActor
    init(viewModel: RemoteMicShellViewModel) {
        _viewModel = StateObject(wrappedValue: viewModel)
    }

    @MainActor
    init() {
        _viewModel = StateObject(wrappedValue: RemoteMicShellEnvironment.production().makeViewModel())
    }

    var body: some View {
        ZStack {
            RemoteMicBackground()

            RemoteMicControlSurface(
                viewModel: viewModel,
                isPressingMicrophone: isPressingMicrophone,
                showConnectionPanel: {
                    isConnectionPanelPresented = true
                },
                togglePower: {
                    Task {
                        let result = await viewModel.toggleConnectionPower()
                        if result == .showPairingPanel {
                            isConnectionPanelPresented = false
                            isPairingPanelPresented = true
                        }
                    }
                },
                sendReturnKey: {
                    Task {
                        await viewModel.sendReturnKey()
                    }
                },
                startRecording: startPressRecording,
                stopRecording: stopPressRecording
            )

            if isConnectionPanelPresented {
                ConnectionPanelOverlay(
                    viewModel: viewModel,
                    dismiss: {
                        isConnectionPanelPresented = false
                    }
                )
                .transition(.opacity.combined(with: .move(edge: .bottom)))
            }

            if isPairingPanelPresented {
                PairingPanelOverlay(
                    viewModel: viewModel,
                    dismiss: {
                        isPairingPanelPresented = false
                    }
                )
                .transition(.opacity.combined(with: .move(edge: .bottom)))
            }
        }
        .animation(.spring(response: 0.28, dampingFraction: 0.88), value: isConnectionPanelPresented)
        .animation(.spring(response: 0.28, dampingFraction: 0.88), value: isPairingPanelPresented)
    }

    private func startPressRecording() {
        guard viewModel.isConnectionActive, !isPressingMicrophone, viewModel.recordingState != .recording, viewModel.recordingState != .stopping else {
            return
        }
        isPressingMicrophone = true
        Task {
            await viewModel.startRecording()
        }
    }

    private func stopPressRecording() {
        guard isPressingMicrophone else {
            return
        }
        isPressingMicrophone = false
        Task {
            await viewModel.stopRecording()
        }
    }
}

private struct RemoteMicBackground: View {
    var body: some View {
        LinearGradient(
            colors: [
                Color(uiColor: .systemBackground),
                Color(uiColor: .secondarySystemBackground)
            ],
            startPoint: .top,
            endPoint: .bottom
        )
        .overlay(alignment: .top) {
            Circle()
                .fill(Color.green.opacity(0.12))
                .frame(width: 260, height: 260)
                .blur(radius: 28)
                .offset(y: 90)
        }
        .ignoresSafeArea()
    }
}

struct RemoteMicControlSurface: View {
    @ObservedObject var viewModel: RemoteMicShellViewModel
    var isPressingMicrophone: Bool
    var showConnectionPanel: () -> Void
    var togglePower: () -> Void
    var sendReturnKey: () -> Void
    var startRecording: () -> Void
    var stopRecording: () -> Void

    var body: some View {
        ZStack {
            Rectangle()
                .fill(.thinMaterial)
                .overlay {
                    Rectangle()
                        .fill(
                            RadialGradient(
                                colors: [
                                    viewModel.powerTint.opacity(viewModel.isConnectionActive ? 0.20 : 0.10),
                                    Color.clear
                                ],
                                center: .center,
                                startRadius: 48,
                                endRadius: 210
                            )
                        )
                }
                .ignoresSafeArea()

            VStack {
                HStack(alignment: .top) {
                    Button(action: showConnectionPanel) {
                        Image(systemName: "slider.horizontal.3")
                            .font(.system(size: 18, weight: .semibold))
                            .frame(width: 44, height: 44)
                    }
                    .buttonStyle(GlassCircleButtonStyle(tint: Color(uiColor: .label)))
                    .accessibilityLabel("Connection")
                    .accessibilityIdentifier("connectionPanelButton")

                    Spacer()

                    Button(action: togglePower) {
                        Image(systemName: "power")
                            .font(.system(size: 20, weight: .bold))
                            .frame(width: 54, height: 54)
                    }
                    .buttonStyle(GlassCircleButtonStyle(tint: viewModel.powerTint, filled: true))
                    .disabled(viewModel.isBusy)
                    .accessibilityLabel("Power")
                    .accessibilityValue(viewModel.isConnectionActive ? "connected" : "disconnected")
                    .accessibilityIdentifier("connectionPowerButton")
                }

                Spacer(minLength: 16)

                MicrophonePressButton(
                    recordingState: viewModel.recordingState,
                    isPressing: isPressingMicrophone,
                    audioLevel: viewModel.audioLevel,
                    isEnabled: viewModel.isConnectionActive && !viewModel.isBusy,
                    startRecording: startRecording,
                    stopRecording: stopRecording
                )

                Button(action: sendReturnKey) {
                    ReturnKeyComboPill(
                        title: viewModel.returnKeyComboTitle,
                        state: viewModel.returnKeySendState
                    )
                    .frame(minWidth: 92, minHeight: 38)
                }
                .padding(.top, 57)
                .buttonStyle(ReturnKeyMicButtonStyle(
                    state: viewModel.returnKeySendState,
                    isEnabled: viewModel.isConnectionActive && !viewModel.isBusy
                ))
                .disabled(!viewModel.isConnectionActive || viewModel.isBusy || viewModel.returnKeySendState == .sending)
                .accessibilityLabel("Return")
                .accessibilityIdentifier("returnKeyButton")

                Spacer(minLength: 28)
            }
            .padding(18)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

private struct ReturnKeyComboPill: View {
    var title: String
    var state: ReturnKeySendState

    var body: some View {
        HStack(spacing: 8) {
            Text(title)
                .font(.system(size: 17, weight: .heavy))
                .monospacedDigit()

            switch state {
            case .sending:
                ProgressView()
                    .controlSize(.small)
                    .tint(.white)
            case .sent:
                Image(systemName: "checkmark")
                    .font(.system(size: 12, weight: .heavy))
            case .failed:
                Image(systemName: "exclamationmark")
                    .font(.system(size: 12, weight: .heavy))
            case .idle:
                EmptyView()
            }
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 10)
        .contentTransition(.symbolEffect(.replace))
        .animation(.snappy(duration: 0.18), value: state)
    }
}

private struct MicrophonePressButton: View {
    var recordingState: RemoteMicRecordingState
    var isPressing: Bool
    var audioLevel: Double
    var isEnabled: Bool
    var startRecording: () -> Void
    var stopRecording: () -> Void

    private var ringTint: Color {
        guard isEnabled else {
            return .secondary
        }

        switch recordingState {
        case .recording:
            return .green
        case .stopping:
            return .orange
        case .failed:
            return .red
        case .idle:
            return .blue
        }
    }

    var body: some View {
        ZStack {
            Circle()
                .fill(ringTint.opacity(recordingState == .idle ? 0.12 : 0.24))
                .frame(width: 232, height: 232)
                .scaleEffect(isPressing ? 1.05 : 1)

            Circle()
                .stroke(ringTint.opacity(0.28 + min(audioLevel, 1) * 0.24), lineWidth: recordingState == .recording ? 16 : 12)
                .frame(width: 220, height: 220)
                .scaleEffect(recordingState == .stopping ? 0.96 : 1)

            if recordingState == .recording {
                AudioBreathingRingLayer(audioLevel: audioLevel, tint: ringTint)
                    .frame(width: 220, height: 220)
                    .allowsHitTesting(false)
            }

            Circle()
                .fill(Color(uiColor: .label))
                .frame(width: 168, height: 168)
                .shadow(color: Color.black.opacity(0.24), radius: 18, y: 12)
                .overlay {
                    if recordingState == .stopping {
                        ProgressView()
                            .tint(ringTint)
                            .controlSize(.large)
                    }
                }
        }
        .contentShape(Circle())
        .gesture(
            DragGesture(minimumDistance: 0)
                .onChanged { _ in
                    guard isEnabled else { return }
                    startRecording()
                }
                .onEnded { _ in
                    guard isEnabled else { return }
                    stopRecording()
                }
        )
        .allowsHitTesting(isEnabled)
        .opacity(isEnabled ? 1 : 0.42)
        .disabled(!isEnabled)
        .accessibilityLabel("Microphone")
        .accessibilityValue(accessibilityValue)
        .accessibilityAddTraits(.isButton)
        .accessibilityIdentifier("microphoneButton")
    }

    private var accessibilityValue: String {
        switch recordingState {
        case .recording:
            return "recording"
        case .stopping:
            return "processing"
        case .failed:
            return "failed"
        case .idle:
            return "idle"
        }
    }
}

struct AudioBreathingRingMetrics {
    static let minimumIntensity: Double = 0
    static let maximumIntensity: Double = 1
    static let baseRadiusExpansion: Double = 0
    static let maximumAudioRadiusExpansion: Double = 50

    static func intensity(for audioLevel: Double) -> Double {
        min(max(audioLevel, minimumIntensity), maximumIntensity)
    }

    static func radiusExpansion(for audioLevel: Double) -> Double {
        baseRadiusExpansion + intensity(for: audioLevel) * maximumAudioRadiusExpansion
    }
}

private struct AudioBreathingRingLayer: View {
    var audioLevel: Double
    var tint: Color

    var body: some View {
        TimelineView(.animation(minimumInterval: 1.0 / 30.0)) { timeline in
            let elapsed = timeline.date.timeIntervalSinceReferenceDate
            let intensity = AudioBreathingRingMetrics.intensity(for: audioLevel)

            ZStack {
                ForEach(0..<3, id: \.self) { index in
                    let progress = breathingProgress(elapsed: elapsed, index: index)
                    let scale = 0.78 + progress * AudioBreathingRingMetrics.radiusExpansion(for: audioLevel)
                    let opacity = (1 - progress) * (0.42 + intensity * 0.28)

                    Circle()
                        .stroke(tint.opacity(opacity), lineWidth: 2.5 + intensity * 3)
                        .scaleEffect(scale)
                }
            }
        }
    }

    private func breathingProgress(elapsed: TimeInterval, index: Int) -> Double {
        let duration = 1.9
        let stagger = duration / 3
        let shifted = elapsed + Double(index) * stagger
        return shifted.truncatingRemainder(dividingBy: duration) / duration
    }
}

struct ConnectionPanelOverlay: View {
    @ObservedObject var viewModel: RemoteMicShellViewModel
    var dismiss: () -> Void
    @State private var isDebugExpanded = false
    @State private var isForgetConfirmationPresented = false

    var body: some View {
        ZStack(alignment: .bottom) {
            Color.black.opacity(0.18)
                .ignoresSafeArea()
                .onTapGesture(perform: dismiss)

            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    Text("设置")
                        .font(.title2.weight(.bold))

                    Spacer()

                    Button(action: dismiss) {
                        Image(systemName: "xmark")
                            .font(.system(size: 14, weight: .bold))
                            .frame(width: 36, height: 36)
                    }
                    .buttonStyle(GlassCircleButtonStyle(tint: Color(uiColor: .label)))
                    .accessibilityLabel("Close")
                    .accessibilityIdentifier("connectionPanelCloseButton")
                }

                if let macName = viewModel.displayMacName {
                    Button {
                        isForgetConfirmationPresented = true
                    } label: {
                        SettingsInfoRow(title: macName, subtitle: viewModel.isConnectionActive ? "已连接" : "已记住", showsStatusDot: true)
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("connectedMacInfo")
                    .confirmationDialog("是否遗忘设备", isPresented: $isForgetConfirmationPresented, titleVisibility: .visible) {
                        Button("遗忘设备", role: .destructive) {
                            Task {
                                await viewModel.forgetKnownMac()
                            }
                        }

                        Button("取消", role: .cancel) {}
                    }
                }

                HStack(spacing: 8) {
                    ForEach(ReturnKeyModifier.allCases, id: \.self) { modifier in
                        ReturnKeyModifierButton(
                            title: modifier.title,
                            isSelected: viewModel.returnKeyModifiers.contains(modifier),
                            action: {
                                viewModel.toggleReturnKeyModifier(modifier)
                            }
                        )
                    }
                }
                .accessibilityIdentifier("returnModifierButtons")

                Button {
                    withAnimation(.spring(response: 0.24, dampingFraction: 0.9)) {
                        isDebugExpanded.toggle()
                    }
                } label: {
                    SettingsInfoRow(title: "Debug", subtitle: "音频状态与错误信息", showsStatusDot: false)
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("debugMenuButton")

                if isDebugExpanded {
                    DebugPanel(viewModel: viewModel)
                        .transition(.opacity.combined(with: .move(edge: .top)))
                }
            }
            .padding(16)
            .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 28, style: .continuous))
            .padding(.horizontal, 16)
            .padding(.bottom, 12)
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("connectionPanel")
        }
    }
}

private struct ReturnKeyModifierButton: View {
    var title: String
    var isSelected: Bool
    var action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(title)
                .font(.caption.weight(.bold))
                .frame(maxWidth: .infinity)
                .frame(height: 34)
        }
        .buttonStyle(.plain)
        .foregroundStyle(isSelected ? .white : Color(uiColor: .label))
        .background(isSelected ? Color.blue : Color(uiColor: .secondarySystemBackground), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
        .accessibilityAddTraits(isSelected ? .isSelected : [])
    }
}

private struct PairingPanelOverlay: View {
    @ObservedObject var viewModel: RemoteMicShellViewModel
    var dismiss: () -> Void

    var body: some View {
        ZStack(alignment: .bottom) {
            Color.black.opacity(0.18)
                .ignoresSafeArea()
                .onTapGesture(perform: dismiss)

            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    Text("连接到 Mac")
                        .font(.title2.weight(.bold))

                    Spacer()

                    Button(action: dismiss) {
                        Image(systemName: "xmark")
                            .font(.system(size: 14, weight: .bold))
                            .frame(width: 36, height: 36)
                    }
                    .buttonStyle(GlassCircleButtonStyle(tint: Color(uiColor: .label)))
                    .accessibilityLabel("Close")
                    .accessibilityIdentifier("pairingPanelCloseButton")
                }

                if viewModel.isBusy {
                    ProgressView("Searching")
                        .frame(maxWidth: .infinity, alignment: .leading)
                }

                if !viewModel.discoveredMacs.isEmpty {
                    Picker("Mac", selection: $viewModel.selectedMacID) {
                        ForEach(viewModel.discoveredMacs) { mac in
                            Text(mac.name).tag(Optional(mac.id))
                        }
                    }
                    .pickerStyle(.menu)
                }

                TextField("PIN", text: $viewModel.pin)
                    .keyboardType(.numberPad)
                    .textContentType(.oneTimeCode)
                    .textFieldStyle(.roundedBorder)
                    .accessibilityIdentifier("pinTextField")

                Button {
                    Task {
                        await viewModel.pairSelectedMac()
                        if viewModel.isConnectionActive {
                            dismiss()
                        }
                    }
                } label: {
                    Text("Connect")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .disabled(viewModel.selectedMacID == nil || viewModel.pin.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || viewModel.isBusy)

                if let error = viewModel.lastErrorMessage {
                    Text(error)
                        .font(.caption)
                        .foregroundStyle(.red)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            .padding(16)
            .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 28, style: .continuous))
            .padding(.horizontal, 16)
            .padding(.bottom, 12)
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("pairingPanel")
            .task {
                if viewModel.discoveredMacs.isEmpty {
                    await viewModel.discover()
                }
            }
        }
    }
}

private struct SettingsInfoRow: View {
    var title: String
    var subtitle: String
    var showsStatusDot: Bool

    var body: some View {
        HStack(spacing: 12) {
            if showsStatusDot {
                Circle()
                    .fill(Color.green)
                    .frame(width: 11, height: 11)
                    .padding(.horizontal, 2)
            }

            VStack(alignment: .leading, spacing: 4) {
                Text(title)
                    .font(.headline)
                    .foregroundStyle(.primary)
                    .lineLimit(1)
                    .minimumScaleFactor(0.76)
                Text(subtitle)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Spacer()
        }
        .padding(15)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(uiColor: .secondarySystemBackground), in: RoundedRectangle(cornerRadius: 12, style: .continuous))
    }
}

struct DebugPanel: View {
    @ObservedObject var viewModel: RemoteMicShellViewModel

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            DebugRow(title: "Audio", value: viewModel.lastAudioDiagnostic ?? "No audio diagnostics")
            DebugRow(title: "Error", value: viewModel.lastErrorMessage ?? "No app errors")
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(uiColor: .tertiarySystemBackground), in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("debugPanel")
    }
}

private struct DebugRow: View {
    var title: String
    var value: String

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(title)
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)
            Text(value)
                .font(.caption2.monospaced())
                .foregroundStyle(.primary)
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
        }
    }
}

private struct GlassCircleButtonStyle: ButtonStyle {
    var tint: Color
    var filled = false

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .foregroundStyle(filled ? .white : tint)
            .background(
                Circle()
                    .fill(filled ? tint : Color(uiColor: .systemBackground).opacity(0.58))
            )
            .scaleEffect(configuration.isPressed ? 0.94 : 1)
            .shadow(color: tint.opacity(filled ? 0.30 : 0.08), radius: filled ? 12 : 8, y: filled ? 8 : 4)
    }
}

private struct ReturnKeyMicButtonStyle: ButtonStyle {
    var state: ReturnKeySendState
    var isEnabled: Bool

    private let returnKeyAcidCore = Color(red: 0.03, green: 0.07, blue: 0.05)
    private let returnKeyAcidRing = Color(red: 0.71, green: 1.00, blue: 0.18)

    private var returnKeyRingTint: Color {
        guard isEnabled else {
            return .secondary
        }
        switch state {
        case .sent:
            return .green
        case .failed:
            return .red
        case .sending:
            return returnKeyAcidRing
        case .idle:
            return returnKeyAcidRing
        }
    }

    private var coreColor: Color {
        switch state {
        case .sent:
            return .green
        case .failed:
            return .red
        case .sending, .idle:
            return returnKeyAcidCore
        }
    }

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .foregroundStyle(.white)
            .background(
                Capsule()
                    .fill(returnKeyRingTint.opacity(state == .idle ? 0.12 : 0.24))
                    .scaleEffect(configuration.isPressed ? 1.03 : 1)
            )
            .background(
                Capsule()
                    .fill(coreColor)
                    .overlay(
                        Capsule()
                            .stroke(returnKeyRingTint.opacity(0.36), lineWidth: state == .idle ? 4 : 5)
                    )
            )
            .opacity(isEnabled ? 1 : 0.42)
            .scaleEffect(configuration.isPressed ? 0.95 : 1)
            .shadow(color: Color.black.opacity(isEnabled ? 0.24 : 0.05), radius: isEnabled ? 18 : 8, y: isEnabled ? 12 : 4)
            .animation(.snappy(duration: 0.18), value: configuration.isPressed)
            .animation(.snappy(duration: 0.18), value: state)
    }
}
