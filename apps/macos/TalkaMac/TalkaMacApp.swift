import AppKit
import Foundation
import SwiftUI

enum ShellMetrics {
    static let panelSpacing: CGFloat = 18
    static let sectionSpacing: CGFloat = 14
    static let contentPadding: CGFloat = 20
    static let cornerRadius: CGFloat = 12
    static let minSettingsWidth: CGFloat = 720
    static let minSettingsHeight: CGFloat = 620
    static let minUtilityWidth: CGFloat = 420
    static let minUtilityHeight: CGFloat = 320
}

enum ShellWindowID {
    static let pairing = "pairing"
    static let diagnostics = "diagnostics"
}

enum ASRProviderOption: String, CaseIterable, Identifiable {
    case embedded = "funasr_embedded"
    case external = "funasr_external"
    case sidecar = "sidecar"
    case container = "funasr_container"

    var id: String { rawValue }

    var title: String {
        switch self {
        case .embedded:
            return "Embedded Runtime"
        case .external:
            return "External FunASR"
        case .sidecar:
            return "Legacy Sidecar"
        case .container:
            return "Managed Docker"
        }
    }
}

enum ServiceDisplayState: String, Equatable {
    case listening
    case paired
    case recording
    case transcribing
    case inserting
    case error
    case unavailable

    init(apiState: String, deviceCount: Int) {
        switch apiState.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() {
        case "running", "listening":
            self = deviceCount > 0 ? .paired : .listening
        case "paired":
            self = .paired
        case "recording":
            self = .recording
        case "transcribing":
            self = .transcribing
        case "inserting":
            self = .inserting
        case "error":
            self = .error
        default:
            self = deviceCount > 0 ? .paired : .listening
        }
    }

    var label: String {
        switch self {
        case .listening:
            return "Listening"
        case .paired:
            return "Paired"
        case .recording:
            return "Recording"
        case .transcribing:
            return "Transcribing"
        case .inserting:
            return "Inserting"
        case .error:
            return "Error"
        case .unavailable:
            return "Unavailable"
        }
    }

    var symbolName: String {
        switch self {
        case .listening:
            return "dot.radiowaves.left.and.right"
        case .paired:
            return "checkmark.shield"
        case .recording:
            return "waveform.badge.mic"
        case .transcribing:
            return "text.quote"
        case .inserting:
            return "text.cursor"
        case .error:
            return "exclamationmark.triangle.fill"
        case .unavailable:
            return "bolt.horizontal.circle"
        }
    }

    var tint: Color {
        switch self {
        case .listening:
            return .blue
        case .paired:
            return .green
        case .recording:
            return .red
        case .transcribing:
            return .orange
        case .inserting:
            return .mint
        case .error:
            return .red
        case .unavailable:
            return .secondary
        }
    }

    var helpText: String {
        switch self {
        case .listening:
            return "The local Talka service is reachable and waiting for paired devices."
        case .paired:
            return "A trusted device is available and the service is ready."
        case .recording:
            return "Audio capture is active on a paired device."
        case .transcribing:
            return "Talka is converting captured audio into text."
        case .inserting:
            return "The service is sending the final text to the active destination."
        case .error:
            return "The control API reported an error state. Check diagnostics for details."
        case .unavailable:
            return "The localhost control API is unavailable. Start or check the local Talka service, then retry."
        }
    }
}

struct ControlServiceError: LocalizedError, Equatable {
    var code: String
    var message: String
    var details: [String: String]

    var errorDescription: String? {
        message
    }
}

enum ControlAPIClientError: LocalizedError, Equatable {
    case serviceUnavailable
    case invalidResponse(statusCode: Int, error: ControlServiceError)
    case transport(String)
    case decoding(String)

    var errorDescription: String? {
        switch self {
        case .serviceUnavailable:
            return "The Talka control service is unavailable."
        case let .invalidResponse(statusCode, error):
            return "The Talka control service returned \(statusCode): \(error.message)"
        case let .transport(message):
            return message
        case let .decoding(message):
            return message
        }
    }
}

protocol RecoveryTextCopying {
    func copy(_ text: String)
}

struct SystemRecoveryTextCopier: RecoveryTextCopying {
    func copy(_ text: String) {
        let pasteboard = NSPasteboard.general
        pasteboard.clearContents()
        pasteboard.setString(text, forType: .string)
    }
}

protocol ControlAPIClient {
    var baseURL: URL { get }
    func fetchStatus() async throws -> ControlStatus
    func fetchDevices() async throws -> [ControlDevice]
    func startPairing() async throws -> ControlPairingSession
    func fetchConfig() async throws -> ControlConfig
    func saveConfig(_ config: ControlConfig) async throws -> ControlConfig
    func forgetDevice(id: String) async throws
    func openAccessibilitySettings() async throws -> AccessibilityGuidance
}

struct ControlStatus: Equatable {
    var serviceName: String
    var state: String
    var configPath: String
    var uptimeSeconds: Int
    var deviceCount: Int
    var pairingActive: Bool
}

struct ControlDevice: Identifiable, Equatable {
    var id: String
    var name: String
    var paired: Bool
    var lastSeenAt: Date?
}

struct ControlPairingSession: Equatable {
    var pairingID: String
    var pin: String
    var expiresAt: Date
    var expiresInSeconds: Int
}

