# Talka

[English](README.md)

Talka 是一个本地优先的 macOS + iOS 语音输入系统。iPhone 作为远程麦克风，通过局域网把音频流传给已配对的 Mac；Mac 负责转写、可选的文本清理，并把最终文本插入到当前正在使用的应用里。

这个项目面向个人听写、短文本写作、聊天回复、笔记和代码相关输入场景。它的核心目标是保留本地控制权：音频留在局域网内，ASR 在 Mac 上运行，文本清理可以使用本地 Ollama 服务或其它 OpenAI-compatible endpoint。

## 功能

- iOS 远程麦克风，支持配对、重连和紧凑的录音界面。
- macOS 菜单栏应用，提供服务状态、配对 PIN、已连接设备、诊断和运行时设置。
- 通过打包的 `sherpa-onnx` runtime assets 和中英 Paraformer 模型配置实现本地流式 ASR。
- 可选的 LLM 文本清理，支持配置 Ollama/OpenAI-compatible API endpoint。
- 使用 PIN 确认的密钥交换和加密音频/控制消息建立安全配对会话。
- 通过原生 macOS paste broker 插入文本，在修改剪贴板前检查 Accessibility 权限。
- iOS 端提供直接发送 Return 键的快捷按钮，可组合 Cmd、Alt 和 Shift，并绕过 ASR/LLM pipeline。

## 当前状态

Talka 仍处于早期阶段。macOS 和 iOS 应用已经可用于本地开发和内部测试，但 release packaging 仍然保持简单：

- GitHub Actions 当前只构建 unsigned/ad-hoc 的 macOS artifacts。
- iOS 构建预期通过本地 Xcode 或辅助脚本完成。
- 默认 workflow 不启用 Apple Developer ID 签名、notarization 或 iOS IPA 分发。

## 架构

Talka 主要由三部分组成：

- `apps/macos/TalkaMac`：SwiftUI 菜单栏应用、设置界面、诊断、进程管理和原生 paste broker。
- `apps/ios/TalkaIOS`：SwiftUI iOS 应用，负责发现、配对、麦克风采集、录音控制和快捷按钮。
- `cmd/talka-server` 与 `internal/*`：Go control service、配对/会话协议、音频 pipeline、ASR/LLM provider、mDNS 广播和文本插入编排。

运行时流程：

1. Mac 在局域网内广播 `_talka._tcp` 服务。
2. iPhone 发现 Mac，通过短 PIN 完成配对，并建立加密会话。
3. iPhone 将 PCM audio frames 发送到 Go service。
4. Mac 把音频输入本地 `sherpa-onnx` recognizer。
5. 最终 transcript 可选地交给配置的 LLM endpoint 清理。
6. Go service 请求 Swift paste broker 把文本插入当前 macOS app。

更多设计和技术细节：

- [产品行为和 UX](docs/product-design.md)
- [运行时和传输架构](docs/technical-architecture.md)
- [工程里程碑](docs/development-plan.md)

## 环境要求

- 安装 Xcode 的 macOS。
- Go 1.24 或更新版本。
- `xcodebuild`、`xcrun`、`swift`、`python3` 和 `shasum`。
- 可选：如果需要本地 LLM 文本清理，需要运行 Ollama。
- 真实 ASR 构建需要通过项目脚本准备 `sherpa-onnx` runtime assets 和模型文件。

## 快速开始

克隆仓库：

```sh
git clone https://github.com/darluc/talka.git
cd talka
```

准备本地 runtime assets：

```sh
./scripts/build-sherpa-onnx-runtime.sh
SHERPA_ONNX_MODEL_PROFILE=bilingual ./scripts/download-sherpa-onnx-model.sh
mkdir -p .sisyphus/evidence
```

检查开发环境：

```sh
./scripts/setup-dev.sh --verify-only
```

运行完整本地测试：

```sh
./scripts/test-all.sh
```

## 构建

构建并打包 macOS app：

