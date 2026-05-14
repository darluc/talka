//go:build sherpa_onnx && (!darwin || !cgo)

package asr

import "fmt"

func newSherpaONNXImpl(config SherpaONNXConfig) (sherpaONNXImpl, error) {
	return nil, fmt.Errorf("sherpa-onnx cgo support is currently only wired for macOS")
}