struct AccessibilityGuidance: Equatable {
    var permission: String
    var opened: Bool
    var settingsURL: String
    var message: String
}

enum InjectionRecoveryAction: String, Equatable {
    case openAccessibilityGuidance = "open_accessibility_guidance"
    case copyFailedText = "copy_failed_text"

    var title: String {
        switch self {
        case .openAccessibilityGuidance:
            return "Open Accessibility Guidance"
        case .copyFailedText:
            return "Copy Failed Text"
        }
    }
}

struct InjectionRecovery: Equatable {
    var diagnosticCode: String
    var message: String
    var failedText: String
    var action: InjectionRecoveryAction

    init(diagnosticCode: String, message: String, failedText: String, action: InjectionRecoveryAction) {
        self.diagnosticCode = diagnosticCode
        self.message = message
        self.failedText = failedText
        self.action = action
    }

    init?(serviceError: ControlServiceError) {
        let defaultAction: InjectionRecoveryAction?
        switch serviceError.code {
        case "accessibility_missing":
            defaultAction = .openAccessibilityGuidance
        case "paste_failed", "clipboard_read_failed", "clipboard_write_failed":
            defaultAction = .copyFailedText
        default:
            defaultAction = nil
        }

        guard let defaultAction else {
            return nil
        }

        let action = serviceError.details["recovery_action"].flatMap(InjectionRecoveryAction.init(rawValue:)) ?? defaultAction
        let failedText = serviceError.details["failed_text"] ?? ""
        let message = serviceError.details["user_message"] ?? serviceError.message
        self.init(diagnosticCode: serviceError.code, message: message, failedText: failedText, action: action)
    }
}

struct ControlConfig: Equatable {
    var path: String
    var server: ControlServerConfig
    var asr: ControlASRConfig
    var llm: ControlLLMConfig
    var injection: ControlInjectionConfig
    var logging: ControlLoggingConfig

    static let placeholder = ControlConfig(
        path: "",
        server: ControlServerConfig(bindHost: "127.0.0.1", port: 8080, serviceName: "Talka"),
        asr: ControlASRConfig(
            provider: "funasr_embedded",
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

struct ControlServerConfig: Codable, Equatable {
    var bindHost: String
    var port: Int
    var serviceName: String

    enum CodingKeys: String, CodingKey {
        case bindHost = "bind_host"
        case port
        case serviceName = "service_name"
    }
}

struct ControlASRConfig: Codable, Equatable {
    var provider: String
    var runtimePath: String
    var host: String
    var port: Int
    var mode: String
    var sampleRate: Int
    var startupTimeoutSeconds: Int
    var containerImage: String
    var containerName: String
    var downloadDir: String
    var hotwordPath: String
    var models: ControlASRModelsConfig

    enum CodingKeys: String, CodingKey {
        case provider
        case runtimePath = "runtime_path"
        case host
        case port
        case mode
        case sampleRate = "sample_rate"
        case startupTimeoutSeconds = "startup_timeout_seconds"
        case containerImage = "container_image"
        case containerName = "container_name"
        case downloadDir = "download_dir"
        case hotwordPath = "hotword_path"
        case models
    }
}

struct ControlASRModelsConfig: Codable, Equatable {
    var asr: String
    var online: String
    var vad: String
    var punc: String
    var itn: String
    var lm: String
}

struct ControlLLMConfig: Codable, Equatable {
    var provider: String
    var baseURL: String
    var model: String
    var timeoutSeconds: Int

    enum CodingKeys: String, CodingKey {
        case provider
        case baseURL = "base_url"
        case model
        case timeoutSeconds = "timeout_seconds"
    }
}

struct ControlInjectionConfig: Codable, Equatable {
    var mode: String
    var restoreClipboard: Bool

    enum CodingKeys: String, CodingKey {
        case mode
        case restoreClipboard = "restore_clipboard"
    }
}

struct ControlLoggingConfig: Codable, Equatable {
    var level: String
    var captureAudio: Bool
    var captureTranscript: Bool

    enum CodingKeys: String, CodingKey {
        case level
        case captureAudio = "capture_audio"
        case captureTranscript = "capture_transcript"
    }
}

private struct ControlStatusResponse: Decodable {
    var serviceName: String
    var state: String
    var configPath: String
    var uptimeSeconds: Int
    var deviceCount: Int
    var pairingActive: Bool

    enum CodingKeys: String, CodingKey {
        case serviceName = "service_name"
        case state
        case configPath = "config_path"
        case uptimeSeconds = "uptime_seconds"
        case deviceCount = "device_count"
        case pairingActive = "pairing_active"
    }
}

private struct ControlDeviceResponse: Decodable {
    var id: String
    var name: String
    var paired: Bool
    var lastSeenAt: Date?

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        name = try container.decode(String.self, forKey: .name)
        paired = try container.decode(Bool.self, forKey: .paired)

        if let lastSeenAtString = try container.decodeIfPresent(String.self, forKey: .lastSeenAt) {
            guard let parsedLastSeenAt = Self.lastSeenAtFormatter.date(from: lastSeenAtString) else {
                throw DecodingError.dataCorruptedError(forKey: .lastSeenAt, in: container, debugDescription: "Expected date string to be ISO8601-formatted.")
            }
            lastSeenAt = parsedLastSeenAt
        } else {
            lastSeenAt = nil
        }
    }

    enum CodingKeys: String, CodingKey {
        case id
        case name
        case paired
        case lastSeenAt = "last_seen_at"
    }

    private static let lastSeenAtFormatter: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()
}

private struct ControlDevicesEnvelope: Decodable {
    var devices: [ControlDeviceResponse]
}

private struct ControlPairingResponse: Decodable {
    var pairingID: String
    var pin: String
    var expiresAt: Date
    var expiresInSeconds: Int

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        pairingID = try container.decode(String.self, forKey: .pairingID)
        pin = try container.decode(String.self, forKey: .pin)
        expiresInSeconds = try container.decode(Int.self, forKey: .expiresInSeconds)

        let expiresAtString = try container.decode(String.self, forKey: .expiresAt)
        guard let parsedExpiresAt = Self.expiresAtFormatter.date(from: expiresAtString) else {
            throw DecodingError.dataCorruptedError(forKey: .expiresAt, in: container, debugDescription: "Expected date string to be ISO8601-formatted.")
        }
        expiresAt = parsedExpiresAt
    }

    enum CodingKeys: String, CodingKey {
        case pairingID = "pairing_id"
        case pin
        case expiresAt = "expires_at"
        case expiresInSeconds = "expires_in_seconds"
    }

    private static let expiresAtFormatter: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()
}

private struct ControlConfigPayload: Codable, Equatable {
    var server: ControlServerConfig
    var asr: ControlASRConfig
    var llm: ControlLLMConfig
    var injection: ControlInjectionConfig
    var logging: ControlLoggingConfig
}

private struct ControlConfigEnvelope: Codable {
    var path: String
    var config: ControlConfigPayload
}

private struct AccessibilityGuidanceResponse: Decodable {
    var permission: String
    var opened: Bool
    var settingsURL: String
    var message: String

