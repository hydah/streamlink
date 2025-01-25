package wav

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Reader WAV 文件读取器
type Reader struct {
	reader     io.ReadSeeker
	format     WAVFormat
	dataOffset int64  // data chunk 的起始位置
	dataSize   uint32 // data chunk 的大小
}

// NewReader 创建新的 WAV 读取器
func NewReader(reader io.ReadSeeker) (*Reader, error) {
	r := &Reader{
		reader: reader,
	}

	// 读取并验证 WAV 头
	if err := r.parseWAV(); err != nil {
		return nil, fmt.Errorf("failed to parse WAV file: %v", err)
	}

	return r, nil
}

// parseWAV 解析 WAV 文件
func (r *Reader) parseWAV() error {
	// 读取 RIFF 头
	var riffID [4]byte
	var riffSize uint32
	var waveID [4]byte

	if err := binary.Read(r.reader, binary.LittleEndian, &riffID); err != nil {
		return fmt.Errorf("failed to read RIFF ID: %v", err)
	}
	if err := binary.Read(r.reader, binary.LittleEndian, &riffSize); err != nil {
		return fmt.Errorf("failed to read RIFF size: %v", err)
	}
	if err := binary.Read(r.reader, binary.LittleEndian, &waveID); err != nil {
		return fmt.Errorf("failed to read WAVE ID: %v", err)
	}

	// 验证文件标识
	if string(riffID[:]) != "RIFF" {
		return fmt.Errorf("not a RIFF file")
	}
	if string(waveID[:]) != "WAVE" {
		return fmt.Errorf("not a WAVE file")
	}

	// 查找 fmt 块
	var chunkID [4]byte
	var chunkSize uint32
	var foundFmt, foundData bool

	for !foundFmt || !foundData {
		if err := binary.Read(r.reader, binary.LittleEndian, &chunkID); err != nil {
			return fmt.Errorf("failed to read chunk ID: %v", err)
		}
		if err := binary.Read(r.reader, binary.LittleEndian, &chunkSize); err != nil {
			return fmt.Errorf("failed to read chunk size: %v", err)
		}

		switch string(chunkID[:]) {
		case "fmt ":
			// 读取 fmt 块内容
			if err := binary.Read(r.reader, binary.LittleEndian, &r.format); err != nil {
				return fmt.Errorf("failed to read format chunk: %v", err)
			}
			foundFmt = true

			// 如果 chunk 大小大于 format 结构体大小，跳过剩余数据
			remaining := int64(chunkSize) - int64(binary.Size(r.format))
			if remaining > 0 {
				if _, err := r.reader.Seek(remaining, io.SeekCurrent); err != nil {
					return fmt.Errorf("failed to seek past extra format data: %v", err)
				}
			}

		case "data":
			// 记录数据块的位置和大小
			offset, err := r.reader.Seek(0, io.SeekCurrent)
			if err != nil {
				return fmt.Errorf("failed to get data offset: %v", err)
			}
			r.dataOffset = offset
			r.dataSize = chunkSize
			foundData = true

			// 跳过数据块
			if _, err := r.reader.Seek(int64(chunkSize), io.SeekCurrent); err != nil {
				return fmt.Errorf("failed to seek past data chunk: %v", err)
			}

		default:
			// 跳过其他块
			if _, err := r.reader.Seek(int64(chunkSize), io.SeekCurrent); err != nil {
				return fmt.Errorf("failed to seek past chunk: %v", err)
			}
		}
	}

	// 验证格式
	if err := r.format.Validate(); err != nil {
		return fmt.Errorf("invalid WAV format: %v", err)
	}

	// 定位到数据块开始位置
	_, err := r.reader.Seek(r.dataOffset, io.SeekStart)
	if err != nil {
		return fmt.Errorf("failed to seek to data start: %v", err)
	}

	return nil
}

// ReadSamples 读取指定数量的采样点
func (r *Reader) ReadSamples(samples []int16) (int, error) {
	// 计算要读取的字节数
	bytesToRead := len(samples) * int(r.format.BlockAlign/r.format.NumChannels)

	// 读取原始字节
	rawData := make([]byte, bytesToRead)
	n, err := r.reader.Read(rawData)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("failed to read samples: %v", err)
	}

	// 将字节转换为采样点
	samplesRead := n / int(r.format.BlockAlign/r.format.NumChannels)
	for i := 0; i < samplesRead; i++ {
		offset := i * 2 // 16位采样，每个采样点2字节
		samples[i] = int16(binary.LittleEndian.Uint16(rawData[offset : offset+2]))
	}

	if err == io.EOF {
		return samplesRead, io.EOF
	}
	return samplesRead, nil
}

// GetFormat 获取 WAV 格式信息
func (r *Reader) GetFormat() WAVFormat {
	return r.format
}

// GetDataSize 获取音频数据大小
func (r *Reader) GetDataSize() uint32 {
	return r.dataSize
}

// Seek 设置读取位置
func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		offset += r.dataOffset
	case io.SeekEnd:
		offset += r.dataOffset + int64(r.dataSize)
	}
	return r.reader.Seek(offset, whence)
}

// Close 关闭读取器
func (r *Reader) Close() error {
	if closer, ok := r.reader.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
