package flux

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"voiceagent/pkg/logic/codec"
	"voiceagent/pkg/logic/dumper"

	"github.com/stretchr/testify/assert"
)

// getProjectRoot 获取项目根目录
func getProjectRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	// pkg/logic/flux/file_audio_src_test.go -> 项目根目录
	return filepath.Join(filepath.Dir(filename), "..", "..", "..")
}

func TestFileAudioSource_WithOpusDecoding(t *testing.T) {
	projectRoot := getProjectRoot()

	// 使用项目相对路径访问测试音频文件
	inputFile := path.Join(projectRoot, "testcase", "testdata", "libai.ogg")
	if _, err := os.Stat(inputFile); os.IsNotExist(err) {
		t.Skipf("Test input file not found: %s", inputFile)
	}

	// 创建输出目录
	outputDir := path.Join(projectRoot, "testcase", "testdump")
	fmt.Println(outputDir)
	err := os.MkdirAll(outputDir, 0755)
	assert.NoError(t, err)

	// 创建 FileAudioSource (48kHz 采样率)
	source := NewFileAudioSource(inputFile, 48000)
	assert.NotNil(t, source)

	// 创建 Opus 解码器 (48kHz, 双声道)
	opusDecoder, err := codec.NewOpusDecoder(48000, 2)
	assert.NoError(t, err)

	// 创建 PCM 转储器
	dumpFile := path.Join(outputDir, "file_source_test.pcm")
	pcmDumper, err := dumper.NewPCMDumper(dumpFile)
	assert.NoError(t, err)

	// 设置处理链
	source.Connect(opusDecoder).Connect(pcmDumper)
	pcmDumper.SetOutput(nil)

	// 启动所有组件
	opusDecoder.Start()
	pcmDumper.Start()
	source.Start()

	// 等待处理完成
	time.Sleep(5 * time.Second)

	// 停止所有组件
	source.Stop()
	opusDecoder.Stop()
	pcmDumper.Stop()

	// 验证输出文件是否存在且不为空
	stat, err := os.Stat(dumpFile)
	assert.NoError(t, err)
	assert.True(t, stat.Size() > 0)
}