    enum CodingKeys: String, CodingKey {
        case permission
        case opened
        case settingsURL = "settings_url"
        case message
    }
}

private struct ControlErrorEnvelope: Decodable {
    var error: ControlErrorPayload
}

private struct ControlErrorPayload: Decodable {
    var code: String
    var message: String
    var details: [String: String]?
}

struct LiveControlAPIClient: ControlAPIClient {
    let baseURL: URL
    private let session: URLSession
    private let decoder: JSONDecoder
    private let encoder: JSONEncoder

    init(baseURL: URL = LiveControlAPIClient.defaultBaseURL, session: URLSession = .shared) {
        self.baseURL = baseURL
        self.session = session
        self.decoder = JSONDecoder()
        self.decoder.dateDecodingStrategy = .iso8601
        self.encoder = JSONEncoder()
        self.encoder.dateEncodingStrategy = .iso8601
    }

    func fetchStatus() async throws -> ControlStatus {
        let response: ControlStatusResponse = try await send(path: "/v1/status", method: "GET")
        return ControlStatus(
            serviceName: response.serviceName,
            state: response.state,
            configPath: response.configPath,
            uptimeSeconds: response.uptimeSeconds,
            deviceCount: response.deviceCount,
            pairingActive: response.pairingActive
        )
    }

    func fetchDevices() async throws -> [ControlDevice] {
        let response: ControlDevicesEnvelope = try await send(path: "/v1/devices", method: "GET")
        return response.devices.map {
            ControlDevice(id: $0.id, name: $0.name, paired: $0.paired, lastSeenAt: $0.lastSeenAt)
        }
    }

    func startPairing() async throws -> ControlPairingSession {
        let response: ControlPairingResponse = try await send(path: "/v1/pairing/start", method: "POST")
        return ControlPairingSession(
            pairingID: response.pairingID,
            pin: response.pin,
            expiresAt: response.expiresAt,
            expiresInSeconds: response.expiresInSeconds
        )
    }

    func fetchConfig() async throws -> ControlConfig {
        let response: ControlConfigEnvelope = try await send(path: "/v1/config", method: "GET")
        return ControlConfig(path: response.path, server: response.config.server, asr: response.config.asr, llm: response.config.llm, injection: response.config.injection, logging: response.config.logging)
    }

    func saveConfig(_ config: ControlConfig) async throws -> ControlConfig {
        let payload = ControlConfigPayload(server: config.server, asr: config.asr, llm: config.llm, injection: config.injection, logging: config.logging)
        let body = try encoder.encode(payload)
        let response: ControlConfigEnvelope = try await send(path: "/v1/config", method: "PUT", body: body)
        return ControlConfig(path: response.path, server: response.config.server, asr: response.config.asr, llm: response.config.llm, injection: response.config.injection, logging: response.config.logging)
    }

    func forgetDevice(id: String) async throws {
        let _: ForgetDeviceReceipt = try await send(path: "/v1/devices/\(id)/forget", method: "POST")
    }

    func openAccessibilitySettings() async throws -> AccessibilityGuidance {
        let response: AccessibilityGuidanceResponse = try await send(path: "/v1/permissions/accessibility/open", method: "POST")
        return AccessibilityGuidance(permission: response.permission, opened: response.opened, settingsURL: response.settingsURL, message: response.message)
    }

