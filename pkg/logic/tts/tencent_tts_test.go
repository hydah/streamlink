package tts

import (
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
	"voiceagent/pkg/logic/codec"
	"voiceagent/pkg/logic/dumper"
	"voiceagent/pkg/logic/pipeline"
	"voiceagent/pkg/logic/resampler"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	ttslib "github.com/tencentcloud/tencentcloud-speech-sdk-go/tts"
)

// getProjectRoot 获取项目根目录
func getProjectRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	// pkg/logic/flux/file_audio_src_test.go -> 项目根目录
	return filepath.Join(filepath.Dir(filename), "..", "..", "..")
}

func init() {
	if err := godotenv.Load("../../../.env.test"); err != nil {
		log.Printf("Error loading .env.test file: %v", err)
	}
}

func TestNewTencentTTS(t *testing.T) {
	// 跳过测试如果环境变量未设置
	appIDStr := os.Getenv("TENCENTTTS_APP_ID")
	secretID := os.Getenv("TENCENTTTS_SECRET_ID")
	secretKey := os.Getenv("TENCENTTTS_SECRET_KEY")
	if appIDStr == "" || secretID == "" || secretKey == "" {
		t.Skip("Tencent credentials not set in environment variables")
	}

	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		t.Fatalf("Failed to parse APP_ID: %v", err)
	}

	tts := NewTencentTTS(appID, secretID, secretKey, 1001, "pcm")
	assert.NotNil(t, tts)
	assert.Equal(t, appID, tts.appID)
	assert.Equal(t, secretID, tts.secretID)
	assert.Equal(t, secretKey, tts.secretKey)
	assert.Equal(t, int64(1001), tts.voiceType)
	assert.Equal(t, "pcm", tts.codec)
}

func TestTencentTTS_Process(t *testing.T) {
	// 跳过测试如果环境变量未设置
	appIDStr := os.Getenv("TENCENTTTS_APP_ID")
	secretID := os.Getenv("TENCENTTTS_SECRET_ID")
	secretKey := os.Getenv("TENCENTTTS_SECRET_KEY")
	if appIDStr == "" || secretID == "" || secretKey == "" {
		t.Skip("Tencent credentials not set in environment variables")
	}

	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		t.Fatalf("Failed to parse APP_ID: %v", err)
	}
	tts := NewTencentTTS(appID, secretID, secretKey, 1001, "mp3")

	// 测试处理字符串数据
	resultReceived := false
	tts.SetOutput(func(packet pipeline.Packet) {
		resultReceived = true
		assert.IsType(t, []byte{}, packet.Data)
		assert.NotEmpty(t, packet.Data)
	})

	// 测试处理字符串数据
	tts.Process(pipeline.Packet{
		Data: "你好，世界",
		Seq:  0,
		Src:  nil,
	})

	// 等待一段时间以确保处理完成
	time.Sleep(1 * time.Second)
	assert.True(t, resultReceived, "Should receive result for valid text")

	// 验证是否生成了音频数据
	audioData := tts.GetAudioData()
	assert.NotNil(t, audioData)
	assert.True(t, len(audioData) > 0)
	fileName := "test1.pcm"
	ttslib.WriteFile(path.Join(getProjectRoot(), "testcase", "testdump", fileName), audioData)

	// 测试处理不支持的数据类型
	tts.Process(pipeline.Packet{
		Data: []byte{1, 2, 3, 4},
		Seq:  1,
		Src:  nil,
	})
}

func TestTencentTTS_SetVoiceAndCodec(t *testing.T) {
	// 跳过测试如果环境变量未设置
	appIDStr := os.Getenv("TENCENTTTS_APP_ID")
	secretID := os.Getenv("TENCENTTTS_SECRET_ID")
	secretKey := os.Getenv("TENCENTTTS_SECRET_KEY")
	if appIDStr == "" || secretID == "" {
		t.Skip("Tencent credentials not set in environment variables")
	}

	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		t.Fatalf("Failed to parse APP_ID: %v", err)
	}
	tts := NewTencentTTS(appID, secretID, secretKey, 1001, "mp3")
	tts.Start()

	// 测试设置音色
	newVoiceType := int64(1002)
	tts.SetVoiceType(newVoiceType)
	assert.Equal(t, newVoiceType, tts.voiceType)

	// 测试 PCM 格式
	tts.SetCodec("pcm")
	tts.Process(pipeline.Packet{
		Data: "测试PCM格式语音",
		Seq:  0,
		Src:  nil,
	})

	audioData := tts.GetAudioData()
	assert.NotNil(t, audioData)
	assert.True(t, len(audioData) > 0)

	fileName := fmt.Sprintf("test_voice_%d.pcm", newVoiceType)
	ttslib.WriteFile(path.Join(getProjectRoot(), "testcase", "testdump", fileName), audioData)
	t.Logf("PCM audio file saved as: %s", fileName)

	// 测试 MP3 格式
	// 测试音色502001，智小柔 对话女声
	tts.SetVoiceType(502001)
	tts.SetCodec("mp3")
	tts.Process(pipeline.Packet{
		Data: "哥哥，502001号技师为您服务",
		Seq:  1,
		Src:  nil,
	})

	audioData = tts.GetAudioData()
	assert.NotNil(t, audioData)
	assert.True(t, len(audioData) > 0)

	fileName = fmt.Sprintf("test_voice_%d.mp3", 502001)
	ttslib.WriteFile(path.Join(getProjectRoot(), "testcase", "testdump", fileName), audioData)
	t.Logf("MP3 audio file saved as: %s", fileName)

	// 测试音色501006，千嶂 对换男声
	tts.SetVoiceType(501006)
	tts.SetCodec("mp3")
	tts.Process(pipeline.Packet{
		Data: "姐姐，501006号技师为您服务",
		Seq:  2,
		Src:  nil,
	})

	audioData = tts.GetAudioData()
	assert.NotNil(t, audioData)
	assert.True(t, len(audioData) > 0)

	fileName = fmt.Sprintf("test_voice_%d.mp3", 501006)
	ttslib.WriteFile(path.Join(getProjectRoot(), "testcase", "testdump", fileName), audioData)
	t.Logf("MP3 audio file saved as: %s", fileName)
}

