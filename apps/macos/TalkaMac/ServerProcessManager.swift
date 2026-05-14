import AppKit
import Darwin
import Foundation

protocol RuntimeConfigGenerator {
    func generateConfig() throws -> URL
}

@MainActor
final class ServerProcessManager: ObservableObject {
    enum PortReuseAction: Equatable {
        case reuseExistingServer
        case replaceExistingServer
    }

    @Published var isRunning = false
    @Published var lastError: String?

    private var process: Process?
    private let configGenerator: RuntimeConfigGenerator
    private let port: Int
    private let pasteBrokerSocketPath: String?
    private var restartCount = 0
    private let maxRestarts = 3
    private var restartTask: Task<Void, Never>?
    private var isTerminating = false

    init(configGenerator: RuntimeConfigGenerator, port: Int = 8080, pasteBrokerSocketPath: String? = nil) {
        self.configGenerator = configGenerator
        self.port = port
        self.pasteBrokerSocketPath = pasteBrokerSocketPath
    }

    func start() async {
        guard !isRunning else { return }
        isTerminating = false
        lastError = nil

        guard !isPortInUse(port: port) else {
            await checkPortReuse()
            return
        }

        startManagedServer()
    }

    private func startManagedServer() {
        let configURL: URL
        do {
            configURL = try configGenerator.generateConfig()
        } catch {
            lastError = "Failed to generate server config: \(error.localizedDescription)"
            return
        }

        guard let serverURL = locateServer() else {
            lastError = "Server executable not found in app bundle"
            return
        }

        restartCount = 0
        launch(executableURL: serverURL, configPath: configURL.path)
    }

    private func checkPortReuse() async {
        let statusURL = URL(string: "http://127.0.0.1:\(port)/v1/status")!
        let (data, response): (Data, URLResponse)
        do {
            (data, response) = try await URLSession.shared.data(from: statusURL)
        } catch {
            lastError = "Port \(port) is in use by another application"
            return
        }

        guard let httpResponse = response as? HTTPURLResponse,
              (200...299).contains(httpResponse.statusCode) else {
            lastError = "Port \(port) is in use by another application"
            return
        }

        guard let action = Self.portReuseAction(
            data: data,
            response: httpResponse,
            currentPasteBrokerSocketPath: pasteBrokerSocketPath
        ) else {
            lastError = "Port \(port) is in use by another application"
            return
        }

        switch action {
        case .reuseExistingServer:
            isRunning = true
        case .replaceExistingServer:
            terminateProcessListening(on: port)
            try? await Task.sleep(nanoseconds: 500_000_000)
            guard !isPortInUse(port: port) else {
                lastError = "Port \(port) is in use by a stale Talka service"
                return
            }
            startManagedServer()
        }
    }

    func terminate() {
        isTerminating = true
        restartTask?.cancel()
        restartTask = nil
        restartCount = 0

        if let process, process.isRunning {
            process.terminate()
        }

        DispatchQueue.main.asyncAfter(deadline: .now() + 5) { [weak self] in
            if let self, let process = self.process, process.isRunning {
                kill(process.processIdentifier, SIGKILL)
            }
            self?.process = nil
            self?.isRunning = false
            self?.isTerminating = false
        }
    }

    private func locateServer() -> URL? {
        if let envPath = ProcessInfo.processInfo.environment["TALKA_SERVER_PATH"],
           let url = URL(string: envPath) {
            return url
        }

        let bundleResources = Bundle.main.bundleURL.appendingPathComponent("Contents/Resources/talka-server")
        if FileManager.default.isExecutableFile(atPath: bundleResources.path) {
            return bundleResources
        }

        let devPath = Bundle.main.bundleURL
            .deletingLastPathComponent()
            .appendingPathComponent("talka-server")
        if FileManager.default.isExecutableFile(atPath: devPath.path) {
            return devPath
        }

        return nil
    }

    private func launch(executableURL: URL, configPath: String) {
        let process = Process()
        process.executableURL = executableURL
        process.arguments = ["-config", configPath]
        if let pasteBrokerSocketPath {
            var environment = ProcessInfo.processInfo.environment
            environment["TALKA_PASTE_BROKER_SOCKET"] = pasteBrokerSocketPath
            process.environment = environment
        }
    let logDir = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!.appendingPathComponent("Talka/logs")
    try? FileManager.default.createDirectory(at: logDir, withIntermediateDirectories: true)
    let logPath = logDir.appendingPathComponent("talka-server.log").path
    if !FileManager.default.fileExists(atPath: logPath) {
        FileManager.default.createFile(atPath: logPath, contents: nil)
    }
    if let logFile = FileHandle(forWritingAtPath: logPath) {
        logFile.seekToEndOfFile()
        process.standardOutput = logFile
        process.standardError = logFile
    } else {
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
    }

        process.terminationHandler = { [weak self] proc in
            DispatchQueue.main.async {
                self?.handleTermination(exitCode: proc.terminationStatus, configPath: configPath)
            }
        }

        do {
            try process.run()
            self.process = process
            isRunning = true
        } catch {
            lastError = "Failed to launch server: \(error.localizedDescription)"
        }
    }

    private func handleTermination(exitCode: Int32, configPath: String) {
        guard isRunning else { return }

        isRunning = false
        process = nil

        if isTerminating {
            isTerminating = false
            return
        }

        guard exitCode != 0, restartCount < maxRestarts else {
            restartCount = 0
            return
        }

        restartCount += 1
        let delay: UInt64 = UInt64(1 << (restartCount - 1))
        restartTask = Task {
            try? await Task.sleep(nanoseconds: delay * 1_000_000_000)
            guard !Task.isCancelled, let url = locateServer() else { return }
            launch(executableURL: url, configPath: configPath)
        }
    }

    private func isPortInUse(port: Int) -> Bool {
        let sock = socket(AF_INET, SOCK_STREAM, 0)
        guard sock >= 0 else { return true }
        defer { close(sock) }

        var addr = sockaddr_in()
        addr.sin_family = sa_family_t(AF_INET)
        addr.sin_port = UInt16(port).bigEndian
        addr.sin_addr.s_addr = inet_addr("127.0.0.1")

        let result = withUnsafeMutablePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                connect(sock, $0, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }

        return result == 0
    }

    private func terminateProcessListening(on port: Int) {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/sbin/lsof")
        process.arguments = ["-ti", "tcp:\(port)", "-sTCP:LISTEN"]
        let output = Pipe()
        process.standardOutput = output
        process.standardError = FileHandle.nullDevice

        do {
            try process.run()
            process.waitUntilExit()
        } catch {
            return
        }

        let data = output.fileHandleForReading.readDataToEndOfFile()
        guard let text = String(data: data, encoding: .utf8) else { return }
        for line in text.split(whereSeparator: \.isNewline) {
            guard let pid = Int32(String(line).trimmingCharacters(in: .whitespacesAndNewlines)) else { continue }
            kill(pid, SIGTERM)
        }
    }

    static func portReuseAction(data: Data, response: URLResponse, currentPasteBrokerSocketPath: String? = nil) -> PortReuseAction? {
        guard let httpResponse = response as? HTTPURLResponse,
              (200...299).contains(httpResponse.statusCode),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let serviceName = json["service_name"] as? String,
              serviceName == "Talka" else {
            return nil
        }

        if let currentPasteBrokerSocketPath {
            let injection = json["injection"] as? [String: Any]
            let existingPasteBrokerSocketPath = injection?["paste_broker_socket"] as? String
            guard existingPasteBrokerSocketPath == currentPasteBrokerSocketPath else {
                return .replaceExistingServer
            }
        }

        return .reuseExistingServer
    }
}