    private func send<Response: Decodable>(path: String, method: String, body: Data? = nil) async throws -> Response {
        var request = URLRequest(url: baseURL.appendingPathComponent(path.trimmingCharacters(in: CharacterSet(charactersIn: "/"))))
        request.httpMethod = method
        request.httpBody = body
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if body != nil {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: request)
        } catch {
            throw mapTransport(error)
        }

        guard let httpResponse = response as? HTTPURLResponse else {
            throw ControlAPIClientError.transport("The Talka control service returned a non-HTTP response.")
        }

        guard (200...299).contains(httpResponse.statusCode) else {
            if let payload = try? decoder.decode(ControlErrorEnvelope.self, from: data).error {
                let serviceError = ControlServiceError(code: payload.code, message: payload.message, details: payload.details ?? [:])
                throw httpResponse.statusCode == 503 ? ControlAPIClientError.serviceUnavailable : .invalidResponse(statusCode: httpResponse.statusCode, error: serviceError)
            }
            let message = HTTPURLResponse.localizedString(forStatusCode: httpResponse.statusCode)
            throw httpResponse.statusCode == 503 ? ControlAPIClientError.serviceUnavailable : .invalidResponse(statusCode: httpResponse.statusCode, error: ControlServiceError(code: "http_error", message: message, details: [:]))
        }

        do {
            return try decoder.decode(Response.self, from: data)
        } catch {
            throw ControlAPIClientError.decoding("The Talka control service returned an unexpected payload.")
        }
    }

    private func mapTransport(_ error: Error) -> ControlAPIClientError {
        if let urlError = error as? URLError {
            switch urlError.code {
            case .cannotConnectToHost, .notConnectedToInternet, .networkConnectionLost, .timedOut, .cannotFindHost:
                return .serviceUnavailable
            default:
                return .transport(urlError.localizedDescription)
            }
        }
        return .transport(error.localizedDescription)
    }

    private struct ForgetDeviceReceipt: Decodable {
        var deviceID: String
        var forgotten: Bool

        enum CodingKeys: String, CodingKey {
            case deviceID = "device_id"
            case forgotten
        }
    }

    static var defaultBaseURL: URL {
        if let raw = ProcessInfo.processInfo.environment["TALKA_CONTROL_API_BASE_URL"], let url = URL(string: raw) {
            return url
        }
        return URL(string: "http://127.0.0.1:8080")!
    }
}

@MainActor
final class PairingViewModel: ObservableObject {
    let pairingID: String
    let pin: String
    let expiresAt: Date
    private let nowProvider: () -> Date

    @Published private(set) var secondsRemaining: Int

    init(session: ControlPairingSession, nowProvider: @escaping () -> Date = Date.init) {
        self.pairingID = session.pairingID
        self.pin = session.pin
        self.expiresAt = session.expiresAt
        self.nowProvider = nowProvider
        self.secondsRemaining = PairingViewModel.remainingSeconds(until: session.expiresAt, nowProvider: nowProvider)
    }

    var expiryText: String {
        let minutes = secondsRemaining / 60
        let seconds = secondsRemaining % 60
        return String(format: "%02d:%02d", minutes, seconds)
    }

    var isExpired: Bool {
        secondsRemaining == 0
    }

    func refreshCountdown() {
        secondsRemaining = Self.remainingSeconds(until: expiresAt, nowProvider: nowProvider)
    }

    private static func remainingSeconds(until expiry: Date, nowProvider: () -> Date) -> Int {
        max(0, Int(expiry.timeIntervalSince(nowProvider()).rounded(.up)))
    }
}

@MainActor
final class AppShellViewModel: ObservableObject {
    @Published private(set) var serviceDisplayState: ServiceDisplayState = .unavailable
    @Published private(set) var status: ControlStatus?
    @Published private(set) var devices: [ControlDevice] = []
    @Published var config: ControlConfig = .placeholder
    @Published private(set) var pairing: PairingViewModel?
    @Published private(set) var accessibilityGuidance: AccessibilityGuidance?
    @Published private(set) var injectionRecovery: InjectionRecovery?
    @Published private(set) var isBusy = false
    @Published private(set) var lastUpdated: Date?
    @Published private(set) var lastErrorMessage: String?

    private let client: ControlAPIClient
    private let textCopier: RecoveryTextCopying
    private let nowProvider: () -> Date
    private var hasLoaded = false

    init(client: ControlAPIClient, textCopier: RecoveryTextCopying = SystemRecoveryTextCopier(), nowProvider: @escaping () -> Date = Date.init) {
        self.client = client
        self.textCopier = textCopier
        self.nowProvider = nowProvider
    }

    var statusMessage: String {
        "\(serviceDisplayState.label): \(serviceDisplayState.helpText)"
    }

    var recoveryActionTitle: String? {
        if let injectionRecovery {
            return injectionRecovery.action.title
        }
        if serviceDisplayState == .unavailable {
            return "Check Local Service"
        }
        return nil
    }

    var menuBarTitle: String {
        "Talka \(serviceDisplayState.label)"
    }

    var pairingStatusText: String {
        if let pairing {
            return pairing.isExpired ? "PIN expired" : "PIN expires in \(pairing.expiryText)"
        }
        return "No active pairing PIN"
    }