func TestTencentTTS_WithOpusEncoding(t *testing.T) {
	// 跳过测试如果环境变量未设置
	appIDStr := os.Getenv("TENCENTTTS_APP_ID")
	secretID := os.Getenv("TENCENTTTS_SECRET_ID")
	secretKey := os.Getenv("TENCENTTTS_SECRET_KEY")
	if appIDStr == "" || secretID == "" || secretKey == "" {
		t.Skip("Tencent credentials not set in environment variables")
	}

	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		t.Fatalf("Failed to parse APP_ID: %v", err)
	}

	// 创建 TTS 实例，使用 PCM 编码
	tts := NewTencentTTS(appID, secretID, secretKey, 502001, "pcm")
	assert.NotNil(t, tts)

	// 创建上采样器 (16kHz -> 48kHz, 单声道)
	upsampler, err := resampler.NewResampler(16000, 48000, 1, 2)
	assert.NoError(t, err)

	// 创建 Opus 编码器 (48kHz, 单声道)
	opusEncoder, err := codec.NewOpusEncoder(48000, 2)
	assert.NoError(t, err)

	// 创建 OGG 转储器 (48kHz, 单声道)
	oggDumper, err := dumper.NewOggDumper(48000, 2, path.Join(getProjectRoot(), "testcase", "testdump", "tencent_tts_test.ogg"))
	oggDumper.SetOutput(nil)
	assert.NoError(t, err)

	// 设置处理链
	tts.Connect(upsampler).Connect(opusEncoder).Connect(oggDumper)
	// 启动所有组件
	upsampler.Start()
	opusEncoder.Start()
	oggDumper.Start()
	tts.Start()

	// 生成测试音频
	tts.Process(pipeline.Packet{
		Data: "  背一首李白的诗",
		Seq:  0,
		Src:  nil,
	})

	// 等待处理完成
	time.Sleep(5 * time.Second)

	// 停止所有组件
	oggDumper.Stop()
	opusEncoder.Stop()
	upsampler.Stop()
	tts.Stop()
}

func TestTencentTTS_WithWav(t *testing.T) {
	// 跳过测试如果环境变量未设置
	appIDStr := os.Getenv("TENCENTTTS_APP_ID")
	secretID := os.Getenv("TENCENTTTS_SECRET_ID")
	secretKey := os.Getenv("TENCENTTTS_SECRET_KEY")
	if appIDStr == "" || secretID == "" || secretKey == "" {
		t.Skip("Tencent credentials not set in environment variables")
	}

	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		t.Fatalf("Failed to parse APP_ID: %v", err)
	}

	// 创建 TTS 实例，使用 PCM 编码
	tts := NewTencentTTS(appID, secretID, secretKey, 502001, "pcm")
	assert.NotNil(t, tts)

	// 创建上采样器 (16kHz -> 48kHz, 双声道)
	upsampler, err := resampler.NewResampler(16000, 48000, 1, 2)
	assert.NoError(t, err)

	// 创建 WAV 转储器 (48kHz, 双声道)
	wavDumper, err := dumper.NewWAVDumper(path.Join(getProjectRoot(), "testcase", "testdump", "tencent_tts_test.wav"), 48000, 2)
	assert.NoError(t, err)

	// 设置处理链
	tts.Connect(upsampler).Connect(wavDumper)
	// 启动所有组件
	upsampler.Start()
	wavDumper.Start()
	tts.Start()

	// 生成测试音频
	tts.Process(pipeline.Packet{
		Data: "你好",
		Seq:  0,
		Src:  nil,
	})

	// 等待处理完成
	time.Sleep(5 * time.Second)

	// 停止所有组件
	wavDumper.Stop()
	upsampler.Stop()
	tts.Stop()
}
