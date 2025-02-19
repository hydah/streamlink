package tts

import (
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"streamlink/pkg/logic/codec"
	"streamlink/pkg/logic/dumper"
	"streamlink/pkg/logic/pipeline"
	"streamlink/pkg/logic/resampler"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
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

func getTestClient(t *testing.T) *TencentTTS {
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
	tts.SetInput() // Set input channel before starting

	// Set default output channel
	outChan := make(chan pipeline.Packet, 100)
	tts.SetOutputChan(outChan)

	// 启动服务
	err = tts.Start()
	if err != nil {
		t.Fatalf("Failed to start TencentTTS: %v", err)
	}

	return tts
}

func cleanup(tts *TencentTTS) {
	if tts != nil {
		tts.Stop()
		close(tts.GetOutputChan())
	}
}

func TestTencentTTS_Process(t *testing.T) {
	tts := getTestClient(t)
	defer cleanup(tts)

	// 测试处理字符串数据
	resultReceived := false
	tts.SetOutput(func(packet pipeline.Packet) {
		resultReceived = true
		assert.IsType(t, []byte{}, packet.Data)
		audioData, ok := packet.Data.([]byte)
		assert.True(t, ok)
		assert.NotEmpty(t, audioData)
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
}

func TestTencentTTS_SetVoiceAndCodec(t *testing.T) {
	tts := getTestClient(t)
	defer cleanup(tts)

	resultReceived := false
	tts.SetOutput(func(packet pipeline.Packet) {
		resultReceived = true
		assert.IsType(t, []byte{}, packet.Data)
		audioData, ok := packet.Data.([]byte)
		assert.True(t, ok)
		assert.NotEmpty(t, audioData)
	})

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

	// 等待一段时间以确保处理完成
	time.Sleep(1 * time.Second)
	assert.True(t, resultReceived, "Should receive result for PCM format")

	// 测试 MP3 格式
	resultReceived = false
	tts.SetCodec("mp3")
	tts.Process(pipeline.Packet{
		Data: "测试MP3格式语音",
		Seq:  1,
		Src:  nil,
	})

	// 等待一段时间以确保处理完成
	time.Sleep(1 * time.Second)
	assert.True(t, resultReceived, "Should receive result for MP3 format")
}

func TestTencentTTS_WithOpusEncoding(t *testing.T) {
	tts := getTestClient(t)
	defer cleanup(tts)

	// 创建上采样器 (16kHz -> 48kHz, 单声道)
	upsampler, err := resampler.NewResampler(16000, 48000, 1, 2)
	assert.NoError(t, err)

	// 创建 Opus 编码器 (48kHz, 单声道)
	opusEncoder, err := codec.NewOpusEncoder(48000, 2)
	assert.NoError(t, err)

	// 创建 OGG 转储器 (48kHz, 单声道)
	oggDumper, err := dumper.NewOggDumper(48000, 2, path.Join(getProjectRoot(), "testcase", "testdump", "tencent_tts_test.ogg"))
	assert.NoError(t, err)

	resultReceived := false
	oggDumper.SetOutput(func(packet pipeline.Packet) {
		resultReceived = true
		// Check if it's an RTPAudioPacket
		audioPacket, ok := packet.Data.(*codec.RTPAudioPacket)
		assert.True(t, ok)
		assert.NotNil(t, audioPacket)
		assert.NotEmpty(t, audioPacket.Payload())
	})

	// 设置处理链
	tts.Connect(upsampler).Connect(opusEncoder).Connect(oggDumper)

	// 启动所有组件
	upsampler.Start()
	opusEncoder.Start()
	oggDumper.Start()

	// 生成测试音频
	tts.Process(pipeline.Packet{
		Data: "背一首李白的诗",
		Seq:  0,
		Src:  nil,
	})

	// 等待处理完成
	time.Sleep(5 * time.Second)
	assert.True(t, resultReceived, "Should receive result for Opus encoding")

	// 停止所有组件
	oggDumper.Stop()
	opusEncoder.Stop()
	upsampler.Stop()
}

func TestTencentTTS_WithWav(t *testing.T) {
	tts := getTestClient(t)
	defer cleanup(tts)

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
}
