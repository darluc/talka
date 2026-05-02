import Foundation

struct SidecarRuntimeConfigGenerator: RuntimeConfigGenerator {
    func generateConfig() throws -> URL {
        let runtimePath = Bundle.main.bundleURL
            .appendingPathComponent("Contents/Resources/talka-asr-runtime")
            .path

        let fileManager = FileManager.default
        guard let appSupport = fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask).first else {
            throw NSError(
                domain: "SidecarRuntimeConfigGenerator",
                code: 1,
                userInfo: [NSLocalizedDescriptionKey: "Unable to locate Application Support directory"]
            )
        }

        let talkaDir = appSupport.appendingPathComponent("Talka")
        try fileManager.createDirectory(at: talkaDir, withIntermediateDirectories: true)

        let configURL = talkaDir.appendingPathComponent("config.yaml")

        let yaml = """
        server:
          bind_host: 0.0.0.0
          port: 8080
          service_name: Talka

        asr:
          provider: sidecar
          host: 127.0.0.1
          port: 19095
          runtime_path: \(runtimePath)
          mode: twopass
          sample_rate: 16000

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

        try yaml.write(to: configURL, atomically: true, encoding: .utf8)
        return configURL
    }
}
