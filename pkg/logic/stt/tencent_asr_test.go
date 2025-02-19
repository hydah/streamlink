package stt

import (
	"fmt"
	"log"
	"os"
	"streamlink/pkg/logic/pipeline"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
)

var (
	// EngineModelType EngineModelType
	EngineModelType = "16k_zh"
	// SliceSize SliceSize
	SliceSize = 6400
)

func init() {
	if err := godotenv.Load("../../../.env.test"); err != nil {
		log.Printf("Error loading .env.test file: %v", err)
	}
}

func TestNewTencentAsr(t *testing.T) {
	// 跳过测试如果环境变量未设置
	appID := os.Getenv("TENCENTASR_APP_ID")
	secretID := os.Getenv("TENCENTASR_SECRET_ID")
	secretKey := os.Getenv("TENCENTASR_SECRET_KEY")
	if appID == "" || secretID == "" || secretKey == "" {
		t.Skip("Tencent credentials not set in environment variables")
	}
	// 创建 ASR 实例
	asr := NewTencentAsr(appID, secretID, secretKey, EngineModelType, SliceSize)

	assert.NotNil(t, asr)
	assert.Equal(t, appID, asr.appID)
	assert.Equal(t, secretID, asr.secretID)
	assert.Equal(t, secretKey, asr.secretKey)
	assert.Equal(t, EngineModelType, asr.engineModelType)
	assert.Equal(t, SliceSize, asr.sliceSize)
	assert.NotNil(t, asr.resultChan)
}

func TestTencentAsr_StartStop(t *testing.T) {
	// 跳过测试如果环境变量未设置
	appID := os.Getenv("TENCENTASR_APP_ID")
	secretID := os.Getenv("TENCENTASR_SECRET_ID")
	secretKey := os.Getenv("TENCENTASR_SECRET_KEY")
	if appID == "" || secretID == "" || secretKey == "" {
		t.Skip("Tencent credentials not set in environment variables")
	}
	asr := NewTencentAsr(appID, secretID, secretKey, EngineModelType, SliceSize)

	// 测试启动
	err := asr.Start()
	assert.Nil(t, err)

	// 测试重复启动
	err = asr.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")

	// 测试停止
	asr.Stop()
	assert.Nil(t, asr.recognizer)
	assert.Empty(t, asr.GetResult())
}

func getTestClient(t *testing.T) *TencentAsr {
	// 跳过测试如果环境变量未设置
	appID := os.Getenv("TENCENTASR_APP_ID")
	secretID := os.Getenv("TENCENTASR_SECRET_ID")
	secretKey := os.Getenv("TENCENTASR_SECRET_KEY")
	if appID == "" || secretID == "" || secretKey == "" {
		t.Skip("Tencent credentials not set in environment variables")
	}

	// 创建 ASR 实例
	asr := NewTencentAsr(appID, secretID, secretKey, EngineModelType, SliceSize)
	asr.SetInput()

	// 启动服务
	err := asr.Start()
	if err != nil {
		t.Fatalf("Failed to start TencentAsr: %v", err)
	}

	return asr
}

func cleanup(asr *TencentAsr) {
	if asr != nil {
		asr.Stop()
	}
}

func TestTencentAsr_Process(t *testing.T) {
	asr := getTestClient(t)
	defer cleanup(asr)

	// 测试处理 []byte 数据
	errorReceived := false
	asr.SetOutput(func(packet pipeline.Packet) {
		errorReceived = true
		// 不期望从无效数据收到结果
		t.Error("Should not receive result for invalid audio data")
	})

	// 测试处理 []byte 数据
	byteData := []byte{1, 2, 3, 4}
	asr.Process(pipeline.Packet{
		Data: byteData,
		Seq:  0,
		Src:  nil,
	})

	// 测试处理 []int16 数据
	int16Data := []int16{1, 2, 3, 4}
	asr.Process(pipeline.Packet{
		Data: int16Data,
		Seq:  1,
		Src:  nil,
	})

	// 等待一段时间以确保处理完成
	time.Sleep(1 * time.Second)
	assert.False(t, errorReceived, "Should not receive result for invalid audio data")
}

func TestTencentAsr_ResultHandling(t *testing.T) {
	// 跳过测试如果环境变量未设置
	appID := os.Getenv("TENCENTASR_APP_ID")
	secretID := os.Getenv("TENCENTASR_SECRET_ID")
	secretKey := os.Getenv("TENCENTASR_SECRET_KEY")
	if appID == "" || secretID == "" || secretKey == "" {
		t.Skip("Tencent credentials not set in environment variables")
	}

	// 创建 ASR 实例
	asr := NewTencentAsr(appID, secretID, secretKey, EngineModelType, SliceSize)

	// 测试获取结果
	assert.Empty(t, asr.GetResult())

	// 测试结果通道
	resultChan := asr.GetResultChan()
	assert.NotNil(t, resultChan)

	// 模拟发送结果到通道
	go func() {
		for i := 0; i < 150; i++ { // 超过通道容量
			select {
			case asr.resultChan <- fmt.Sprintf("result-%d", i):
			default:
				// 通道已满，跳过
			}
		}
	}()

	// 读取一些结果
	count := 0
	timeout := time.After(100 * time.Millisecond)
	for {
		select {
		case result := <-resultChan:
			assert.Contains(t, result, "result-")
			count++
		case <-timeout:
			goto done
		}
	}
done:
	assert.True(t, count > 0)
}

func TestTencentAsr_ProcessRealAudio(t *testing.T) {
	asr := getTestClient(t)
	defer cleanup(asr)

	// 打开测试音频文件
	audio, err := os.Open("testdata/test.pcm")
	if err != nil {
		t.Skipf("Test audio file not found: %v", err)
		return
	}
	defer audio.Close()

	// 设置输出回调
	resultReceived := false
	asr.SetOutput(func(packet pipeline.Packet) {
		resultReceived = true
		assert.IsType(t, "", packet.Data)
		response, ok := packet.Data.(string)
		fmt.Println("response: ", response)
		assert.True(t, ok)
		assert.NotEmpty(t, response)
	})

	// 读取并处理音频数据
	buffer := make([]byte, SliceSize)
	for {
		n, err := audio.Read(buffer)
		if err != nil {
			break
		}
		if n <= 0 {
			break
		}

		asr.Process(pipeline.Packet{
			Data: buffer[:n],
			Seq:  0,
			Src:  nil,
		})

		// 模拟实时音频流，每200ms发送一次数据
		time.Sleep(200 * time.Millisecond)
	}

	// 等待一段时间以确保所有结果都被处理
	time.Sleep(5 * time.Second)
	assert.True(t, resultReceived, "Should receive result for valid audio")
}
