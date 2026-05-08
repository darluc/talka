import Foundation

struct EmbeddedRuntimeConfigGenerator: RuntimeConfigGenerator {
    func generateConfig() throws -> URL {
        let fileManager = FileManager.default
        let configURL = try configURL(fileManager: fileManager)
        let configDir = configURL.deletingLastPathComponent()
        try fileManager.createDirectory(at: configDir, withIntermediateDirectories: true)
        if fileManager.fileExists(atPath: configURL.path) {
            try refreshEmbeddedResourcePathsIfNeeded(at: configURL)
            return configURL
        }

        let resourcesURL = try runtimeResourcesURL()
        let yaml = defaultConfigYAML(resourcesURL: resourcesURL)

        try yaml.write(to: configURL, atomically: true, encoding: .utf8)
        return configURL
    }

    private func refreshEmbeddedResourcePathsIfNeeded(at configURL: URL) throws {
        var yaml = try String(contentsOf: configURL, encoding: .utf8)
        guard isEmbeddedResourceConfig(yaml) else {
            return
        }

        let resourcesURL = try runtimeResourcesURL()
        let runtimeURL = resourcesURL.appendingPathComponent("talka-asr-runtime")
        let modelsURL = resourcesURL.appendingPathComponent("models/funasr")
        let hotwordsPath = hotwordsPath(resourcesURL: resourcesURL)
        let originalYAML = yaml

        yaml = removeMalformedEmbeddedResourceLines(from: yaml)
        yaml = ensureEmbeddedASRProvider(in: yaml)
        yaml = replaceYAMLValue(named: "runtime_path", indent: "  ", with: runtimeURL.path, in: yaml)
        yaml = replaceYAMLValue(named: "hotword_path", indent: "  ", with: hotwordsPath, in: yaml)
        yaml = replaceYAMLValue(named: "asr", indent: "    ", with: modelsURL.appendingPathComponent("paraformer-zh-onnx").path, in: yaml)
        yaml = replaceYAMLValue(named: "online", indent: "    ", with: modelsURL.appendingPathComponent("paraformer-zh-online-onnx").path, in: yaml)
        yaml = replaceYAMLValue(named: "vad", indent: "    ", with: modelsURL.appendingPathComponent("fsmn-vad-onnx").path, in: yaml)
        yaml = replaceYAMLValue(named: "punc", indent: "    ", with: modelsURL.appendingPathComponent("ct-punc-onnx").path, in: yaml)
        yaml = replaceYAMLValue(named: "itn", indent: "    ", with: modelsURL.appendingPathComponent("itn-zh").path, in: yaml)

        if yaml != originalYAML {
            try yaml.write(to: configURL, atomically: true, encoding: .utf8)
        }
    }

    private func isEmbeddedResourceConfig(_ yaml: String) -> Bool {
        yaml.contains("provider: funasr_embedded")
            || yaml.contains("provider: funasr_onnx")
            || yaml.contains("talka-asr-runtime")
            || yaml.contains("/models/funasr/")
    }

    private func removeMalformedEmbeddedResourceLines(from yaml: String) -> String {
        yaml.replacingOccurrences(
            of: #"(?m)^  /.*(?:/Contents/Resources/|/models/funasr/).*\n?"#,
            with: "",
            options: .regularExpression
        )
    }

    private func ensureEmbeddedASRProvider(in yaml: String) -> String {
        if yaml.range(of: #"(?m)^asr:\n  provider:"#, options: .regularExpression) != nil {
            return yaml
        }

        return yaml.replacingOccurrences(
            of: #"(?m)^asr:\n"#,
            with: "asr:\n  provider: funasr_embedded\n",
            options: .regularExpression
        )
    }

    private func replaceYAMLValue(named key: String, indent: String, with value: String, in yaml: String) -> String {
        yaml.replacingOccurrences(
            of: #"(?m)^(\#(indent)\#(key):\s*).*$"#,
            with: "$1\(value)",
            options: .regularExpression
        )
    }

    private func hotwordsPath(resourcesURL: URL) -> String {
        let hotwordsURL = resourcesURL.appendingPathComponent("hotwords.txt")
        guard FileManager.default.fileExists(atPath: hotwordsURL.path) else {
            return "\"\""
        }
        return hotwordsURL.path
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
        let hotwordsPath = hotwordsPath(resourcesURL: resourcesURL)

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
          startup_timeout_seconds: 180
          container_image: ""
          container_name: ""
          download_dir: ""
          hotword_path: \(hotwordsPath)
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
