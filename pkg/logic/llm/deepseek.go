package llm

import (
	"context"
	"fmt"
	"streamlink/pkg/logger"
	"streamlink/pkg/logic/pipeline"
	"sync"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/ssestream"
)

// ChatClient 定义了聊天客户端的接口
type ChatClient interface {
	New(ctx context.Context, params openai.ChatCompletionNewParams, opts ...option.RequestOption) (*openai.ChatCompletion, error)
	NewStreaming(ctx context.Context, params openai.ChatCompletionNewParams, opts ...option.RequestOption) *ssestream.Stream[openai.ChatCompletionChunk]
}

// DeepSeek 实现 Component 接口
type DeepSeek struct {
	*pipeline.BaseComponent
	client      ChatClient
	messages    []openai.ChatCompletionMessageParamUnion
	model       string
	maxMessages int
	streaming   bool
	mu          sync.Mutex
	metrics     pipeline.TurnMetrics
	// 自定义指标
	firstTokenLatencyMs int64 // 首token延迟(毫秒)
	totalLatencyMs      int64 // 总延迟(毫秒)
}

// NewDeepSeek 创建一个新的 DeepSeek 实例
func NewDeepSeek(apiKey string, baseURL string) *DeepSeek {
	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(baseURL),
	)

	d := &DeepSeek{
		BaseComponent: pipeline.NewBaseComponent("DeepSeek", 100),
		client:        client.Chat.Completions,
		messages:      make([]openai.ChatCompletionMessageParamUnion, 0),
		model:         "Qwen/Qwen2.5-14B-Instruct",
		maxMessages:   10,    // 保留最近的10条消息
		streaming:     false, // 默认启用流式处理
	}

	// 设置处理函数
	d.BaseComponent.SetProcess(d.processPacket)
	d.RegisterCommandHandler(pipeline.PacketCommandInterrupt, d.handleInterrupt)

	return d
}

func (d *DeepSeek) handleInterrupt(packet pipeline.Packet) {
	logger.Info("**%s** Received interrupt command for turn %d", d.GetName(), packet.TurnSeq)
	d.SetCurTurnSeq(packet.TurnSeq)

	d.ForwardPacket(packet)
}

func (d *DeepSeek) ProcessText(text string) string {
	// 如果添加新消息后会超过最大限制，移除最早的消息
	for len(d.messages) >= d.maxMessages {
		d.messages = d.messages[1:]
	}

	// 添加用户消息
	d.messages = append(d.messages, openai.UserMessage(text))

	// 创建聊天完成请求
	resp, err := d.client.New(
		context.Background(),
		openai.ChatCompletionNewParams{
			Messages: openai.F(d.messages),
			Model:    openai.F(d.model),
		},
	)

	if err != nil {
		logger.Error("Error creating chat completion: %v", err)
		return ""
	}

	// 获取助手的回复
	assistantMessage := resp.Choices[0].Message.Content

	// 如果添加助手回复会超过限制，先移除最早的消息
	if len(d.messages) >= d.maxMessages {
		d.messages = d.messages[1:]
	}

	d.messages = append(d.messages, openai.AssistantMessage(assistantMessage))

	return assistantMessage
}

// processPacket 处理输入的数据包
func (d *DeepSeek) processPacket(packet pipeline.Packet) {
	switch data := packet.Data.(type) {
	case string:
		d.mu.Lock()
		d.metrics.TurnStartTs = time.Now().UnixMilli()
		d.metrics.TurnEndTs = 0
		d.firstTokenLatencyMs = 0
		d.totalLatencyMs = 0
		d.mu.Unlock()

		logger.Info("**%s** Process turn_seq=%d, cur_turn_seq=%d, text: %s", d.GetName(), packet.TurnSeq, d.GetCurTurnSeq(), data)

		if d.streaming {
			go d.processTextStreaming(data, packet)
		} else {
			d.processTextNonStreaming(data, packet)
		}
	default:
		d.HandleUnsupportedData(packet.Data)
	}
}

