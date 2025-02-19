package flux

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"streamlink/pkg/logic/dumper"
	"streamlink/pkg/logic/resampler"
	"testing"
	"time"

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
	inputFile := path.Join(projectRoot, "testcase", "testdata", "libai.wav")
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
	// resampler
	resampler, err := resampler.NewResampler(48000, 16000, 2, 1)
	assert.NoError(t, err)

	// 创建 PCM 转储器
	dumpFile := path.Join(outputDir, "file_source_test.pcm")
	pcmDumper, err := dumper.NewPCMDumper(dumpFile)
	assert.NoError(t, err)

	// 设置处理链
	source.Connect(resampler).Connect(pcmDumper)
	pcmDumper.SetOutput(nil)

	// 启动所有组件
	resampler.Start()
	pcmDumper.Start()
	source.Start()

	// 等待处理完成
	time.Sleep(5 * time.Second)

	// 停止所有组件
	source.Stop()
	resampler.Stop()
	pcmDumper.Stop()

	// 验证输出文件是否存在且不为空
	stat, err := os.Stat(dumpFile)
	assert.NoError(t, err)
	assert.True(t, stat.Size() > 0)
}
