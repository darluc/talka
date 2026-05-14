package main

import "testing"

func TestModelFilesForPrecisionSelectsInt8AndFP32(t *testing.T) {
	int8Files := modelFilesForPrecision("models/sherpa", "int8")
	if int8Files.encoder != "models/sherpa/encoder.int8.onnx" {
		t.Fatalf("int8 encoder = %q", int8Files.encoder)
	}
	if int8Files.decoder != "models/sherpa/decoder.int8.onnx" {
		t.Fatalf("int8 decoder = %q", int8Files.decoder)
	}

	fp32Files := modelFilesForPrecision("models/sherpa", "fp32")
	if fp32Files.encoder != "models/sherpa/encoder.onnx" {
		t.Fatalf("fp32 encoder = %q", fp32Files.encoder)
	}
	if fp32Files.decoder != "models/sherpa/decoder.onnx" {
		t.Fatalf("fp32 decoder = %q", fp32Files.decoder)
	}
}