// processTextStreaming 处理流式文本请求
func (d *DeepSeek) processTextStreaming(text string, packet pipeline.Packet) {
	d.mu.Lock()
	// 如果添加新消息后会超过最大限制，移除最早的消息
	for len(d.messages) >= d.maxMessages {
		d.messages = d.messages[1:]
	}
	// 添加用户消息
	d.messages = append(d.messages, openai.UserMessage(text))
	messagesCopy := make([]openai.ChatCompletionMessageParamUnion, len(d.messages))
	copy(messagesCopy, d.messages)
	modelCopy := d.model
	d.mu.Unlock()

	// 记录开始时间
	startTime := time.Now()
	var firstTokenTime time.Time

	// 在单独的goroutine中处理流式响应，避免阻塞processLoop
	go func() {
		// 创建上下文，使其可以被取消
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// 创建流式聊天完成请求
		stream := d.client.NewStreaming(
			ctx,
			openai.ChatCompletionNewParams{
				Messages: openai.F(messagesCopy),
				Model:    openai.F(modelCopy),
			},
		)
		defer stream.Close()

		// 使用累加器收集完整响应
		acc := openai.ChatCompletionAccumulator{}
		var fullResponse string
		var chunkCount int
		isFirstToken := true
		padding := ""

		// 处理流式响应
		for stream.Next() {
			// 记录首个token的时间
			if isFirstToken {
				firstTokenTime = time.Now()
				firstTokenLatency := firstTokenTime.Sub(startTime)
				d.mu.Lock()
				d.firstTokenLatencyMs = firstTokenLatency.Milliseconds()
				d.mu.Unlock()
				logger.Info("[TurnSeq: %d] **%s** First token latency: %v", packet.TurnSeq, d.GetName(), firstTokenLatency)
				isFirstToken = false
				padding = "。"
			}

			// 检查当前turn sequence是否已经改变，如果改变则停止处理
			if packet.TurnSeq < d.GetCurTurnSeq() {
				logger.Info("**%s** Turn sequence changed from %d to %d, stopping stream", d.GetName(), packet.TurnSeq, d.GetCurTurnSeq())
				return
			}

			chunk := stream.Current()
			acc.AddChunk(chunk)
			chunkCount++

			// 发送内容更新
			if content, ok := acc.JustFinishedContent(); ok {
				logger.Debug("**%s** Streaming content: %s", d.GetName(), content)
				d.ForwardPacket(pipeline.Packet{
					Data:    content,
					Seq:     d.GetSeq(),
					TurnSeq: packet.TurnSeq,
				})
				fullResponse += content
			}

			// 如果当前块有内容，也发送
			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				logger.Debug("**%s** Streaming content: %s", d.GetName(), chunk.Choices[0].Delta.Content+padding)
				d.ForwardPacket(pipeline.Packet{
					Data:    chunk.Choices[0].Delta.Content + padding,
					Seq:     d.GetSeq(),
					TurnSeq: packet.TurnSeq,
				})
				fullResponse += chunk.Choices[0].Delta.Content
				padding = ""
			}
		}

		// 计算总耗时
		totalDuration := time.Since(startTime)
		d.mu.Lock()
		d.totalLatencyMs = totalDuration.Milliseconds()
		d.metrics.TurnEndTs = time.Now().UnixMilli()
		d.mu.Unlock()

		logger.Info("[TurnSeq: %d] **%s** Total streaming duration: %v (first token: %v)",
			packet.TurnSeq, d.GetName(), totalDuration,
			time.Duration(d.firstTokenLatencyMs)*time.Millisecond)

		if err := stream.Err(); err != nil {
			logger.Error("Error in stream: %v", err)
			d.UpdateErrorStatus(err)
			return
		}

		d.mu.Lock()
		// 如果添加助手回复会超过限制，先移除最早的消息
		if len(d.messages) >= d.maxMessages {
			d.messages = d.messages[1:]
		}
		// 将完整的回复添加到消息历史
		d.messages = append(d.messages, openai.AssistantMessage(fullResponse))
		d.mu.Unlock()
	}()

	// 立即返回，不阻塞processLoop
}

