package codec

import (
	"fmt"
	"streamlink/pkg/logger"
	"streamlink/pkg/logic/pipeline"
	"time"

	"github.com/hraban/opus"
)

// OpusEncoder 结构体 (实现 Component 接口)
type OpusEncoder struct {
	*pipeline.BaseComponent
	opusEncoder *opus.Encoder
	frameSize   int                // 每帧的采样点数
	dataBuffer  []int16            // PCM 数据缓冲区
	encodeChan  chan encodeRequest // 新增：编码请求通道
	metrics     pipeline.TurnMetrics
}

// 新增：编码请求结构
type encodeRequest struct {
	data    []int16
	turnSeq int
}

func NewOpusEncoder(sampleRate, channels int) (*OpusEncoder, error) {
	// 创建 Opus 编码器
	opusEncoder, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		return nil, fmt.Errorf("failed to create Opus encoder: %v", err)
	}

	encoder := &OpusEncoder{
		BaseComponent: pipeline.NewBaseComponent("OpusEncoder", 4000),
		opusEncoder:   opusEncoder,
		frameSize:     960 * channels, // 每帧 20ms，对于 48kHz 采样率，就是 960 个采样点
		dataBuffer:    make([]int16, 0),
		encodeChan:    make(chan encodeRequest, 100),
	}

	// 注册打断指令处理函数
	encoder.RegisterCommandHandler(pipeline.PacketCommandInterrupt, encoder.handleInterrupt)

	// 设置处理函数
	encoder.BaseComponent.SetProcess(encoder.processPacket)

	return encoder, nil
}

// handleInterrupt 处理打断指令
func (e *OpusEncoder) handleInterrupt(packet pipeline.Packet) {
	logger.Info("**%s** Received interrupt command for turn %d", e.GetName(), packet.TurnSeq)
	e.SetCurTurnSeq(packet.TurnSeq)

	// 立即转发打断指令并清空缓冲区
	e.ForwardPacket(packet)
	e.dataBuffer = e.dataBuffer[:0]
}

// processPacket 处理输入的数据包
func (e *OpusEncoder) processPacket(packet pipeline.Packet) {
	e.metrics.TurnStartTs = time.Now().UnixMilli()
	e.metrics.TurnEndTs = 0

	// 如果是新的轮次，更新 curTurnSeq 并清空缓冲区
	if packet.TurnSeq < e.GetCurTurnSeq() {
		logger.Info("**%s** cur turn: %d, drop old turn packet(seq: %d)", e.GetName(), e.GetCurTurnSeq(), packet.TurnSeq)
		e.dataBuffer = e.dataBuffer[:0]
		return
	}

	data, ok := packet.Data.([]int16)
	if !ok {
		e.HandleUnsupportedData(packet.Data)
		return
	}

	// 将新数据添加到缓冲区
	e.dataBuffer = append(e.dataBuffer, data...)

	// 发送编码请求
	select {
	case e.encodeChan <- encodeRequest{data: e.dataBuffer, turnSeq: packet.TurnSeq}:
		// print turn_metric_stat
		for _, k := range packet.TurnMetricKeys {
			logger.Info("turn_metric_stat: %s, %d, %d, latency: %d ms", k,
				packet.TurnMetricStat[k].TurnStartTs,
				packet.TurnMetricStat[k].TurnEndTs,
				packet.TurnMetricStat[k].TurnEndTs-packet.TurnMetricStat[k].TurnStartTs)
		}
		// log.Printf("turn latency(asr start): %d ms",
		// 	e.metrics.TurnStartTs-packet.TurnMetricStat[packet.TurnMetricKeys[0]].TurnStartTs)
		// log.Printf("turn latency(asr end): %d ms",
		// 	e.metrics.TurnStartTs-packet.TurnMetricStat[packet.TurnMetricKeys[0]].TurnEndTs)

		e.dataBuffer = make([]int16, 0) // 清空缓冲区
	default:
		logger.Error("**%s** Encode channel full, dropping data", e.GetName())
	}
}

// encodeLoop 在单独的 goroutine 中处理编码
func (e *OpusEncoder) encodeLoop() {
	for req := range e.encodeChan {
		data := req.data
		for len(data) >= e.frameSize {
			if req.turnSeq < e.GetCurTurnSeq() {
				logger.Debug("**%s** encode loop drop old turn packet(seq: %d)", e.GetName(), req.turnSeq)
				break
			}

			// 获取一帧数据
			frame := data[:e.frameSize]

			// 分配足够大的缓冲区用于 Opus 编码
			opusFrame := make([]byte, 2048)
			n, err := e.opusEncoder.Encode(frame, opusFrame)
			if err != nil {
				logger.Error("**%s** Opus encoding failed: %v", e.GetName(), err)
				e.UpdateErrorStatus(err)
				break
			}

			// 创建 AudioPacket
			audioPacket := NewRTPAudioPacket(opusFrame[:n], uint32(time.Now().UnixNano()/1e6))

			// 发送编码后的数据
			e.SendPacket(audioPacket, e)

			// 更新缓冲区
			data = data[e.frameSize:]

			// 更新健康状态
			health := e.GetHealth()
			health.ProcessedCount++
			health.LastUpdateTime = time.Now()
			e.UpdateHealth(health)

			time.Sleep(18 * time.Millisecond)
		}
	}
}

// SetFrameSize 设置每帧的采样点数
func (e *OpusEncoder) SetFrameSize(size int) {
	e.frameSize = size
}

// Stop 实现 Component 接口
func (e *OpusEncoder) Stop() {
	e.BaseComponent.Stop()
	close(e.encodeChan)
	e.opusEncoder = nil
	e.dataBuffer = nil
}

// GetID 实现 Component 接口
func (e *OpusEncoder) GetID() interface{} {
	return e.GetSeq()
}

// 为了向后兼容，保留这些方法
func (e *OpusEncoder) Process(packet pipeline.Packet) {
	select {
	case e.GetInputChan() <- packet:
	default:
		logger.Error("**%s** Input channel full, dropping packet", e.GetName())
	}
}

func (e *OpusEncoder) SetOutput(output func(pipeline.Packet)) {
	outChan := make(chan pipeline.Packet, 100)
	e.SetOutputChan(outChan)
	go func() {
		for packet := range e.GetOutputChan() {
			if output != nil {
				output(packet)
			}
		}
	}()
}

// Start 实现 Component 接口
func (e *OpusEncoder) Start() error {
	// 启动编码 goroutine
	go e.encodeLoop()
	return e.BaseComponent.Start()
}

// GetHealth 实现 Component 接口
func (e *OpusEncoder) GetHealth() pipeline.ComponentHealth {
	return e.BaseComponent.GetHealth()
}

// UpdateHealth 实现 Component 接口
func (e *OpusEncoder) UpdateHealth(health pipeline.ComponentHealth) {
	e.BaseComponent.UpdateHealth(health)
}