    var diagnosticsRows: [(String, String)] {
        [
            ("Control API", client.baseURL.absoluteString),
            ("Service", status?.serviceName ?? "Talka"),
            ("Status", serviceDisplayState.label),
            ("Config Path", status?.configPath.isEmpty == false ? status!.configPath : config.path.isEmpty ? "Not loaded" : config.path),
            ("Uptime", formattedUptime),
            ("Known Devices", String(devices.count)),
            ("Pairing", status?.pairingActive == true ? "Active" : "Idle"),
            ("Recovery Code", injectionRecovery?.diagnosticCode ?? "None"),
            ("Last Refresh", lastUpdated.map(Self.timestampFormatter.string(from:)) ?? "Never")
        ]
    }

    var formattedUptime: String {
        guard let status else {
            return "Unavailable"
        }
        let interval = TimeInterval(status.uptimeSeconds)
        return Self.uptimeFormatter.string(from: interval) ?? "\(status.uptimeSeconds)s"
    }

    func refreshIfNeeded() async {
        guard !hasLoaded else { return }
        hasLoaded = true
        await refresh()
    }

    func refresh() async {
        isBusy = true
        defer { isBusy = false }

        do {
            let fetchedStatus = try await client.fetchStatus()
            async let devices = client.fetchDevices()
            async let config = client.fetchConfig()

            let fetchedDevices = try await devices
            let fetchedConfig = try await config
            let sortedDevices = fetchedDevices.sorted { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }

            self.status = fetchedStatus
            self.devices = sortedDevices
            self.config = fetchedConfig
            self.serviceDisplayState = ServiceDisplayState(apiState: fetchedStatus.state, deviceCount: sortedDevices.count)
            self.injectionRecovery = nil
            self.lastUpdated = nowProvider()
            self.lastErrorMessage = serviceDisplayState == .error ? serviceDisplayState.helpText : nil
        } catch let error as ControlAPIClientError {
            handle(error: error)
        } catch {
            handle(error: ControlAPIClientError.transport(error.localizedDescription))
        }
    }

    func recoverService() async {
        await refresh()
    }

    func performRecoveryAction() async {
        if let injectionRecovery {
            switch injectionRecovery.action {
            case .openAccessibilityGuidance:
                await requestAccessibilityGuidance()
            case .copyFailedText:
                textCopier.copy(injectionRecovery.failedText)
                lastUpdated = nowProvider()
                lastErrorMessage = injectionRecovery.message
            }
            return
        }

        if serviceDisplayState == .unavailable {
            await recoverService()
            return
        }
    }

    func startPairing() async {
        isBusy = true
        defer { isBusy = false }

        do {
            let session = try await client.startPairing()
            pairing = PairingViewModel(session: session, nowProvider: nowProvider)
            lastErrorMessage = nil
            lastUpdated = nowProvider()
            await refresh()
        } catch let error as ControlAPIClientError {
            handle(error: error)
        } catch {
            handle(error: ControlAPIClientError.transport(error.localizedDescription))
        }
    }

    func saveConfig() async {
        isBusy = true
        defer { isBusy = false }

        do {
            config = try await client.saveConfig(config)
            lastUpdated = nowProvider()
            lastErrorMessage = nil
        } catch let error as ControlAPIClientError {
            handle(error: error)
        } catch {
            handle(error: ControlAPIClientError.transport(error.localizedDescription))
        }
    }

    func forgetDevice(id: String) async {
        isBusy = true
        defer { isBusy = false }

        do {
            try await client.forgetDevice(id: id)
            devices.removeAll { $0.id == id }
            if let status {
                self.status = ControlStatus(serviceName: status.serviceName, state: status.state, configPath: status.configPath, uptimeSeconds: status.uptimeSeconds, deviceCount: max(0, status.deviceCount - 1), pairingActive: status.pairingActive)
            }
            serviceDisplayState = status.map { ServiceDisplayState(apiState: $0.state, deviceCount: devices.count) } ?? serviceDisplayState
            lastUpdated = nowProvider()
            lastErrorMessage = nil
        } catch let error as ControlAPIClientError {
            handle(error: error)
        } catch {
            handle(error: ControlAPIClientError.transport(error.localizedDescription))
        }
    }

    func requestAccessibilityGuidance() async {
        isBusy = true
        defer { isBusy = false }

        do {
            accessibilityGuidance = try await client.openAccessibilitySettings()
            lastUpdated = nowProvider()
            lastErrorMessage = nil
        } catch let error as ControlAPIClientError {
            handle(error: error)
        } catch {
            handle(error: ControlAPIClientError.transport(error.localizedDescription))
        }
    }

    private func handle(error: ControlAPIClientError) {
        if case let .invalidResponse(_, serviceError) = error, let recovery = InjectionRecovery(serviceError: serviceError) {
            injectionRecovery = recovery
            lastErrorMessage = recovery.message
        } else {
            injectionRecovery = nil
            lastErrorMessage = error.localizedDescription
        }
        lastUpdated = nowProvider()
        if error == .serviceUnavailable {
            serviceDisplayState = .unavailable
        } else {
            serviceDisplayState = .error
        }
    }

    func setInjectionRecoveryForTesting(_ recovery: InjectionRecovery?) {
        injectionRecovery = recovery
    }

