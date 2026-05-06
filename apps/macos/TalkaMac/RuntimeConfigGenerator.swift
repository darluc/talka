import Foundation

struct EmbeddedRuntimeConfigGenerator: RuntimeConfigGenerator {
    func generateConfig() throws -> URL {
        let fileManager = FileManager.default
        let configURL = try configURL(fileManager: fileManager)
        let configDir = configURL.deletingLastPathComponent()
        try fileManager.createDirectory(at: configDir, withIntermediateDirectories: true)
        if fileManager.fileExists(atPath: configURL.path) {
            return configURL
        }

        let resourcesURL = try runtimeResourcesURL()
        let yaml = defaultConfigYAML(resourcesURL: resourcesURL)

        try yaml.write(to: configURL, atomically: true, encoding: .utf8)
        return configURL
    }

    private func configURL(fileManager: FileManager) throws -> URL {
        if let override = ProcessInfo.processInfo.environment["TALKA_CONFIG_PATH"], !override.isEmpty {
            return URL(fileURLWithPath: override)
        }

        guard let appSupport = fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask).first else {
            throw NSError(
                domain: "EmbeddedRuntimeConfigGenerator",
                code: 1,
                userInfo: [NSLocalizedDescriptionKey: "Unable to locate Application Support directory"]
            )
        }

        return appSupport.appendingPathComponent("Talka/config.yaml")
    }

    private func runtimeResourcesURL() throws -> URL {
        if let override = ProcessInfo.processInfo.environment["TALKA_RESOURCES_PATH"], !override.isEmpty {
            return URL(fileURLWithPath: override, isDirectory: true)
        }

        if let resourceURL = Bundle.main.resourceURL {
            return resourceURL
        }

        throw NSError(
            domain: "EmbeddedRuntimeConfigGenerator",
            code: 2,
            userInfo: [NSLocalizedDescriptionKey: "Unable to locate app bundle resources"]
        )
    }

    private func defaultConfigYAML(resourcesURL: URL) -> String {
        let runtimeURL = resourcesURL.appendingPathComponent("talka-asr-runtime")
        let modelsURL = resourcesURL.appendingPathComponent("models/funasr")
        let hotwordsURL = resourcesURL.appendingPathComponent("hotwords.txt")

        return """
        server:
          bind_host: 0.0.0.0
          port: 8080
          service_name: Talka

        asr:
          provider: funasr_embedded
          runtime_path: \(runtimeURL.path)
          host: 127.0.0.1
          port: 10095
          mode: 2pass
          sample_rate: 16000
          startup_timeout_seconds: 30
          container_image: ""
          container_name: ""
          download_dir: ""
          hotword_path: \(hotwordsURL.path)
          models:
            asr: \(modelsURL.appendingPathComponent("paraformer-zh-onnx").path)
            online: \(modelsURL.appendingPathComponent("paraformer-zh-online-onnx").path)
            vad: \(modelsURL.appendingPathComponent("fsmn-vad-onnx").path)
            punc: \(modelsURL.appendingPathComponent("ct-punc-onnx").path)
            itn: \(modelsURL.appendingPathComponent("itn-zh").path)
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
    }
}
