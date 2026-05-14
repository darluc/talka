//go:build !sherpa_onnx

package asr

import "fmt"

func newSherpaONNXImpl(config SherpaONNXConfig) (sherpaONNXImpl, error) {
	return nil, fmt.Errorf("sherpa-onnx support is not compiled into this talka-server binary")
}
