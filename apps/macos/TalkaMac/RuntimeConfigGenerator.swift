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
			let originalYAML = yaml

			yaml = removeMalformedEmbeddedResourceLines(from: yaml)
			yaml = removeDuplicatedEmbeddedASRProvider(from: yaml)
            yaml = replaceASRBlock(in: yaml, resourcesURL: resourcesURL, preservingProviderFrom: originalYAML)

        if yaml != originalYAML {
            try yaml.write(to: configURL, atomically: true, encoding: .utf8)
        }
    }

	private func isEmbeddedResourceConfig(_ yaml: String) -> Bool {
		yaml.contains("provider: funasr_embedded")
		|| yaml.contains("provider: funasr")
		|| yaml.contains("provider: funasr_external")
		|| yaml.contains("provider: funasr_onnx")
		|| yaml.contains("talka-asr-runtime")
		|| yaml.contains("funasr-wss-server-2pass")
		|| yaml.contains("/models/funasr/")
		|| yaml.contains("provider: sherpa_onnx_streaming")
		|| yaml.contains("provider: onnx")
		|| yaml.contains("/models/sherpa-onnx/")
	}

    private func removeMalformedEmbeddedResourceLines(from yaml: String) -> String {
        yaml.replacingOccurrences(
            of: #"(?m)^  /.*(?:/Contents/Resources/|/models/funasr/).*\n?"#,
            with: "",
            options: .regularExpression
        )
    }

    private func removeDuplicatedEmbeddedASRProvider(from yaml: String) -> String {
        yaml.replacingOccurrences(
            of: #"(?m)^  provider: (?:funasr|funasr_embedded)\n(?=    provider: (?:funasr|funasr_embedded)$)"#,
            with: "",
            options: .regularExpression
        )
    }

    private func ensureEmbeddedASRProvider(in yaml: String) -> String {
        if yaml.range(of: #"(?m)^asr:\n[ \t]+provider:"#, options: .regularExpression) != nil {
            return yaml
        }

        return yaml.replacingOccurrences(
            of: #"(?m)^asr:\n"#,
            with: "asr:\n  provider: funasr\n",
            options: .regularExpression
        )
    }

    private func replaceASRBlock(in yaml: String, resourcesURL: URL, preservingProviderFrom originalYAML: String? = nil) -> String {
        let sourceYAML = originalYAML ?? yaml
        let block = defaultASRYAML(
            resourcesURL: resourcesURL,
            preferredProvider: preferredASRProvider(in: sourceYAML),
            preferredSherpaProfile: preferredSherpaProfile(in: sourceYAML)
        )
        if yaml.range(of: #"(?ms)^asr:\n.*?(?=^llm:\n)"#, options: .regularExpression) != nil {
            return yaml.replacingOccurrences(
                of: #"(?ms)^asr:\n.*?(?=^llm:\n)"#,
                with: block + "\n",
                options: .regularExpression
            )
        }

        return yaml.replacingOccurrences(
            of: #"(?m)^server:\n"#,
            with: "server:\n",
            options: .regularExpression
        ) + "\n" + block
    }

    private func replaceYAMLValue(named key: String, indent: String, with value: String, in yaml: String) -> String {
        let escapedKey = NSRegularExpression.escapedPattern(for: key)
        let indentPattern = #"[ \t]{\#(indent.count),}"#
        return yaml.replacingOccurrences(
            of: #"(?m)^(\#(indentPattern)\#(escapedKey):\s*).*$"#,
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
		return """
server:
  bind_host: 0.0.0.0
  port: 8080
  service_name: Talka

\(defaultASRYAML(resourcesURL: resourcesURL))

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

    private func defaultASRYAML(resourcesURL: URL, preferredProvider: String? = nil, preferredSherpaProfile: String? = nil) -> String {
        let provider = preferredProvider ?? "auto"
        if provider != "funasr", let sherpaModel = availableSherpaModel(resourcesURL: resourcesURL, preferredProfile: preferredSherpaProfile) {
            return sherpaASRYAML(resourcesURL: resourcesURL, model: sherpaModel)
        }
        return funasrASRYAML(resourcesURL: resourcesURL)
    }

    private func preferredASRProvider(in yaml: String) -> String? {
        if yaml.range(of: #"(?m)^\s*provider:\s*(funasr|funasr_embedded|funasr_external|funasr_container|sidecar)\s*$"#, options: .regularExpression) != nil {
            return "funasr"
        }
        if yaml.range(of: #"(?m)^\s*provider:\s*(onnx|sherpa|sherpa_onnx_streaming)\s*$"#, options: .regularExpression) != nil {
            return "onnx"
        }
        return nil
    }

    private func preferredSherpaProfile(in yaml: String) -> String? {
        if yaml.range(of: #"(?m)^\s*model_profile:\s*paraformer-bilingual\s*$"#, options: .regularExpression) != nil ||
            yaml.contains("streaming-paraformer-bilingual-zh-en") {
            return "paraformer-bilingual"
        }
        if yaml.range(of: #"(?m)^\s*model_profile:\s*paraformer-trilingual\s*$"#, options: .regularExpression) != nil ||
            yaml.contains("streaming-paraformer-trilingual-zh-cantonese-en") {
            return "paraformer-trilingual"
        }
        return nil
    }

    private struct SherpaModelBundle {
        let profile: String
        let modelType: String
        let precision: String
        let directory: URL
        let encoderFile: String
        let decoderFile: String
        let joinerFile: String
    }

    private func availableSherpaModel(resourcesURL: URL, preferredProfile: String? = nil) -> SherpaModelBundle? {
        let frameworksURL = resourcesURL.deletingLastPathComponent().appendingPathComponent("Frameworks")
        let dylibs = (try? FileManager.default.contentsOfDirectory(atPath: frameworksURL.path)) ?? []
        let hasLibrary = dylibs.contains { $0.hasPrefix("libsherpa-onnx-c-api") && $0.hasSuffix(".dylib") }
        guard hasLibrary else { return nil }

        let candidates = [
            SherpaModelBundle(
                profile: "paraformer-trilingual",
                modelType: "paraformer",
                precision: "int8",
                directory: resourcesURL.appendingPathComponent("models/sherpa-onnx/streaming-paraformer-trilingual-zh-cantonese-en"),
                encoderFile: "encoder.int8.onnx",
                decoderFile: "decoder.int8.onnx",
                joinerFile: ""
            ),
            SherpaModelBundle(
                profile: "paraformer-bilingual",
                modelType: "paraformer",
                precision: "int8",
                directory: resourcesURL.appendingPathComponent("models/sherpa-onnx/streaming-paraformer-bilingual-zh-en"),
                encoderFile: "encoder.int8.onnx",
                decoderFile: "decoder.int8.onnx",
                joinerFile: ""
            ),
        ]

        let orderedCandidates: [SherpaModelBundle]
        if let preferredProfile, let preferred = candidates.first(where: { $0.profile == preferredProfile }) {
            orderedCandidates = [preferred] + candidates.filter { $0.profile != preferredProfile }
        } else {
            orderedCandidates = candidates
        }

        return orderedCandidates.first { model in
            var requiredModelFiles = [
                model.directory.appendingPathComponent("tokens.txt"),
                model.directory.appendingPathComponent(model.encoderFile),
                model.directory.appendingPathComponent(model.decoderFile),
            ]
            if !model.joinerFile.isEmpty {
                requiredModelFiles.append(model.directory.appendingPathComponent(model.joinerFile))
            }
            return requiredModelFiles.allSatisfy { FileManager.default.fileExists(atPath: $0.path) }
        }
    }

    private func sherpaASRYAML(resourcesURL: URL, model: SherpaModelBundle) -> String {
        let runtimeURL = resourcesURL.appendingPathComponent("talka-asr-runtime")
        let funasrBinaryURL = resourcesURL.appendingPathComponent("funasr-wss-server-2pass")
        let funasrModelsURL = resourcesURL.appendingPathComponent("models/funasr")
        let hotwordsPath = hotwordsPath(resourcesURL: resourcesURL)
        let joinerPath = model.joinerFile.isEmpty ? "" : model.directory.appendingPathComponent(model.joinerFile).path

        return """
asr:
  provider: onnx
  runtime_path: \(runtimeURL.path)
  funasr_binary_path: \(funasrBinaryURL.path)
  host: 127.0.0.1
  port: 10095
  mode: streaming
  sample_rate: 16000
  startup_timeout_seconds: 180
  container_image: ""
  container_name: ""
  download_dir: ""
  hotword_path: \(hotwordsPath)
  models:
    asr: \(funasrModelsURL.appendingPathComponent("paraformer-zh-onnx").path)
    online: \(funasrModelsURL.appendingPathComponent("paraformer-zh-online-onnx").path)
    vad: \(funasrModelsURL.appendingPathComponent("fsmn-vad-onnx").path)
    punc: \(funasrModelsURL.appendingPathComponent("ct-punc-onnx").path)
    itn: \(funasrModelsURL.appendingPathComponent("itn-zh").path)
    lm: ""
  sherpa_onnx:
    model_profile: \(model.profile)
    model_type: \(model.modelType)
    precision: \(model.precision)
    tokens_path: \(model.directory.appendingPathComponent("tokens.txt").path)
    encoder_path: \(model.directory.appendingPathComponent(model.encoderFile).path)
    decoder_path: \(model.directory.appendingPathComponent(model.decoderFile).path)
    joiner_path: \(joinerPath)
    num_threads: 2
    decoding_method: greedy_search
    feature_dim: 80
    provider: cpu
"""
    }

    private func funasrASRYAML(resourcesURL: URL) -> String {
        let runtimeURL = resourcesURL.appendingPathComponent("talka-asr-runtime")
        let funasrBinaryURL = resourcesURL.appendingPathComponent("funasr-wss-server-2pass")
        let modelsURL = resourcesURL.appendingPathComponent("models/funasr")
        let hotwordsPath = hotwordsPath(resourcesURL: resourcesURL)

        return """
asr:
  provider: funasr
  runtime_path: \(runtimeURL.path)
  funasr_binary_path: \(funasrBinaryURL.path)
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
  sherpa_onnx:
    model_profile: paraformer-trilingual
    model_type: paraformer
    precision: int8
    tokens_path: \(resourcesURL.appendingPathComponent("models/sherpa-onnx/streaming-paraformer-trilingual-zh-cantonese-en/tokens.txt").path)
    encoder_path: \(resourcesURL.appendingPathComponent("models/sherpa-onnx/streaming-paraformer-trilingual-zh-cantonese-en/encoder.int8.onnx").path)
    decoder_path: \(resourcesURL.appendingPathComponent("models/sherpa-onnx/streaming-paraformer-trilingual-zh-cantonese-en/decoder.int8.onnx").path)
    joiner_path: ""
    num_threads: 2
    decoding_method: greedy_search
    feature_dim: 80
    provider: cpu
"""
    }
}