    private static let timestampFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateStyle = .none
        formatter.timeStyle = .medium
        return formatter
    }()

    private static let uptimeFormatter: DateComponentsFormatter = {
        let formatter = DateComponentsFormatter()
        formatter.allowedUnits = [.hour, .minute, .second]
        formatter.unitsStyle = .abbreviated
        formatter.zeroFormattingBehavior = .dropLeading
        return formatter
    }()
}

final class TalkaMacLifecycleDelegate: NSObject, NSApplicationDelegate {
    static var terminateHandler: (() -> Void)?

    func applicationWillTerminate(_ notification: Notification) {
        Self.terminateHandler?()
    }
}

@main
struct TalkaMacApp: App {
    @NSApplicationDelegateAdaptor(TalkaMacLifecycleDelegate.self) private var lifecycleDelegate
    @StateObject private var viewModel: AppShellViewModel
    @StateObject private var serverManager: ServerProcessManager

    init() {
        let sm = ServerProcessManager(configGenerator: EmbeddedRuntimeConfigGenerator())
        let vm = AppShellViewModel(client: LiveControlAPIClient())
        _serverManager = StateObject(wrappedValue: sm)
        _viewModel = StateObject(wrappedValue: vm)

        Task {
            await sm.start()
        }

        TalkaMacLifecycleDelegate.terminateHandler = {
            sm.terminate()
        }
    }

    var body: some Scene {
        MenuBarExtra(viewModel.menuBarTitle, systemImage: viewModel.serviceDisplayState.symbolName) {
            MenuBarContentView(viewModel: viewModel, serverManager: serverManager)
                .frame(minWidth: ShellMetrics.minUtilityWidth)
                .task {
                    await viewModel.refreshIfNeeded()
                }
        }
        .menuBarExtraStyle(.window)

        Settings {
            SettingsShellView(viewModel: viewModel)
                .frame(minWidth: ShellMetrics.minSettingsWidth, minHeight: ShellMetrics.minSettingsHeight)
                .task {
                    await viewModel.refreshIfNeeded()
                }
        }

        Window("Pairing PIN", id: ShellWindowID.pairing) {
            PairingWindowView(viewModel: viewModel)
                .frame(minWidth: ShellMetrics.minUtilityWidth, minHeight: ShellMetrics.minUtilityHeight)
                .task {
                    await viewModel.refreshIfNeeded()
                }
        }

        Window("Diagnostics", id: ShellWindowID.diagnostics) {
            DiagnosticsView(viewModel: viewModel)
                .frame(minWidth: ShellMetrics.minUtilityWidth, minHeight: ShellMetrics.minUtilityHeight)
                .task {
                    await viewModel.refreshIfNeeded()
                }
        }
    }
}

struct MenuBarContentView: View {
    @ObservedObject var viewModel: AppShellViewModel
    @ObservedObject var serverManager: ServerProcessManager
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        Group {
            if serverManager.isRunning {
                VStack(alignment: .leading, spacing: ShellMetrics.panelSpacing) {
                    StatusSummaryCard(viewModel: viewModel)

                    VStack(alignment: .leading, spacing: ShellMetrics.sectionSpacing) {
                        Button("Refresh Status") {
                            Task {
                                await viewModel.refresh()
                            }
                        }

                        Button("Start Pairing") {
                            openWindow(id: ShellWindowID.pairing)
                            Task {
                                await viewModel.startPairing()
                            }
                        }

                        Button("Accessibility Guidance") {
                            Task {
                                await viewModel.requestAccessibilityGuidance()
                            }
                        }

                        Button("Diagnostics") {
                            openWindow(id: ShellWindowID.diagnostics)
                        }

                        SettingsLink {
                            Text("Settings")
                        }
                    }

                    if let actionTitle = viewModel.recoveryActionTitle {
                        Button(actionTitle) {
                            Task {
                                await viewModel.performRecoveryAction()
                            }
                        }
                    }
                }
                .padding(ShellMetrics.contentPadding)
            } else {
                VStack(spacing: 8) {
                    ProgressView()
                        .scaleEffect(0.8)
                    Text("Talka Server Starting...")
                        .font(.body)
                        .foregroundStyle(.secondary)
                }
                .frame(minWidth: ShellMetrics.minUtilityWidth)
                .padding(ShellMetrics.contentPadding)
            }
        }
    }
}

struct SettingsShellView: View {
    @ObservedObject var viewModel: AppShellViewModel
    @Environment(\.openWindow) private var openWindow

    private var isEmbeddedProvider: Bool {
        let provider = viewModel.config.asr.provider.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        return provider == "funasr_embedded" || provider == "funasr_onnx"
    }

    private var isExternalProvider: Bool {
        viewModel.config.asr.provider.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() == "funasr_external"
    }

