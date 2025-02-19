package llm

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

func init() {
	if err := godotenv.Load("../../../.env.test"); err != nil {
		log.Printf("Error loading .env.test file: %v", err)
	}
}

func getTestClient(t *testing.T) *DeepSeek {
	apiKey := os.Getenv("SILICON_API_KEY")
	if apiKey == "" {
		t.Skip("SILICON_API_KEY not set")
	}
	baseURL := "https://api.siliconflow.cn/v1"

	ds := NewDeepSeek(apiKey, baseURL)
	ds.SetInput()
	// Start the component
	if err := ds.Start(); err != nil {
		t.Fatalf("Failed to start DeepSeek: %v", err)
	}
	return ds
}

func cleanup(ds *DeepSeek) {
	if ds != nil {
		ds.Stop()
		ds.ClearHistory()
	}
}

func TestDeepSeek_Process(t *testing.T) {
	ds := getTestClient(t)
	defer cleanup(ds)

	// 测试处理文本
	resultReceived := false
	ds.SetOutput(func(packet pipeline.Packet) {
		resultReceived = true
		assert.IsType(t, "", packet.Data)
		response, ok := packet.Data.(string)
		fmt.Println("response: ", response)
		assert.True(t, ok)
		assert.NotEmpty(t, response)
	})

	// 测试处理字符串数据
	ds.Process(pipeline.Packet{
		Data: "What is the capital of China?",
		Seq:  0,
		Src:  nil,
	})

	// 等待一段时间以确保处理完成
	time.Sleep(5 * time.Second)
	assert.True(t, resultReceived, "Should receive result for valid text")

	// 测试历史记录是否正确保存
	assert.Equal(t, 2, len(ds.messages), "Expected 2 messages in history") // 一条用户消息和一条助手回复
}

func TestDeepSeek_Streaming(t *testing.T) {
	ds := getTestClient(t)
	defer cleanup(ds)

	// Test streaming configuration
	assert.True(t, ds.streaming, "Streaming should be enabled by default")
	ds.SetStreaming(false)
	assert.False(t, ds.streaming, "Streaming should be disabled after SetStreaming(false)")
	ds.SetStreaming(true)
	assert.True(t, ds.streaming, "Streaming should be enabled after SetStreaming(true)")

	// Test with streaming enabled
	resultCount := 0
	ds.SetOutput(func(packet pipeline.Packet) {
		resultCount++
		assert.IsType(t, "", packet.Data)
		response, ok := packet.Data.(string)
		assert.True(t, ok)
		assert.NotEmpty(t, response)
	})

	ds.Process(pipeline.Packet{
		Data: "Count from 1 to 5",
		Seq:  0,
		Src:  nil,
	})

	time.Sleep(5 * time.Second)
	assert.Greater(t, resultCount, 1, "Should receive multiple chunks when streaming is enabled")

	// Test with streaming disabled
	ds.SetStreaming(false)
	resultCount = 0
	ds.Process(pipeline.Packet{
		Data: "Count from 1 to 5",
		Seq:  1,
		Src:  nil,
	})

	time.Sleep(5 * time.Second)
	assert.Equal(t, 1, resultCount, "Should receive single response when streaming is disabled")
}

func TestDeepSeek_ClearHistory(t *testing.T) {
	ds := getTestClient(t)
	defer cleanup(ds)

	// 添加一些消息
	ds.Process(pipeline.Packet{
		Data: "What is the capital of China?",
		Seq:  0,
		Src:  nil,
	})
	time.Sleep(5 * time.Second)

	// 清除历史
	ds.ClearHistory()
	assert.Equal(t, 0, len(ds.messages), "Expected empty message history after clear")
}

func TestDeepSeek_SetMaxMessages(t *testing.T) {
	ds := getTestClient(t)
	defer cleanup(ds)

	// 设置最大消息数为2
	ds.SetMaxMessages(2)

	// 第一轮对话：应该保留用户消息和助手回复
	ds.Process(pipeline.Packet{
		Data: "What is the capital of China?",
		Seq:  0,
		Src:  nil,
	})
	time.Sleep(5 * time.Second)
	assert.Equal(t, 2, len(ds.messages), "After first message - Expected 2 messages")

	// 第二轮对话：应该移除最早的消息，保留最新的消息
	ds.Process(pipeline.Packet{
		Data: "What is its population?",
		Seq:  1,
		Src:  nil,
	})
	time.Sleep(5 * time.Second)
	assert.Equal(t, 2, len(ds.messages), "After second message - Expected 2 messages")

	// 验证消息数量始终保持在限制内
	assert.LessOrEqual(t, len(ds.messages), ds.maxMessages, "Message count should not exceed limit")
}

func TestDeepSeek_SetModel(t *testing.T) {
	ds := getTestClient(t)
	defer cleanup(ds)

	// 测试设置模型
	newModel := "gpt-4"
	ds.SetModel(newModel)
	assert.Equal(t, newModel, ds.model, "Model should be updated")
}
