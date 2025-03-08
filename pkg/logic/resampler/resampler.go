package resampler

import (
	"bytes"
	"fmt"
	"io"
	"streamlink/pkg/logger"
	"streamlink/pkg/logic/codec"
	"streamlink/pkg/logic/pipeline"
	"time"

	"github.com/zaf/resample"
)

// Resampler 结构体 (实现 Component 接口)
type Resampler struct {
	*pipeline.BaseComponent
	resampler     *resample.Resampler
	buffer        *bytes.Buffer
	inputBuffer   []int16 // 用于累积输入样本的缓冲区
	channelsIn    int
	channelsOut   int
	sampleRateOut int
	sampleRateIn  int
	metrics       pipeline.TurnMetrics
	minSamples    int // 重采样所需的最小样本数
}

func NewResampler(sampleRateIn, sampleRateOut, channelsIn, channelsOut int) (*Resampler, error) {
	// 创建 buffer
	buffer := new(bytes.Buffer)

	// 创建 resampler
	resampler, err := resample.New(
		buffer,
		float64(sampleRateIn),
		float64(sampleRateOut),
		channelsOut,
		resample.I16,
		resample.HighQ, // 使用高质量重采样
	)
	if err != nil {
		return nil, err
	}

	// 计算最小样本数 - 基于输入采样率，确保有足够的数据进行重采样
	// 这里使用输入采样率的 20ms 作为最小处理单位
	minSamples := (sampleRateIn * channelsIn * 20) / 1000

	name := fmt.Sprintf("Resampler_%dHz_%dCh->%dHz_%dCh", sampleRateIn, channelsIn, sampleRateOut, channelsOut)
	r := &Resampler{
		BaseComponent: pipeline.NewBaseComponent(name, 100),
		resampler:     resampler,
		buffer:        buffer,
		inputBuffer:   make([]int16, 0),
		channelsIn:    channelsIn,
		channelsOut:   channelsOut,
		sampleRateOut: sampleRateOut,
		sampleRateIn:  sampleRateIn,
		minSamples:    minSamples,
	}

	// 设置处理函数
	r.BaseComponent.SetProcess(r.processPacket)
	r.RegisterCommandHandler(pipeline.PacketCommandInterrupt, r.handleInterrupt)

	return r, nil
}

func (r *Resampler) handleInterrupt(packet pipeline.Packet) {
	// log.Printf("**%s** Received interrupt command for turn %d", r.GetName(), packet.TurnSeq)
	r.SetCurTurnSeq(packet.TurnSeq)

	r.ForwardPacket(packet)
}