```sh
./scripts/package-macos-app.sh --arch arm64
./scripts/package-macos-app.sh --arch x86_64
```

构建产物会写入 `dist/`：

- `dist/TalkaMac-macOS-arm64.zip`
- `dist/TalkaMac-macOS-x86_64.zip`

通过 Xcode 本地构建 iOS：

```sh
open apps/Talka.xcworkspace
```

如果已经连接开发设备，并且本地 Apple signing 设置有效，可以用辅助脚本构建并安装 iOS app：

```sh
./scripts/deploy-ios.sh
```

## Release Workflow

推送 `v` 或 `alpha.` 开头的 tag 会触发 GitHub release workflow：

```sh
git tag v0.1.0
git push origin v0.1.0
```

当前 workflow 只构建 macOS app，并发布：

- `TalkaMac-macOS-arm64.zip`
- `TalkaMac-macOS-x86_64.zip`

workflow 不使用 Apple signing secrets。生成的 macOS bundle 是 ad-hoc signed，适合内部测试，但不等同于 notarized Developer ID release。

## 权限

Talka 需要 macOS Accessibility 权限才能向其它应用插入文本。如果 System Settings 已显示开启 Accessibility，但 Talka 仍提示 `Accessibility Required`，请先从以下位置删除旧的 TalkaMac 条目：

`System Settings > Privacy & Security > Accessibility`

然后重新添加或启用当前安装的 app。ad-hoc signed builds 可能在重新打包后改变 code-signing identity，macOS TCC 可能会把它们视作不同的授权主体。

## 仓库结构

```text
apps/
  macos/TalkaMac/      macOS SwiftUI app
  ios/TalkaIOS/        iOS SwiftUI app
cmd/
  talka-server/        Go control service
  talka-fake-asr/      测试用 fake ASR helper
  talka-sherpa-transcribe/
internal/
  app/                 control API 和 pipeline wiring
  asr/                 fake 与 sherpa-onnx ASR providers
  config/              runtime config loading 和 validation
  crypto/              pairing/session crypto helpers
  inject/              text insertion abstraction
  llm/                 Ollama/OpenAI-compatible cleanup provider
  mdns/                Bonjour/mDNS service metadata
  pairing/             pairing state 和 trusted devices
  protocol/            wire protocol types
  session/             encrypted session state
scripts/               setup、packaging、smoke 和 QA helpers
docs/                  产品和架构文档
```

## 参与贡献

欢迎提交 issues 和 pull requests。提交 PR 前建议运行：

```sh
./scripts/test-all.sh
git diff --check
```

请不要提交生成的模型、下载的 runtime binaries、本地 build products 或个人 signing material。

## 致谢

Talka 基于多个开源项目和平台技术构建：

- [sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx)，用于本地流式 ASR 集成和 macOS runtime artifacts。
- [ONNX Runtime](https://onnxruntime.ai/)，作为打包的 ONNX recognizer runtime 底层依赖。
- [Ollama](https://ollama.com/)，作为默认支持的本地 LLM cleanup endpoint。
- [FunASR](https://github.com/modelscope/FunASR)，用于早期 runtime scaffold 和模型转换相关说明。
- [ModelScope](https://modelscope.cn/) 与 [Hugging Face](https://huggingface.co/)，用于 ASR 模型来源和镜像相关脚本/说明。
- [Go](https://go.dev/) 以及本项目使用的 Go modules，包括 `golang.org/x/crypto`、`golang.org/x/sys` 和 `gopkg.in/yaml.v3`。
- Apple 的 SwiftUI、AVFoundation、Network/Bonjour、CryptoKit、CoreGraphics 和 Accessibility APIs，它们提供了原生 macOS/iOS 界面和系统集成能力。

在重新分发 bundled runtime 或模型 artifacts 前，请检查各 upstream project 的 license。

## 许可证

Talka 使用 [MIT License](LICENSE) 发布。第三方 runtime libraries、模型和工具保留各自 upstream licenses。
