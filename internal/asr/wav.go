package asr

import (
	"encoding/binary"
	"fmt"
	"os"
)

func LoadWAVFrames(path string, frameSize int) ([][]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pcm, err := decodePCM16LEWAV(data)
	if err != nil {
		return nil, err
	}
	return splitPCMFrames(pcm, frameSize), nil
}

func decodePCM16LEWAV(data []byte) ([]byte, error) {
	if len(data) < 44 {
		return nil, fmt.Errorf("wav file is too small")
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, fmt.Errorf("wav file must use RIFF/WAVE")
	}

	var (
		formatFound   bool
		dataFound     bool
		channels      uint16
		sampleRate    uint32
		bitsPerSample uint16
		pcm           []byte
	)

	for offset := 12; offset+8 <= len(data); {
		chunkID := string(data[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		offset += 8
		if offset+chunkSize > len(data) {
			return nil, fmt.Errorf("wav chunk %s exceeds file size", chunkID)
		}
		chunk := data[offset : offset+chunkSize]
		switch chunkID {
		case "fmt ":
			if len(chunk) < 16 {
				return nil, fmt.Errorf("wav fmt chunk is too small")
			}
			audioFormat := binary.LittleEndian.Uint16(chunk[0:2])
			channels = binary.LittleEndian.Uint16(chunk[2:4])
			sampleRate = binary.LittleEndian.Uint32(chunk[4:8])
			bitsPerSample = binary.LittleEndian.Uint16(chunk[14:16])
			if audioFormat != 1 {
				return nil, fmt.Errorf("wav audio format %d is not PCM", audioFormat)
			}
			formatFound = true
		case "data":
			pcm = append([]byte(nil), chunk...)
			dataFound = true
		}
		offset += chunkSize
		if chunkSize%2 == 1 {
			offset++
		}
	}

	if !formatFound || !dataFound {
		return nil, fmt.Errorf("wav file must include fmt and data chunks")
	}
	if channels != 1 || sampleRate != 16000 || bitsPerSample != 16 {
		return nil, fmt.Errorf("wav file must be 16kHz mono 16-bit PCM")
	}
	return pcm, nil
}
