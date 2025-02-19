package tts

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/tencentcloud/tencentcloud-speech-sdk-go/tts"
)

func init() {
	if err := godotenv.Load("../../../.env.test"); err != nil {
		panic("Error loading .env.test file")
	}
}

// // getProjectRoot 获取项目根目录
// func getProjectRoot() string {
// 	_, filename, _, _ := runtime.Caller(0)
// 	return filepath.Join(filepath.Dir(filename), "..", "..", "..")
// }

// mockListener 模拟的监听器实现
type mockListener struct {
	sync.Mutex
	startCalled    bool
	endCalled      bool
	audioReceived  []byte
	textReceived   []map[string]interface{}
	errorReceived  []map[string]interface{}
	startSessionID string
	// 新增：用于跟踪每段文本的音频数据
	currentText   string
	audioSegments map[string][]byte
}

func newMockListener() *mockListener {
	return &mockListener{
		audioReceived: make([]byte, 0),
		textReceived:  make([]map[string]interface{}, 0),
		errorReceived: make([]map[string]interface{}, 0),
		audioSegments: make(map[string][]byte),
	}
}

func (l *mockListener) OnSynthesisStart(sessionID string) {
	l.Lock()
	defer l.Unlock()
	l.startCalled = true
	l.startSessionID = sessionID
}

func (l *mockListener) OnSynthesisEnd() {
	l.Lock()
	defer l.Unlock()
	l.endCalled = true
}

func (l *mockListener) OnTextResult(response map[string]interface{}) {
	l.Lock()
	defer l.Unlock()
	l.textReceived = append(l.textReceived, response)

	// 打印完整的响应内容以便调试
	// fmt.Printf("OnTextResult full response: %+v\n", response)

	// 从 result.subtitles 中提取文本
	if result, ok := response["result"].(map[string]interface{}); ok {
		if subtitles, ok := result["subtitles"].([]interface{}); ok {
			var fullText strings.Builder
			for _, sub := range subtitles {
				if subtitle, ok := sub.(map[string]interface{}); ok {
					if text, ok := subtitle["Text"].(string); ok {
						fullText.WriteString(text)
					}
				}
			}
			text := fullText.String()
			if text != "" {
				fmt.Printf("Extracted full text: %s\n", text)
				l.currentText = text
			}
		}
	}
}

func (l *mockListener) OnSynthesisFail(response map[string]interface{}) {
	l.Lock()
	defer l.Unlock()
	l.errorReceived = append(l.errorReceived, response)
}

func (l *mockListener) OnAudioResult(audioBytes []byte) {
	l.Lock()
	defer l.Unlock()
	log.Printf("OnAudioResult: %d bytes", len(audioBytes))
	if len(audioBytes) == 0 {
		fmt.Printf("time: %v, cur end", time.Now())
		l.audioSegments[l.currentText] = l.audioReceived
		l.audioReceived = make([]byte, 0)
		return
	}
	fmt.Printf("time: %v, cur data", time.Now())

	l.audioReceived = append(l.audioReceived, audioBytes...)
}

