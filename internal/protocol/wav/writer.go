package wav

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// Writer WAV 文件写入器
type Writer struct {
	writer     io.WriteSeeker
	header     WAVHeader
	format     WAVFormat
	dataSize   uint32
	dataOffset int64
}

// NewWriter 创建新的 WAV 写入器
func NewWriter(writer io.WriteSeeker, format WAVFormat) (*Writer, error) {
	// 验证格式
	if err := format.Validate(); err != nil {
		return nil, fmt.Errorf("invalid WAV format: %v", err)
	}

	w := &Writer{
		writer: writer,
		format: format,
		header: NewWAVHeader(format, 0), // 初始数据大小为0
	}

	// 写入头部
	if err := w.writeHeader(); err != nil {
		return nil, fmt.Errorf("failed to write WAV header: %v", err)
	}

	// 记录数据段的起始位置
	offset, err := writer.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, fmt.Errorf("failed to get data offset: %v", err)
	}
	w.dataOffset = offset

	return w, nil
}

// NewFileWriter 创建新的 WAV 文件写入器
func NewFileWriter(filename string, format WAVFormat) (*Writer, error) {
	file, err := os.Create(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %v", err)
	}

	writer, err := NewWriter(file, format)
	if err != nil {
		file.Close()
		return nil, err
	}

	return writer, nil
}

// writeHeader 写入 WAV 文件头
func (w *Writer) writeHeader() error {
	return w.header.Write(w.writer)
}

// WriteSamples 写入采样点数据
func (w *Writer) WriteSamples(samples []int16) error {
	// 计算要写入的字节数
	bytesToWrite := len(samples) * int(w.format.BlockAlign/w.format.NumChannels)
	rawData := make([]byte, bytesToWrite)

	// 将采样点转换为字节
	for i := 0; i < len(samples); i++ {
		offset := i * 2 // 16位采样，每个采样点2字节
		binary.LittleEndian.PutUint16(rawData[offset:offset+2], uint16(samples[i]))
	}

	// 写入数据
	n, err := w.writer.Write(rawData)
	if err != nil {
		return fmt.Errorf("failed to write samples: %v", err)
	}

	// 更新数据大小
	w.dataSize += uint32(n)
	return nil
}

// Close 更新文件头并关闭写入器
func (w *Writer) Close() error {
	// 更新文件头中的数据大小
	w.header.Subchunk2Size = w.dataSize
	w.header.ChunkSize = 36 + w.dataSize

	// 回到文件开头
	_, err := w.writer.Seek(0, io.SeekStart)
	if err != nil {
		return fmt.Errorf("failed to seek to start: %v", err)
	}

	// 重写文件头
	if err := w.writeHeader(); err != nil {
		return fmt.Errorf("failed to update header: %v", err)
	}

	// 关闭写入器
	if closer, ok := w.writer.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// GetDataSize 获取已写入的数据大小
func (w *Writer) GetDataSize() uint32 {
	return w.dataSize
}

// GetFormat 获取 WAV 格式信息
func (w *Writer) GetFormat() WAVFormat {
	return w.format
}

// GetHeader 获取 WAV 头信息
func (w *Writer) GetHeader() WAVHeader {
	return w.header
}
