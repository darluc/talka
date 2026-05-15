//go:build darwin && cgo && sherpa_onnx

package asr

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/sherpa-onnx/include
#cgo LDFLAGS: -L${SRCDIR}/../../third_party/sherpa-onnx/lib -lsherpa-onnx-c-api
#include <stdlib.h>
#include <string.h>
#include "sherpa-onnx/c-api/c-api.h"
*/
import "C"

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"talka/internal/protocol"
)

type sherpaONNXNativeProvider struct {
	mu         sync.Mutex
	recognizer *C.SherpaOnnxOnlineRecognizer
	closed     bool
}

type sherpaONNXNativeStream struct {
	provider *sherpaONNXNativeProvider
	stream   *C.SherpaOnnxOnlineStream
	metadata protocol.AudioMetadata
	mu       sync.Mutex
	closed   bool
}

func newSherpaONNXImpl(config SherpaONNXConfig) (sherpaONNXImpl, error) {
	modelType := strings.TrimSpace(config.ModelType)
	cTokens := C.CString(config.TokensPath)
	cEncoder := C.CString(config.EncoderPath)
	cDecoder := C.CString(config.DecoderPath)
	var cJoiner *C.char
	if strings.TrimSpace(config.JoinerPath) != "" {
		cJoiner = C.CString(config.JoinerPath)
	}
	cProvider := C.CString(config.Provider)
	cDecoding := C.CString(config.DecodingMethod)
	defer C.free(unsafe.Pointer(cTokens))
	defer C.free(unsafe.Pointer(cEncoder))
	defer C.free(unsafe.Pointer(cDecoder))
	if cJoiner != nil {
		defer C.free(unsafe.Pointer(cJoiner))
	}
	defer C.free(unsafe.Pointer(cProvider))
	defer C.free(unsafe.Pointer(cDecoding))

	var cfg C.SherpaOnnxOnlineRecognizerConfig
	C.memset(unsafe.Pointer(&cfg), 0, C.size_t(unsafe.Sizeof(cfg)))
	cfg.feat_config.sample_rate = 16000
	cfg.feat_config.feature_dim = C.int32_t(config.FeatureDim)
	switch modelType {
	case "transducer":
		cfg.model_config.transducer.encoder = cEncoder
		cfg.model_config.transducer.decoder = cDecoder
		cfg.model_config.transducer.joiner = cJoiner
	case "paraformer":
		cfg.model_config.paraformer.encoder = cEncoder
		cfg.model_config.paraformer.decoder = cDecoder
	default:
		return nil, fmt.Errorf("unsupported sherpa-onnx model_type %q", config.ModelType)
	}
	cfg.model_config.tokens = cTokens
	cfg.model_config.num_threads = C.int32_t(config.NumThreads)
	cfg.model_config.provider = cProvider
	cfg.decoding_method = cDecoding

	recognizer := C.SherpaOnnxCreateOnlineRecognizer(&cfg)
	if recognizer == nil {
		return nil, fmt.Errorf("create sherpa-onnx online recognizer failed")
	}
	return &sherpaONNXNativeProvider{recognizer: recognizer}, nil
}

func (p *sherpaONNXNativeProvider) NewStream(ctx context.Context, metadata protocol.AudioMetadata) (StreamingSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := protocol.ValidateAudioMetadata(metadata); err != nil {
		return nil, err
	}
	if metadata.Encoding != protocol.EncodingPCMS16LE || metadata.Channels != 1 {
		return nil, fmt.Errorf("sherpa-onnx requires mono pcm_s16le audio")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.recognizer == nil {
		return nil, fmt.Errorf("sherpa-onnx recognizer is closed")
	}
	stream := C.SherpaOnnxCreateOnlineStream(p.recognizer)
	if stream == nil {
		return nil, fmt.Errorf("create sherpa-onnx online stream failed")
	}
	return &sherpaONNXNativeStream{provider: p, stream: stream, metadata: metadata}, nil
}

func (p *sherpaONNXNativeProvider) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	if p.recognizer != nil {
		C.SherpaOnnxDestroyOnlineRecognizer(p.recognizer)
		p.recognizer = nil
	}
	return nil
}

func (s *sherpaONNXNativeStream) AcceptFrame(ctx context.Context, frame []byte) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.stream == nil {
		return Result{}, fmt.Errorf("sherpa-onnx stream is closed")
	}
	samples, err := pcmS16LEToFloat32(frame)
	if err != nil {
		return Result{}, err
	}
	if len(samples) > 0 {
		C.SherpaOnnxOnlineStreamAcceptWaveform(s.stream, C.int32_t(s.metadata.SampleRate), (*C.float)(unsafe.Pointer(&samples[0])), C.int32_t(len(samples)))
	}
	s.decodeReadyLocked()
	return s.resultLocked()
}

func (s *sherpaONNXNativeStream) Finish(ctx context.Context) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.stream == nil {
		return Result{}, fmt.Errorf("sherpa-onnx stream is closed")
	}
	s.acceptSilenceLocked(sherpaONNXFinalSilenceSampleCount(s.metadata.SampleRate))
	C.SherpaOnnxOnlineStreamInputFinished(s.stream)
	s.decodeReadyLocked()
	return s.resultLocked()
}

func (s *sherpaONNXNativeStream) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.stream != nil {
		C.SherpaOnnxDestroyOnlineStream(s.stream)
		s.stream = nil
	}
	return nil
}

func (s *sherpaONNXNativeStream) decodeReadyLocked() {
	s.provider.mu.Lock()
	recognizer := s.provider.recognizer
	s.provider.mu.Unlock()
	if recognizer == nil {
		return
	}
	for C.SherpaOnnxIsOnlineStreamReady(recognizer, s.stream) != 0 {
		C.SherpaOnnxDecodeOnlineStream(recognizer, s.stream)
		runtime.Gosched()
	}
}

func (s *sherpaONNXNativeStream) acceptSilenceLocked(sampleCount int) {
	if sampleCount <= 0 {
		return
	}
	silence := make([]float32, sampleCount)
	C.SherpaOnnxOnlineStreamAcceptWaveform(s.stream, C.int32_t(s.metadata.SampleRate), (*C.float)(unsafe.Pointer(&silence[0])), C.int32_t(len(silence)))
}

func (s *sherpaONNXNativeStream) resultLocked() (Result, error) {
	s.provider.mu.Lock()
	recognizer := s.provider.recognizer
	s.provider.mu.Unlock()
	if recognizer == nil {
		return Result{}, fmt.Errorf("sherpa-onnx recognizer is closed")
	}
	result := C.SherpaOnnxGetOnlineStreamResult(recognizer, s.stream)
	if result == nil {
		return Result{}, fmt.Errorf("get sherpa-onnx result failed")
	}
	defer C.SherpaOnnxDestroyOnlineRecognizerResult(result)

	text := strings.TrimSpace(C.GoString(result.text))
	return Result{Transcript: text}, nil
}

func pcmS16LEToFloat32(frame []byte) ([]float32, error) {
	if len(frame)%2 != 0 {
		return nil, fmt.Errorf("pcm_s16le frame has odd byte length %d", len(frame))
	}
	samples := make([]float32, len(frame)/2)
	for i := range samples {
		v := int16(binary.LittleEndian.Uint16(frame[i*2:]))
		samples[i] = float32(math.Max(-1, math.Min(1, float64(v)/32768.0)))
	}
	return samples, nil
}