// processPacket 处理输入的数据包
func (r *Resampler) processPacket(packet pipeline.Packet) {
	if packet.TurnSeq < r.GetCurTurnSeq() {
		logger.Info("**%s** Skip turn_seq=%d , text: %s", r.GetName(), packet.TurnSeq, packet.Data)
		r.inputBuffer = make([]int16, 0)
		return
	}
	r.metrics.TurnStartTs = time.Now().UnixMilli()
	r.metrics.TurnEndTs = 0

	var processData []int16

	switch data := packet.Data.(type) {
	case codec.AudioPacket:
		bytesData := data.Payload()
		processData = make([]int16, len(bytesData)/2)
		for i := 0; i < len(bytesData); i += 2 {
			processData[i/2] = int16(bytesData[i]) | (int16(bytesData[i+1]) << 8)
		}
	case []int16:
		processData = data
	case []byte:
		// 将 []byte 转换为 []int16
		processData = make([]int16, len(data)/2)
		for i := 0; i < len(data); i += 2 {
			processData[i/2] = int16(data[i]) | (int16(data[i+1]) << 8)
		}
	default:
		r.HandleUnsupportedData(packet.Data)
		return
	}

	if len(processData) == 0 {
		logger.Warn("**%s** Warning: received empty input data", r.GetName())
		r.UpdateErrorStatus(fmt.Errorf("received empty input data"))
		return
	}

	// 将新数据添加到输入缓冲区
	r.inputBuffer = append(r.inputBuffer, processData...)

	// 如果累积的样本数不够，等待更多数据
	if len(r.inputBuffer) < r.minSamples {
		return
	}

	// 计算可以处理的样本数（必须是minSamples的整数倍）
	processableSamples := (len(r.inputBuffer) / r.minSamples) * r.minSamples

	// 获取要处理的数据
	samplesForProcessing := r.inputBuffer[:processableSamples]

	// 保存剩余的数据
	remainingSamples := r.inputBuffer[processableSamples:]

	// 处理输入缓冲区中的数据
	var processedData []int16

	// 如果是立体声转单声道，先做声道转换
	if r.channelsIn > r.channelsOut {
		// 立体声转单声道
		processedData = make([]int16, len(samplesForProcessing)/2)
		for i := 0; i < len(samplesForProcessing); i += r.channelsIn {
			if i+1 >= len(samplesForProcessing) {
				break
			}
			left := int32(samplesForProcessing[i])
			right := int32(samplesForProcessing[i+1])

			leftF := float64(left) / 32768.0
			rightF := float64(right) / 32768.0
			mixed := (leftF + rightF) * 0.5 * 32768.0

			if mixed > 32767.0 {
				mixed = 32767.0
			} else if mixed < -32768.0 {
				mixed = -32768.0
			}

			processedData[i/2] = int16(mixed)
		}
	} else if r.channelsIn < r.channelsOut {
		processedData = make([]int16, len(samplesForProcessing)*2)
		for i := 0; i < len(samplesForProcessing); i++ {
			processedData[i*2] = samplesForProcessing[i]
			processedData[i*2+1] = samplesForProcessing[i]
		}
	} else {
		processedData = make([]int16, len(samplesForProcessing))
		copy(processedData, samplesForProcessing)
	}

	// Convert []int16 to []byte
	audioBytes := make([]byte, len(processedData)*2)
	for i, sample := range processedData {
		audioBytes[i*2] = byte(sample)
		audioBytes[i*2+1] = byte(sample >> 8)
	}

	r.buffer.Reset()
	// 写入数据
	_, err := r.resampler.Write(audioBytes)
	if err != nil {
		logger.Error("**%s** Resampling failed: %v", r.GetName(), err)
		r.UpdateErrorStatus(err)
		return
	}

	// 读取重采样后的数据
	resampledBytes := make([]byte, r.buffer.Len())
	n, err := r.buffer.Read(resampledBytes)
	if err != nil && err != io.EOF {
		logger.Error("**%s** Failed to read resampled data: %v", r.GetName(), err)
		r.UpdateErrorStatus(err)
		return
	}
	resampledBytes = resampledBytes[:n]

	// Convert []byte back to []int16
	currentData := make([]int16, len(resampledBytes)/2)
	for i := 0; i < len(resampledBytes)/2; i++ {
		currentData[i] = int16(resampledBytes[i*2]) | (int16(resampledBytes[i*2+1]) << 8)
	}

	// 更新输入缓冲区为剩余的样本
	r.inputBuffer = make([]int16, len(remainingSamples))
	copy(r.inputBuffer, remainingSamples)

	// 发送重采样后的数据
	r.metrics.TurnEndTs = time.Now().UnixMilli()

	previousMetrics := packet.TurnMetricStat
	if previousMetrics != nil {
		packet.TurnMetricKeys = append(packet.TurnMetricKeys, fmt.Sprintf("%s_%d", r.GetName(), r.GetSeq()))
		previousMetrics[fmt.Sprintf("%s_%d", r.GetName(), r.GetSeq())] = r.metrics
	}

	r.ForwardPacket(pipeline.Packet{
		Data:           currentData,
		Seq:            r.GetSeq(),
		TurnSeq:        r.GetCurTurnSeq(),
		TurnMetricStat: previousMetrics,
		TurnMetricKeys: packet.TurnMetricKeys,
	})
}

// GetID 实现 Component 接口
func (r *Resampler) GetID() interface{} {
	return r.GetSeq()
}

// Stop 实现 Component 接口，扩展基础组件的 Stop 方法
func (r *Resampler) Stop() {
	r.BaseComponent.Stop()
}

// 为了向后兼容，保留这些方法
func (r *Resampler) Process(packet pipeline.Packet) {
	select {
	case r.GetInputChan() <- packet:
	default:
		logger.Error("**%s** Input channel full, dropping packet", r.GetName())
	}
}

func (r *Resampler) SetOutput(output func(pipeline.Packet)) {
	outChan := make(chan pipeline.Packet, 100)
	r.SetOutputChan(outChan)
	go func() {
		for packet := range r.GetOutputChan() {
			if output != nil {
				output(packet)
			}
		}
	}()
}

// Start 实现 Component 接口
func (r *Resampler) Start() error {
	r.BaseComponent.Start()
	return nil
}

// GetHealth 实现 Component 接口
func (r *Resampler) GetHealth() pipeline.ComponentHealth {
	return r.BaseComponent.GetHealth()
}

// UpdateHealth 实现 Component 接口
func (r *Resampler) UpdateHealth(health pipeline.ComponentHealth) {
	r.BaseComponent.UpdateHealth(health)
}
