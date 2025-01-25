package wav

import (
	"encoding/binary"
	"fmt"
	"io"
)

// WAVFormat WAV 文件格式信息
type WAVFormat struct {
	AudioFormat   uint16 // 音频格式（1 表示 PCM）
	NumChannels   uint16 // 声道数
	SampleRate    uint32 // 采样率
	ByteRate      uint32 // 字节率 = SampleRate * NumChannels * BitsPerSample/8
	BlockAlign    uint16 // 数据块对齐 = NumChannels * BitsPerSample/8
	BitsPerSample uint16 // 采样位数
}

// WAVHeader WAV 文件头
type WAVHeader struct {
	ChunkID       [4]byte // "RIFF"
	ChunkSize     uint32  // 文件总大小 - 8
	Format        [4]byte // "WAVE"
	Subchunk1ID   [4]byte // "fmt "
	Subchunk1Size uint32  // 格式块大小（16 字节）
	AudioFormat   uint16  // 音频格式（1 表示 PCM）
	NumChannels   uint16  // 声道数
	SampleRate    uint32  // 采样率
	ByteRate      uint32  // 字节率
	BlockAlign    uint16  // 数据块对齐
	BitsPerSample uint16  // 采样位数
	Subchunk2ID   [4]byte // "data"
	Subchunk2Size uint32  // 音频数据大小
}

// NewWAVHeader 创建新的 WAV 文件头
func NewWAVHeader(format WAVFormat, dataSize uint32) WAVHeader {
	return WAVHeader{
		ChunkID:       [4]byte{'R', 'I', 'F', 'F'},
		ChunkSize:     36 + dataSize, // 文件总大小 - 8
		Format:        [4]byte{'W', 'A', 'V', 'E'},
		Subchunk1ID:   [4]byte{'f', 'm', 't', ' '},
		Subchunk1Size: 16, // PCM 格式块大小固定为 16
		AudioFormat:   format.AudioFormat,
		NumChannels:   format.NumChannels,
		SampleRate:    format.SampleRate,
		ByteRate:      format.ByteRate,
		BlockAlign:    format.BlockAlign,
		BitsPerSample: format.BitsPerSample,
		Subchunk2ID:   [4]byte{'d', 'a', 't', 'a'},
		Subchunk2Size: dataSize,
	}
}

// Validate 验证 WAV 格式是否合法
func (f *WAVFormat) Validate() error {
	if f.AudioFormat != 1 {
		return fmt.Errorf("unsupported audio format: %d (expected 1 for PCM)", f.AudioFormat)
	}
	if f.BitsPerSample != 16 {
		return fmt.Errorf("unsupported bits per sample: %d (expected 16)", f.BitsPerSample)
	}
	if f.ByteRate != f.SampleRate*uint32(f.NumChannels)*uint32(f.BitsPerSample)/8 {
		return fmt.Errorf("invalid byte rate")
	}
	if f.BlockAlign != f.NumChannels*f.BitsPerSample/8 {
		return fmt.Errorf("invalid block align")
	}
	return nil
}

// Write 将 WAV 头写入到 writer
func (h *WAVHeader) Write(w io.Writer) error {
	return binary.Write(w, binary.LittleEndian, h)
}

// Read 从 reader 读取 WAV 头
func (h *WAVHeader) Read(r io.Reader) error {
	return binary.Read(r, binary.LittleEndian, h)
}

// GetFormat 从头部信息获取 WAV 格式
func (h *WAVHeader) GetFormat() WAVFormat {
	return WAVFormat{
		AudioFormat:   h.AudioFormat,
		NumChannels:   h.NumChannels,
		SampleRate:    h.SampleRate,
		ByteRate:      h.ByteRate,
		BlockAlign:    h.BlockAlign,
		BitsPerSample: h.BitsPerSample,
	}
}
