package wav

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

// getProjectRoot 获取项目根目录
func getProjectRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..", "..")
}

func TestWAVReadWrite(t *testing.T) {
	// 创建测试目录
	testDir := path.Join(getProjectRoot(), "testcase", "testdump")
	err := os.MkdirAll(testDir, 0755)
	assert.NoError(t, err)

	// 创建测试格式
	format := WAVFormat{
		AudioFormat:   1, // PCM
		NumChannels:   2, // 双声道
		SampleRate:    48000,
		BitsPerSample: 16,
		BlockAlign:    4,      // 2 channels * 2 bytes per sample
		ByteRate:      192000, // 48000 * 2 * 2
	}

	// 生成测试数据
	testData := make([]int16, 48000) // 1秒的音频数据
	for i := range testData {
		testData[i] = int16(i % 32768) // 生成锯齿波
	}

	t.Run("Write and Read WAV File", func(t *testing.T) {
		filename := path.Join(testDir, "test.wav")

		// 写入测试文件
		writer, err := NewFileWriter(filename, format)
		assert.NoError(t, err)

		err = writer.WriteSamples(testData)
		assert.NoError(t, err)

		err = writer.Close()
		assert.NoError(t, err)

		// 读取测试文件
		file, err := os.Open(filename)
		assert.NoError(t, err)
		defer file.Close()

		reader, err := NewReader(file)
		assert.NoError(t, err)

		// 验证格式
		readFormat := reader.GetFormat()
		assert.Equal(t, format, readFormat)

		// 读取数据
		readData := make([]int16, len(testData))
		n, err := reader.ReadSamples(readData)
		assert.NoError(t, err)
		assert.Equal(t, len(testData), n)

		// 验证数据
		assert.Equal(t, testData, readData[:n])
	})

	t.Run("Write and Read WAV Memory", func(t *testing.T) {
		// 使用内存缓冲区
		buf := &bytes.Buffer{}
		writer, err := NewWriter(newSeekBuffer(buf), format)
		assert.NoError(t, err)

		err = writer.WriteSamples(testData)
		assert.NoError(t, err)

		err = writer.Close()
		assert.NoError(t, err)

		// 从内存读取
		reader, err := NewReader(newSeekBuffer(bytes.NewBuffer(buf.Bytes())))
		assert.NoError(t, err)

		// 验证格式
		readFormat := reader.GetFormat()
		assert.Equal(t, format, readFormat)

		// 读取数据
		readData := make([]int16, len(testData))
		n, err := reader.ReadSamples(readData)
		assert.NoError(t, err)
		assert.Equal(t, len(testData), n)

		// 验证数据
		assert.Equal(t, testData, readData[:n])
	})
}

// seekBuffer 实现 io.ReadWriteSeeker 接口
type seekBuffer struct {
	*bytes.Buffer
	pos int64
}

func newSeekBuffer(buf *bytes.Buffer) *seekBuffer {
	return &seekBuffer{Buffer: buf}
}

func (b *seekBuffer) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case 0:
		abs = offset
	case 1:
		abs = b.pos + offset
	case 2:
		abs = int64(b.Len()) + offset
	}
	if abs < 0 {
		return 0, os.ErrInvalid
	}
	b.pos = abs
	return abs, nil
}

func (b *seekBuffer) Write(p []byte) (n int, err error) {
	n, err = b.Buffer.Write(p)
	if err == nil {
		b.pos += int64(n)
	}
	return
}

func (b *seekBuffer) Read(p []byte) (n int, err error) {
	n, err = b.Buffer.Read(p)
	if err == nil {
		b.pos += int64(n)
	}
	return
}
