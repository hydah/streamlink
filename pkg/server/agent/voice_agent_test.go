package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"voiceagent/internal/config"
	"voiceagent/pkg/logic/codec"
	"voiceagent/pkg/logic/dumper"
	"voiceagent/pkg/logic/flux"
	"voiceagent/pkg/logic/pipeline"
	"voiceagent/pkg/logic/resampler"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
)

func init() {
	if err := godotenv.Load("../../../.env.test"); err != nil {
		panic("Error loading .env.test file")
	}
}

// getProjectRoot 获取项目根目录
func getProjectRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	// pkg/logic/flux/file_audio_src_test.go -> 项目根目录
	return filepath.Join(filepath.Dir(filename), "..", "..", "..")
}

type fileAudioProcessor struct {
	inputSampleRate  uint32
	outputSampleRate uint32
	inputChannels    uint16
	outputChannels   uint16
}

// ProcessInput 处理输入音频：重采样 16kHz,单声道
func (p *fileAudioProcessor) ProcessInput(source pipeline.Component) flux.ProcessingChain {
	// 创建重采样器 (48kHz,双声道 -> 16kHz,单声道)
	downsampler, err := resampler.NewResampler(
		int(p.inputSampleRate),
		16000,
		int(p.inputChannels),
		1,
	)
	if err != nil {
		return flux.ProcessingChain{
			First: nil,
			Last:  nil,
			All:   []pipeline.Component{},
		}
	}

	// 连接处理链

	return flux.ProcessingChain{
		First: downsampler,
		Last:  downsampler,
		All:   []pipeline.Component{downsampler},
	}
}

// ProcessOutput 处理输出音频：重采样 -> Opus编码
func (p *fileAudioProcessor) ProcessOutput(sink pipeline.Component) flux.ProcessingChain {
	// 创建重采样器 (16kHz,单声道 -> 48kHz,双声道)
	upsampler, err := resampler.NewResampler(
		16000,
		int(p.outputSampleRate),
		1,
		int(p.outputChannels),
	)
	if err != nil {
		return flux.ProcessingChain{
			First: sink,
			Last:  sink,
			All:   []pipeline.Component{sink},
		}
	}

	// 创建 Opus 编码器
	encoder, err := codec.NewOpusEncoder(int(p.outputSampleRate), int(p.outputChannels))
	if err != nil {
		return flux.ProcessingChain{
			First: sink,
			Last:  sink,
			All:   []pipeline.Component{sink},
		}
	}

	// 返回处理链
	return flux.ProcessingChain{
		First: upsampler,
		Last:  sink,
		All:   []pipeline.Component{upsampler, encoder, sink},
	}
}

func TestVoiceAgent_WithFileAudio(t *testing.T) {
	projectRoot := getProjectRoot()
	outputPath := filepath.Join(projectRoot, "testcase", "testdump", "output.ogg")
	// 创建配置
	cfg := &config.Config{}
	cfg.ASR.TencentASR.AppID = "$TENCENTASR_APP_ID"
	cfg.ASR.TencentASR.SecretID = "$TENCENTASR_SECRET_ID"
	cfg.ASR.TencentASR.SecretKey = "$TENCENTASR_SECRET_KEY"
	cfg.ASR.TencentASR.EngineModelType = "16k_zh"
	cfg.ASR.TencentASR.SliceSize = 6400

	cfg.LLM.OpenAI.APIKey = "$SILICON_API_KEY"
	cfg.LLM.OpenAI.BaseURL = "https://api.siliconflow.cn/v1"

	cfg.TTS.TencentTTS.AppID = "$TENCENTTTS_APP_ID"
	cfg.TTS.TencentTTS.SecretID = "$TENCENTTTS_SECRET_ID"
	cfg.TTS.TencentTTS.SecretKey = "$TENCENTTTS_SECRET_KEY"
	cfg.TTS.TencentTTS.VoiceType = 502001
	cfg.TTS.TencentTTS.Codec = "pcm"

	// 创建文件音频处理器
	processor := &fileAudioProcessor{
		inputSampleRate:  48000,
		outputSampleRate: 48000,
		inputChannels:    2,
		outputChannels:   2,
	}
	// 创建文件音频源 (48kHz 采样率)
	source := flux.NewFileAudioSource(filepath.Join(projectRoot, "testcase", "testdata", "libai.wav"), 48000)

	// 创建文件音频接收器 (48kHz 采样率)
	sink, _ := dumper.NewOggDumper(48000, 2, outputPath)
	sink.SetOutput(nil)

	// 创建 VoiceAgent
	agent := NewVoiceAgent(cfg, source, sink, processor)
	assert.NotNil(t, agent)

	// 启动 VoiceAgent
	err := agent.Start()
	assert.NoError(t, err)

	source.Start()

	// 等待处理完成
	time.Sleep(28 * time.Second)

	// 停止 VoiceAgent
	agent.Stop()

	// 验证输出文件是否存在且不为空
	stat, err := os.Stat(outputPath)
	assert.NoError(t, err)
	assert.True(t, stat.Size() > 0)
}

func TestVoiceAgent_ErrorHandling(t *testing.T) {
	projectRoot := getProjectRoot()
	outputPath := filepath.Join(projectRoot, "testcase", "testdump", "voice_test.ogg")

	// 使用无效的配置测试错误处理
	cfg := &config.Config{}
	cfg.ASR.TencentASR.AppID = "invalid"
	cfg.ASR.TencentASR.SecretID = "invalid"
	cfg.ASR.TencentASR.SecretKey = "invalid"

	source := flux.NewFileAudioSource("nonexistent.ogg", 48000)
	sink, _ := dumper.NewOggDumper(48000, 2, outputPath)
	processor := &fileAudioProcessor{
		inputSampleRate:  48000,
		outputSampleRate: 48000,
		inputChannels:    2,
		outputChannels:   2,
	}

	agent := NewVoiceAgent(cfg, source, sink, processor)
	err := agent.Start()
	assert.Error(t, err)
}

func TestVoiceAgent_ComponentHealth(t *testing.T) {
	projectRoot := getProjectRoot()
	outputPath := filepath.Join(projectRoot, "testcase", "testdump", "voice_test.ogg")
	cfg := &config.Config{}
	cfg.ASR.TencentASR.AppID = "$TENCENTASR_APP_ID"
	cfg.ASR.TencentASR.SecretID = "$TENCENTASR_SECRET_ID"
	cfg.ASR.TencentASR.SecretKey = "$TENCENTASR_SECRET_KEY"
	cfg.ASR.TencentASR.EngineModelType = "16k_zh"
	cfg.ASR.TencentASR.SliceSize = 6400

	source := flux.NewFileAudioSource("testdata/test_input.ogg", 48000)
	sink, _ := dumper.NewOggDumper(48000, 2, outputPath)
	processor := &fileAudioProcessor{
		inputSampleRate:  48000,
		outputSampleRate: 48000,
		inputChannels:    2,
		outputChannels:   2,
	}

	agent := NewVoiceAgent(cfg, source, sink, processor)
	assert.NotNil(t, agent)

	// 启动并立即停止，检查组件状态
	err := agent.Start()
	assert.NoError(t, err)
	agent.Stop()
}
