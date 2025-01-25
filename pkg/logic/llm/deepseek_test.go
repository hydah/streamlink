package llm

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"
	"voiceagent/pkg/logic/pipeline"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
)

func init() {
	if err := godotenv.Load("../../../.env.test"); err != nil {
		log.Printf("Error loading .env.test file: %v", err)
	}
}

func TestDeepSeek_Process(t *testing.T) {
	apiKey := os.Getenv("SILICON_API_KEY")
	if apiKey == "" {
		t.Skip("SILICON_API_KEY not set")
	}

	ds := NewDeepSeek(apiKey, "https://api.siliconflow.cn/v1")

	// 测试处理文本
	resultReceived := false
	ds.SetOutput(func(packet pipeline.Packet) {
		resultReceived = true
		assert.IsType(t, "", packet.Data)
		response, ok := packet.Data.(string)
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
	time.Sleep(2 * time.Second)
	assert.True(t, resultReceived, "Should receive result for valid text")

	// 测试历史记录是否正确保存
	if len(ds.messages) != 2 { // 一条用户消息和一条助手回复
		t.Errorf("Expected 2 messages in history, got %d", len(ds.messages))
	}

	// 测试处理不支持的数据类型
	ds.Process(pipeline.Packet{
		Data: []byte{1, 2, 3, 4},
		Seq:  1,
		Src:  nil,
	})
}

func TestDeepSeek_ClearHistory(t *testing.T) {
	apiKey := os.Getenv("SILICON_API_KEY")
	if apiKey == "" {
		t.Skip("SILICON_API_KEY not set")
	}

	ds := NewDeepSeek(apiKey, "https://api.siliconflow.cn/v1")

	// 添加一些消息
	ds.Process(pipeline.Packet{
		Data: "What is the capital of China?",
		Seq:  0,
		Src:  nil,
	})
	ds.Process(pipeline.Packet{
		Data: "What is its population?",
		Seq:  1,
		Src:  nil,
	})

	// 清除历史
	ds.ClearHistory()
	if len(ds.messages) != 0 {
		t.Errorf("Expected empty message history after clear, got %d messages", len(ds.messages))
	}
}

func TestDeepSeek_SetMaxMessages(t *testing.T) {
	apiKey := os.Getenv("SILICON_API_KEY")
	if apiKey == "" {
		t.Skip("SILICON_API_KEY not set")
	}

	ds := NewDeepSeek(apiKey, "https://api.siliconflow.cn/v1")

	// 设置最大消息数为2
	ds.SetMaxMessages(2)

	// 第一轮对话：应该保留用户消息和助手回复
	ds.Process(pipeline.Packet{
		Data: "What is the capital of China?",
		Seq:  0,
		Src:  nil,
	})
	time.Sleep(2 * time.Second)
	if len(ds.messages) != 2 {
		t.Errorf("After first message - Expected 2 messages, got %d", len(ds.messages))
	}

	// 第二轮对话：应该移除最早的消息，保留最新的消息
	ds.Process(pipeline.Packet{
		Data: "What is its population?",
		Seq:  1,
		Src:  nil,
	})
	time.Sleep(2 * time.Second)
	if len(ds.messages) != 2 {
		t.Errorf("After second message - Expected 2 messages, got %d", len(ds.messages))
	}

	// 验证消息数量始终保持在限制内
	if len(ds.messages) > ds.maxMessages {
		t.Errorf("Message count %d exceeds limit %d", len(ds.messages), ds.maxMessages)
	}
}

func TestDeepSeek_SetModel(t *testing.T) {
	apiKey := os.Getenv("SILICON_API_KEY")
	if apiKey == "" {
		t.Skip("SILICON_API_KEY not set")
	}

	ds := NewDeepSeek(apiKey, "https://api.siliconflow.cn/v1")

	// 测试设置模型
	newModel := "deepseek-reasoner"
	ds.SetModel(newModel)
	if ds.model != newModel {
		t.Errorf("Expected model to be %s, got %s", newModel, ds.model)
	}
}

func TestDeepSeek_StreamProcessing(t *testing.T) {
	apiKey := os.Getenv("SILICON_API_KEY")
	if apiKey == "" {
		t.Skip("SILICON_API_KEY not set")
	}

	ds := NewDeepSeek(apiKey, "https://api.siliconflow.cn/v1")

	// 测试流式处理
	fmt.Println("\n=== Testing streaming mode ===")
	streamChan := ds.ProcessTextStream("What is the capital of China?")

	var streamResponse string
	var chunkCount int
	startTime := time.Now()
	var firstChunkTime time.Duration
	var totalTime time.Duration

	fmt.Println("Starting to receive stream chunks...")

	for chunk := range streamChan {
		chunkCount++

		// 每个块都应该是非空的
		if chunk == "" {
			t.Error("Received empty chunk in stream")
		}

		// 记录首个chunk的接收时间
		if chunkCount == 1 {
			firstChunkTime = time.Since(startTime)
			fmt.Printf("\nFirst chunk received after: %v", firstChunkTime)
		}

		streamResponse += chunk
	}

	totalTime = time.Since(startTime)
	averageTime := totalTime / time.Duration(chunkCount)

	fmt.Printf("\n\nStreaming Statistics:")
	fmt.Printf("\n- Time to first chunk: %v", firstChunkTime)
	fmt.Printf("\n- Total chunks: %d", chunkCount)
	fmt.Printf("\n- Total time: %v", totalTime)
	fmt.Printf("\n- Average time per chunk: %v", averageTime)
	fmt.Printf("\n\nFinal response:\n%s\n", streamResponse)

	// 完整的响应不应该为空
	if streamResponse == "" {
		t.Error("Expected non-empty response for stream mode")
	}

	// 验证是否确实收到多个块
	if chunkCount < 3 {
		t.Errorf("Expected at least 3 chunks for streaming response, got %d", chunkCount)
	}

	// 验证消息历史是否正确保存
	if len(ds.messages) != 2 { // 一轮对话，两条消息
		t.Errorf("Expected 2 messages in history, got %d", len(ds.messages))
	}
}

func TestDeepSeek_MultiTurnPerformance(t *testing.T) {
	apiKey := os.Getenv("SILICON_API_KEY")
	if apiKey == "" {
		t.Skip("SILICON_API_KEY not set")
	}

	// 定义多轮对话的问题
	questions := []string{
		"What is the capital of China?",
		"What is its population?",
		"What is the largest city in China",
	}

	fmt.Println("\n=== Testing Multi-turn Performance ===")

	// 测试非流式处理
	fmt.Println("\n--- Non-streaming mode ---")
	dsNonStream := NewDeepSeek(apiKey, "https://api.siliconflow.cn/v1")

	// 设置输出函数以捕获响应
	var responses []string
	dsNonStream.SetOutput(func(packet pipeline.Packet) {
		if response, ok := packet.Data.(string); ok {
			responses = append(responses, response)
		}
	})

	for i, question := range questions {
		start := time.Now()
		dsNonStream.Process(pipeline.Packet{
			Data: question,
			Seq:  i,
			Src:  nil,
		})
		// 等待处理完成
		time.Sleep(2 * time.Second)
		duration := time.Since(start)

		fmt.Printf("\nTurn %d:", i+1)
		fmt.Printf("\n- Question: %s", question)
		if i < len(responses) {
			fmt.Printf("\n- Response: %s", responses[i])
		}
		fmt.Printf("\n- Total time: %v", duration)
	}

	// 使用新的客户端进行流式测试
	fmt.Println("\n\n--- Streaming mode ---")
	dsStream := NewDeepSeek(apiKey, "https://api.siliconflow.cn/v1")
	for i, question := range questions {
		fmt.Printf("\nTurn %d:", i+1)
		fmt.Printf("\n- Question: %s", question)

		startTime := time.Now()
		streamChan := dsStream.ProcessTextStream(question)

		var streamResponse string
		var chunkCount int
		var firstChunkTime time.Duration

		for chunk := range streamChan {
			chunkCount++

			if chunkCount == 1 {
				firstChunkTime = time.Since(startTime)
				fmt.Printf("\n- Time to first chunk: %v", firstChunkTime)
			}

			streamResponse += chunk
		}

		totalTime := time.Since(startTime)
		averageChunkTime := totalTime / time.Duration(chunkCount)

		fmt.Printf("\n- Total chunks: %d", chunkCount)
		fmt.Printf("\n- Average time per chunk: %v", averageChunkTime)
		fmt.Printf("\n- Total time: %v", totalTime)
		fmt.Printf("\n- Response: %s", streamResponse)
	}

	// 验证两个客户端的消息历史是否正确保存
	expectedMessages := len(questions) * 2 // 每轮对话包含一个问题和一个回答
	if len(dsNonStream.messages) != expectedMessages {
		t.Errorf("Non-streaming client: Expected %d messages in history, got %d", expectedMessages, len(dsNonStream.messages))
	}
	if len(dsStream.messages) != expectedMessages {
		t.Errorf("Streaming client: Expected %d messages in history, got %d", expectedMessages, len(dsStream.messages))
	}
}