// processTextNonStreaming 处理非流式文本请求
func (d *DeepSeek) processTextNonStreaming(text string, packet pipeline.Packet) {
	d.mu.Lock()
	// 如果添加新消息后会超过最大限制，移除最早的消息
	for len(d.messages) >= d.maxMessages {
		d.messages = d.messages[1:]
	}
	// 添加用户消息
	d.messages = append(d.messages, openai.UserMessage(text))
	d.mu.Unlock()

	// 创建聊天完成请求
	resp, err := d.client.New(
		context.Background(),
		openai.ChatCompletionNewParams{
			Messages: openai.F(d.messages),
			Model:    openai.F(d.model),
		},
	)

	if err != nil {
		logger.Error("Error creating chat completion: %v", err)
		d.UpdateErrorStatus(err)
		return
	}

	// 获取助手的回复
	assistantMessage := resp.Choices[0].Message.Content

	d.mu.Lock()
	// 如果添加助手回复会超过限制，先移除最早的消息
	if len(d.messages) >= d.maxMessages {
		d.messages = d.messages[1:]
	}
	// 将回复添加到消息历史
	d.messages = append(d.messages, openai.AssistantMessage(assistantMessage))
	d.mu.Unlock()

	d.metrics.TurnEndTs = time.Now().UnixMilli()

	// 发送回复
	previousMetrics := packet.TurnMetricStat
	if packet.TurnMetricStat != nil {
		previousMetrics[fmt.Sprintf("%s_%d", d.GetName(), d.GetSeq())] = d.metrics
		packet.TurnMetricKeys = append(packet.TurnMetricKeys, fmt.Sprintf("%s_%d", d.GetName(), d.GetSeq()))
	}

	d.ForwardPacket(pipeline.Packet{
		Data:           assistantMessage,
		Seq:            d.GetSeq(),
		TurnSeq:        d.GetCurTurnSeq(),
		TurnMetricStat: previousMetrics,
		TurnMetricKeys: packet.TurnMetricKeys,
	})
}

// GetID 实现 Component 接口
func (d *DeepSeek) GetID() interface{} {
	return d.GetSeq()
}

// Stop 实现 Component 接口，扩展基础组件的 Stop 方法
func (d *DeepSeek) Stop() {
	d.BaseComponent.Stop()
	// 清理状态
	d.mu.Lock()
	d.messages = make([]openai.ChatCompletionMessageParamUnion, 0)
	d.mu.Unlock()
}

// 为了向后兼容，保留这些方法
func (d *DeepSeek) Process(packet pipeline.Packet) {
	select {
	case d.GetInputChan() <- packet:
	default:
		logger.Error("DeepSeek: input channel full, dropping packet")
	}
}

func (d *DeepSeek) SetInput() {
	inChan := make(chan pipeline.Packet, 100)
	d.SetInputChan(inChan)
}

func (d *DeepSeek) SetOutput(output func(pipeline.Packet)) {
	go func() {
		for packet := range d.GetOutputChan() {
			if output != nil {
				output(packet)
			}
		}
	}()
}

// ClearHistory 清除对话历史
func (d *DeepSeek) ClearHistory() {
	d.mu.Lock()
	d.messages = make([]openai.ChatCompletionMessageParamUnion, 0)
	d.mu.Unlock()
}

// SetMaxMessages 设置保留的最大消息数量
func (d *DeepSeek) SetMaxMessages(max int) {
	d.maxMessages = max
}

// SetModel 设置使用的模型
func (d *DeepSeek) SetModel(model string) {
	d.model = model
}

// SetStreaming 设置是否使用流式处理
func (d *DeepSeek) SetStreaming(enabled bool) {
	d.streaming = enabled
}

// GetHealth 实现 Component 接口
func (d *DeepSeek) GetHealth() pipeline.ComponentHealth {
	return d.BaseComponent.GetHealth()
}

// UpdateHealth 实现 Component 接口
func (d *DeepSeek) UpdateHealth(health pipeline.ComponentHealth) {
	d.BaseComponent.UpdateHealth(health)
}
