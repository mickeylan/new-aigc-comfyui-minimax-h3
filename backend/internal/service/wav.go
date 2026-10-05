package service

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"

	"comfyui-console/internal/indextts"
)

func encodePCM16WAV(audio indextts.Audio) ([]byte, error) {
	if audio.SampleRate == 0 || audio.Channels == 0 || len(audio.Samples) == 0 {
		return nil, fmt.Errorf("无效的 IndexTTS 音频格式")
	}
	if len(audio.Samples)%int(audio.Channels) != 0 {
		return nil, fmt.Errorf("IndexTTS 音频样本数与声道数不匹配")
	}
	if uint64(len(audio.Samples)) > uint64(math.MaxUint32-36)/2 {
		return nil, fmt.Errorf("IndexTTS 音频过大")
	}
	dataSize := uint32(len(audio.Samples) * 2)
	var output bytes.Buffer
	output.Grow(44 + int(dataSize))
	output.WriteString("RIFF")
	_ = binary.Write(&output, binary.LittleEndian, uint32(36)+dataSize)
	output.WriteString("WAVEfmt ")
	_ = binary.Write(&output, binary.LittleEndian, uint32(16))
	_ = binary.Write(&output, binary.LittleEndian, uint16(1))
	_ = binary.Write(&output, binary.LittleEndian, uint16(audio.Channels))
	_ = binary.Write(&output, binary.LittleEndian, audio.SampleRate)
	_ = binary.Write(&output, binary.LittleEndian, audio.SampleRate*audio.Channels*2)
	_ = binary.Write(&output, binary.LittleEndian, uint16(audio.Channels*2))
	_ = binary.Write(&output, binary.LittleEndian, uint16(16))
	output.WriteString("data")
	_ = binary.Write(&output, binary.LittleEndian, dataSize)
	for _, sample := range audio.Samples {
		if math.IsNaN(float64(sample)) || math.IsInf(float64(sample), 0) {
			return nil, fmt.Errorf("IndexTTS 音频包含非有限样本")
		}
		sample = max(-1, min(1, sample))
		value := int16(math.Round(float64(sample) * 32767))
		if sample <= -1 {
			value = math.MinInt16
		}
		_ = binary.Write(&output, binary.LittleEndian, value)
	}
	return output.Bytes(), nil
}