    private var isContainerProvider: Bool {
        viewModel.config.asr.provider.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() == "funasr_container"
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: ShellMetrics.panelSpacing) {
                StatusSummaryCard(viewModel: viewModel)

                GroupBox("Runtime") {
                    VStack(alignment: .leading, spacing: ShellMetrics.sectionSpacing) {
                        VStack(alignment: .leading, spacing: 6) {
                            Text("ASR Provider")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                            Picker("ASR Provider", selection: $viewModel.config.asr.provider) {
                                ForEach(ASRProviderOption.allCases) { option in
                                    Text(option.title).tag(option.rawValue)
                                }
                            }
                            .pickerStyle(.menu)
                        }
                        LabeledField(title: "ASR Runtime Path", text: $viewModel.config.asr.runtimePath)
                        LabeledField(title: "ASR Host", text: $viewModel.config.asr.host)
                        LabeledField(
                            title: "ASR Port",
                            text: Binding(
                                get: { String(viewModel.config.asr.port) },
                                set: { viewModel.config.asr.port = Int($0) ?? viewModel.config.asr.port }
                            )
                        )
                        LabeledField(title: "ASR Mode", text: $viewModel.config.asr.mode)
                        if isEmbeddedProvider || isContainerProvider {
                            LabeledField(
                                title: "ASR Startup Timeout",
                                text: Binding(
                                    get: { String(viewModel.config.asr.startupTimeoutSeconds) },
                                    set: { viewModel.config.asr.startupTimeoutSeconds = Int($0) ?? viewModel.config.asr.startupTimeoutSeconds }
                                )
                            )
                        }
                        if isEmbeddedProvider || isContainerProvider {
                            LabeledField(title: "ASR Model", text: $viewModel.config.asr.models.asr)
                            LabeledField(title: "ASR Online Model", text: $viewModel.config.asr.models.online)
                            LabeledField(title: "VAD Model", text: $viewModel.config.asr.models.vad)
                            LabeledField(title: "Punctuation Model", text: $viewModel.config.asr.models.punc)
                            LabeledField(title: "ITN Model", text: $viewModel.config.asr.models.itn)
                            LabeledField(title: "LM Model", text: $viewModel.config.asr.models.lm)
                            LabeledField(title: "Hotword File", text: $viewModel.config.asr.hotwordPath)
                        }
                        if isContainerProvider {
                            LabeledField(title: "ASR Container Image", text: $viewModel.config.asr.containerImage)
                            LabeledField(title: "ASR Container Name", text: $viewModel.config.asr.containerName)
                            LabeledField(title: "ASR Download Dir", text: $viewModel.config.asr.downloadDir)
                        }
                        if isExternalProvider {
                            Text("External FunASR mode connects directly to the configured host and port.")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        LabeledField(title: "Ollama Base URL", text: $viewModel.config.llm.baseURL)
                        LabeledField(title: "Ollama Model", text: $viewModel.config.llm.model)
                        LabeledField(title: "Insertion Mode", text: $viewModel.config.injection.mode)
                    }
                    .padding(.top, 4)
                }

                GroupBox("Permissions") {
                    VStack(alignment: .leading, spacing: ShellMetrics.sectionSpacing) {
                        Text("Talka needs Accessibility access before the Go service can insert final text into other apps.")
                            .foregroundStyle(.secondary)
                        Button("Open Accessibility Guidance") {
                            Task {
                                await viewModel.requestAccessibilityGuidance()
                            }
                        }
                        AccessibilityGuidanceView(guidance: viewModel.accessibilityGuidance)
                    }
                    .padding(.top, 4)
                }

                GroupBox("Devices") {
                    VStack(alignment: .leading, spacing: ShellMetrics.sectionSpacing) {
                        if viewModel.devices.isEmpty {
                            Text("No paired devices are currently reported by the control API.")
                                .foregroundStyle(.secondary)
                        } else {
                            ForEach(viewModel.devices) { device in
                                HStack(alignment: .firstTextBaseline) {
                                    VStack(alignment: .leading, spacing: 4) {
                                        Text(device.name)
                                            .font(.headline)
                                        Text(device.id)
                                            .font(.caption)
                                            .foregroundStyle(.secondary)
                                        Text(device.paired ? "Trusted device" : "Unpaired")
                                            .font(.caption)
                                            .foregroundStyle(.secondary)
                                    }
                                    Spacer()
                                    Button("Forget") {
                                        Task {
                                            await viewModel.forgetDevice(id: device.id)
                                        }
                                    }
                                }
                                Divider()
                            }
                        }
                    }
                    .padding(.top, 4)
                }

                GroupBox("Logging") {
                    VStack(alignment: .leading, spacing: ShellMetrics.sectionSpacing) {
                        Toggle("Capture audio", isOn: $viewModel.config.logging.captureAudio)
                        Toggle("Capture transcripts", isOn: $viewModel.config.logging.captureTranscript)
                        Toggle("Debug logging", isOn: Binding(
                            get: { viewModel.config.logging.level == "debug" },
                            set: { viewModel.config.logging.level = $0 ? "debug" : "info" }
                        ))
                    }
                    .padding(.top, 4)
                }

                HStack {
                    Button("Save Configuration") {
                        Task {
                            await viewModel.saveConfig()
                        }
                    }

                    Button("Diagnostics") {
                        openWindow(id: ShellWindowID.diagnostics)
                    }

                    Spacer()

                    if viewModel.isBusy {
                        ProgressView()
                            .controlSize(.small)
                    }
                }
            }
            .padding(ShellMetrics.contentPadding)
        }
    }
}

struct PairingWindowView: View {
    @ObservedObject var viewModel: AppShellViewModel
    @State private var tick = Date()