func TestFlowingSpeechSynthesizer_Basic(t *testing.T) {
	// 从环境变量获取配置
	appIDStr := os.Getenv("TENCENTTTS_APP_ID")
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		t.Fatalf("Failed to parse APP_ID: %v", err)
	}
	secretID := os.Getenv("TENCENTTTS_SECRET_ID")
	secretKey := os.Getenv("TENCENTTTS_SECRET_KEY")

	// 创建凭证
	credential := &Credential{
		SecretID:  secretID,
		SecretKey: secretKey,
	}

	// 创建监听器
	listener := newMockListener()

	// 创建合成器
	synthesizer := NewFlowingSpeechSynthesizer(appID, credential, listener)
	assert.NotNil(t, synthesizer)

	// 设置参数
	// 301037
	synthesizer.SetVoiceType(601000)
	synthesizer.SetCodec("mp3")
	synthesizer.SetSampleRate(16000)
	synthesizer.SetVolume(0)
	synthesizer.SetSpeed(0)
	synthesizer.SetEnableSubtitle(true)

	// 启动合成器
	err = synthesizer.Start()
	assert.NoError(t, err)

	// 等待就绪
	ready := synthesizer.WaitReady(5000)
	fmt.Printf("ready: %v\n", ready)
	assert.True(t, ready)

	// 准备测试文本
	texts := []string{
		"今天天气真不错，阳光明媚，让人心情愉悦。",
		"今天天气真不错，挺风和日丽的。",
	}

	// 发送所有文本
	for i, text := range texts {
		t.Logf("Processing text %d: %s, time: %v", i+1, text, time.Now())
		err = synthesizer.Process(text, "ACTION_SYNTHESIS")
		listener.audioSegments[text] = nil
		assert.NoError(t, err)
		// 增加等待时间，确保文本处理完成
		time.Sleep(500 * time.Millisecond)
	}

	// 所有文本发送完后，发送一次完成请求
	err = synthesizer.Complete("ACTION_COMPLETE")
	assert.NoError(t, err)

	// 等待合成完成
	synthesizer.Wait()

	// 停止合成器
	synthesizer.Stop()

	// 保存每段文本对应的音频文件
	for i, text := range texts {
		if audioData, ok := listener.audioSegments[text]; ok {
			filename := fmt.Sprintf("output_%d.mp3", i+1)
			tts.WriteFile(filename, audioData)
			fmt.Printf("Saved audio for text %d to %s, size: %d bytes\n", i+1, filename, len(audioData))
		}
	}

	// 验证结果
	assert.True(t, listener.startCalled)
	assert.True(t, listener.endCalled)
	assert.NotEmpty(t, listener.startSessionID)
	assert.Equal(t, len(texts), len(listener.audioSegments))

	// 打印统计信息
	t.Logf("Total audio segments: %d", len(listener.audioSegments))
	for text, audio := range listener.audioSegments {
		t.Logf("Text: %s, Audio size: %d bytes", text, len(audio))
	}
}

func TestFlowingSpeechSynthesizer_Parameters(t *testing.T) {
	credential := &Credential{
		SecretID:  "test-id",
		SecretKey: "test-key",
	}
	listener := newMockListener()
	synthesizer := NewFlowingSpeechSynthesizer(502001, credential, listener)

	// 测试参数设置
	testCases := []struct {
		name     string
		setter   func()
		getter   func() interface{}
		expected interface{}
	}{
		{
			name:     "VoiceType",
			setter:   func() { synthesizer.SetVoiceType(101) },
			getter:   func() interface{} { return synthesizer.voiceType },
			expected: int64(101),
		},
		{
			name:     "Volume",
			setter:   func() { synthesizer.SetVolume(8) },
			getter:   func() interface{} { return synthesizer.volume },
			expected: 8,
		},
		{
			name:     "Speed",
			setter:   func() { synthesizer.SetSpeed(2) },
			getter:   func() interface{} { return synthesizer.speed },
			expected: 2,
		},
		{
			name:     "Codec",
			setter:   func() { synthesizer.SetCodec("wav") },
			getter:   func() interface{} { return synthesizer.codec },
			expected: "wav",
		},
		{
			name:     "SampleRate",
			setter:   func() { synthesizer.SetSampleRate(24000) },
			getter:   func() interface{} { return synthesizer.sampleRate },
			expected: 24000,
		},
		{
			name:     "EnableSubtitle",
			setter:   func() { synthesizer.SetEnableSubtitle(true) },
			getter:   func() interface{} { return synthesizer.enableSubtitle },
			expected: true,
		},
		{
			name:     "EmotionCategory",
			setter:   func() { synthesizer.SetEmotionCategory("happy") },
			getter:   func() interface{} { return synthesizer.emotionCategory },
			expected: "happy",
		},
		{
			name:     "EmotionIntensity",
			setter:   func() { synthesizer.SetEmotionIntensity(80) },
			getter:   func() interface{} { return synthesizer.emotionIntensity },
			expected: 80,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setter()
			assert.Equal(t, tc.expected, tc.getter())
		})
	}
}

func TestFlowingSpeechSynthesizer_SignatureGeneration(t *testing.T) {
	credential := &Credential{
		SecretID:  "test-id",
		SecretKey: "test-key",
	}
	listener := newMockListener()
	synthesizer := NewFlowingSpeechSynthesizer(502001, credential, listener)

	// 测试参数生成
	params := synthesizer.genParams("test-session")
	assert.Equal(t, _ACTION, params["Action"])
	assert.Equal(t, int64(502001), params["AppId"])
	assert.Equal(t, "test-id", params["SecretId"])
	assert.Equal(t, "test-session", params["SessionId"])

	// 测试签名生成
	signature := synthesizer.genSignature(params)
	assert.NotEmpty(t, signature)
}

