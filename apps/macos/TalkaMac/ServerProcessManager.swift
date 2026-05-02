import AppKit
import Darwin
import Foundation

protocol RuntimeConfigGenerator {
    func generateConfig() throws -> URL
}

@MainActor
final class ServerProcessManager: ObservableObject {
    @Published var isRunning = false
    @Published var lastError: String?

    private var process: Process?
    private let configGenerator: RuntimeConfigGenerator
    private let port: Int
    private var restartCount = 0
    private let maxRestarts = 3
    private var restartTask: Task<Void, Never>?
    private var isTerminating = false

    init(configGenerator: RuntimeConfigGenerator, port: Int = 8080) {
        self.configGenerator = configGenerator
        self.port = port
    }

    func start() async {
        guard !isRunning else { return }
        isTerminating = false
        lastError = nil

        guard !isPortInUse(port: port) else {
            await checkPortReuse()
            return
        }

        let configURL: URL
        do {
            configURL = try configGenerator.generateConfig()
        } catch {
            return
        }

        guard let serverURL = locateServer() else { return }

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

        guard let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              json["service_name"] != nil else {
            lastError = "Port \(port) is in use by another application"
            return
        }

        isRunning = true
    }

    func terminate() {
        isTerminating = true
        restartTask?.cancel()
        restartTask = nil
        restartCount = 0

        guard let process, process.isRunning else {
            process = nil
            isRunning = false
            isTerminating = false
            return
        }

        process.terminate()

        DispatchQueue.main.asyncAfter(deadline: .now() + 5) { [weak self] in
            guard let self, let process = self.process, process.isRunning else {
                self?.isTerminating = false
                return
            }
            kill(process.processIdentifier, SIGKILL)
            self.isTerminating = false
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
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice

        process.terminationHandler = { [weak self] proc in
            DispatchQueue.main.async {
                self?.handleTermination(exitCode: proc.terminationStatus, configPath: configPath)
            }
        }

        do {
            try process.run()
            self.process = process
            isRunning = true
        } catch {}
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
}