    var body: some View {
        VStack(alignment: .leading, spacing: ShellMetrics.panelSpacing) {
            StatusSummaryCard(viewModel: viewModel)

            GroupBox("PIN Pairing") {
                VStack(alignment: .leading, spacing: ShellMetrics.sectionSpacing) {
                    if let pairing = viewModel.pairing {
                        Text(pairing.pin)
                            .font(.system(size: 36, weight: .semibold, design: .rounded))
                            .monospacedDigit()
                        Text(viewModel.pairingStatusText)
                            .foregroundStyle(pairing.isExpired ? .red : .secondary)
                        Text("Pairing ID: \(pairing.pairingID)")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    } else {
                        Text("Request a pairing PIN from the local Go control service.")
                            .foregroundStyle(.secondary)
                    }

                    HStack {
                        Button("New PIN") {
                            Task {
                                await viewModel.startPairing()
                            }
                        }

                        if viewModel.isBusy {
                            ProgressView()
                                .controlSize(.small)
                        }
                    }
                }
                .padding(.top, 4)
            }
        }
        .padding(ShellMetrics.contentPadding)
        .onReceive(Timer.publish(every: 1, on: .main, in: .common).autoconnect()) { value in
            tick = value
            viewModel.pairing?.refreshCountdown()
        }
    }
}

struct DiagnosticsView: View {
    @ObservedObject var viewModel: AppShellViewModel

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: ShellMetrics.panelSpacing) {
                StatusSummaryCard(viewModel: viewModel)

                GroupBox("Diagnostics") {
                    VStack(alignment: .leading, spacing: ShellMetrics.sectionSpacing) {
                        ForEach(Array(viewModel.diagnosticsRows.enumerated()), id: \.offset) { _, row in
                            LabeledContent(row.0) {
                                Text(row.1)
                                    .font(.body.monospacedDigit())
                            }
                        }
                    }
                    .padding(.top, 4)
                }

                GroupBox("Accessibility Guidance") {
                    VStack(alignment: .leading, spacing: ShellMetrics.sectionSpacing) {
                        AccessibilityGuidanceView(guidance: viewModel.accessibilityGuidance)
                        Button("Refresh Guidance") {
                            Task {
                                await viewModel.requestAccessibilityGuidance()
                            }
                        }
                    }
                    .padding(.top, 4)
                }

                if let recovery = viewModel.injectionRecovery {
                    GroupBox("Insertion Recovery") {
                        RecoveryStateView(recovery: recovery)
                            .padding(.top, 4)
                    }
                }
            }
            .padding(ShellMetrics.contentPadding)
        }
    }
}

struct StatusSummaryCard: View {
    @ObservedObject var viewModel: AppShellViewModel

    var body: some View {
        VStack(alignment: .leading, spacing: ShellMetrics.sectionSpacing) {
            HStack(alignment: .center, spacing: 10) {
                Image(systemName: viewModel.serviceDisplayState.symbolName)
                    .foregroundStyle(viewModel.serviceDisplayState.tint)
                VStack(alignment: .leading, spacing: 4) {
                    Text(viewModel.serviceDisplayState.label)
                        .font(.headline)
                    Text(viewModel.statusMessage)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                if viewModel.isBusy {
                    ProgressView()
                        .controlSize(.small)
                }
            }

            if let message = viewModel.lastErrorMessage, !message.isEmpty {
                Text(message)
                    .font(.caption)
                    .foregroundStyle(viewModel.serviceDisplayState == .error ? .red : .secondary)
            }

            if let recovery = viewModel.injectionRecovery {
                Text("Diagnostic code: \(recovery.diagnosticCode)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(ShellMetrics.contentPadding)
        .background(.quinary, in: RoundedRectangle(cornerRadius: ShellMetrics.cornerRadius, style: .continuous))
    }
}

struct AccessibilityGuidanceView: View {
    let guidance: AccessibilityGuidance?

    var body: some View {
        if let guidance {
            VStack(alignment: .leading, spacing: ShellMetrics.sectionSpacing) {
                Text(guidance.message)
                if let url = URL(string: guidance.settingsURL) {
                    Link(guidance.settingsURL, destination: url)
                        .font(.caption)
                }
                Text(guidance.opened ? "System Settings opened from the control API." : "The control API returned guidance without opening System Settings automatically.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        } else {
            Text("Request guidance from the local control API to confirm the expected Accessibility permission path.")
                .foregroundStyle(.secondary)
        }
    }
}

struct RecoveryStateView: View {
    let recovery: InjectionRecovery

    var body: some View {
        VStack(alignment: .leading, spacing: ShellMetrics.sectionSpacing) {
            Text(recovery.message)
            Text("Diagnostic code: \(recovery.diagnosticCode)")
                .font(.caption)
                .foregroundStyle(.secondary)
            if !recovery.failedText.isEmpty {
                Text(recovery.failedText)
                    .font(.body.monospaced())
            }
            Text("Primary action: \(recovery.action.title)")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }
}

struct LabeledField: View {
    let title: String
    @Binding var text: String

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title)
                .font(.caption)
                .foregroundStyle(.secondary)
            TextField(title, text: $text)
                .textFieldStyle(.roundedBorder)
        }
    }
}