func TestFlowingSpeechSynthesizer_MessageHandling(t *testing.T) {
	// 从环境变量获取配置
	appIDStr := os.Getenv("TENCENTTTS_APP_ID")
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		t.Fatalf("Failed to parse APP_ID: %v", err)
	}
	secretID := os.Getenv("TENCENTTTS_SECRET_ID")
	secretKey := os.Getenv("TENCENTTTS_SECRET_KEY")

	credential := &Credential{
		SecretID:  secretID,
		SecretKey: secretKey,
	}
	listener := newMockListener()
	synthesizer := NewFlowingSpeechSynthesizer(appID, credential, listener)
	synthesizer.SetCodec("mp3")

	// 启动合成器
	err = synthesizer.Start()
	assert.NoError(t, err)

	// 等待就绪
	ready := synthesizer.WaitReady(5000)
	assert.True(t, ready)

	// 测试多个文本合成请求
	texts := []string{
		"第一句测试文本",
		"第二句测试文本",
		"第三句测试文本",
	}

	for _, text := range texts {
		err = synthesizer.Process(text, "ACTION_SYNTHESIS")
		assert.NoError(t, err)
		time.Sleep(100 * time.Millisecond)
	}

	err = synthesizer.Complete("ACTION_COMPLETE")
	assert.NoError(t, err)

	// 等待合成完成
	synthesizer.Wait()

	// 停止合成器
	synthesizer.Stop()

	tts.WriteFile("output.mp3", listener.audioReceived)

	// 验证结果
	assert.True(t, listener.startCalled)
	assert.True(t, listener.endCalled)
	assert.NotEmpty(t, listener.audioReceived)
	assert.NotEmpty(t, listener.startSessionID)

	// 打印统计信息
	t.Logf("Received %d audio chunks", len(listener.audioReceived))
	t.Logf("Received %d text results", len(listener.textReceived))
	t.Logf("Total audio data size: %d bytes", getTotalAudioSize(listener.audioReceived))
}

// getTotalAudioSize 计算音频数据的总大小
func getTotalAudioSize(data []byte) int {
	return len(data)
}

func TestFlowingSpeechSynthesizer_ErrorHandling(t *testing.T) {
	credential := &Credential{
		SecretID:  "invalid-id",
		SecretKey: "invalid-key",
	}
	listener := newMockListener()
	synthesizer := NewFlowingSpeechSynthesizer(502001, credential, listener)

	// 启动合成器
	err := synthesizer.Start()
	assert.NoError(t, err) // Start() 本身不会返回错误，因为错误会在使用时发生

	// 尝试处理文本，这时应该会触发错误
	err = synthesizer.Process("测试文本", "ACTION_SYNTHESIS")
	assert.NoError(t, err) // Process() 本身也不会返回错误，因为它只是发送消息

	// 等待一段时间以确保收到错误回调
	time.Sleep(1 * time.Second)
	assert.True(t, len(listener.errorReceived) > 0, "Should receive error callback")

	// 测试无效的参数
	synthesizer.SetVoiceType(-1)
	synthesizer.SetSampleRate(-1)
	params := synthesizer.genParams("test-session")
	assert.Equal(t, int64(-1), params["VoiceType"])
	assert.Equal(t, -1, params["SampleRate"])
}

func TestFlowingSpeechSynthesizer_RequestMessage(t *testing.T) {
	credential := &Credential{
		SecretID:  "test-id",
		SecretKey: "test-key",
	}
	listener := newMockListener()
	synthesizer := NewFlowingSpeechSynthesizer(502001, credential, listener)
	synthesizer.sessionID = "test-session"

	// 测试请求消息格式
	msg := synthesizer.newWSRequestMessage("test-action", "test-data")
	assert.Equal(t, "test-session", msg["session_id"])
	assert.Equal(t, "test-action", msg["action"])
	assert.Equal(t, "test-data", msg["data"])
	assert.NotEmpty(t, msg["message_id"])

	// 测试消息序列化
	data, err := json.Marshal(msg)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)
}
