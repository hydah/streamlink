package codec

import (
	"fmt"
	"log"
	"strings"

	"voiceagent/pkg/logic/pipeline"

	"github.com/hraban/opus"
)

// OpusDecoder 结构体 (实现 Component 接口)
type OpusDecoder struct {
	*pipeline.BaseComponent
	opusDecoder  *opus.Decoder
	sampleRateIn int
	channelsIn   int
}

func NewOpusDecoder(sampleRateIn, channelsIn int) (*OpusDecoder, error) {
	// 创建 Opus 解码器
	opusDecoder, err := opus.NewDecoder(sampleRateIn, channelsIn)
	if err != nil {
		return nil, fmt.Errorf("failed to create Opus decoder: %v", err)
	}

	decoder := &OpusDecoder{
		BaseComponent: pipeline.NewBaseComponent("OpusDecoder", 100),
		opusDecoder:   opusDecoder,
		sampleRateIn:  sampleRateIn,
		channelsIn:    channelsIn,
	}

	// 设置处理函数
	decoder.BaseComponent.SetProcess(decoder.processPacket)
	decoder.RegisterCommandHandler(pipeline.PacketCommandInterrupt, decoder.handleInterrupt)

	return decoder, nil
}

func (d *OpusDecoder) handleInterrupt(packet pipeline.Packet) {
	log.Printf("**%s** Received interrupt command for turn %d", d.GetName(), packet.TurnSeq)
	d.IncrTurnSeq()

	d.ForwardPacket(packet)
}

// processPacket 处理输入的数据包
func (d *OpusDecoder) processPacket(packet pipeline.Packet) {
	audioPacket, ok := packet.Data.(AudioPacket)
	if !ok {
		d.HandleUnsupportedData(packet.Data)
		return
	}

	// 处理音频包
	d.processAudio(audioPacket, packet)
}

// processAudio 处理音频包
func (d *OpusDecoder) processAudio(audioPacket AudioPacket, packet pipeline.Packet) {
	// 解码 Opus 数据为 PCM
	pcmData := make([]int16, 960*d.channelsIn)
	n, err := d.opusDecoder.Decode(audioPacket.Payload(), pcmData)
	if err != nil {
		if strings.Contains(err.Error(), "no data supplied") {
			return
		}
		log.Printf("**%s** Decoding failed: %v", d.GetName(), err)
		d.UpdateErrorStatus(err)
		return
	}

	// 将解码后的 PCM 数据传递给下一个组件
	d.SendPacket(pcmData[:n*d.channelsIn], d)
}

// GetID 实现 Component 接口
func (d *OpusDecoder) GetID() interface{} {
	return d.GetSeq()
}

// 为了向后兼容，保留这些方法
func (d *OpusDecoder) Process(packet pipeline.Packet) {
	select {
	case d.GetInputChan() <- packet:
	default:
		log.Printf("**%s** Input channel full, dropping packet", d.GetName())
	}
}

func (d *OpusDecoder) SetOutput(output func(pipeline.Packet)) {
	outChan := make(chan pipeline.Packet, 100)
	d.SetOutputChan(outChan)
	go func() {
		for packet := range outChan {
			if output != nil {
				output(packet)
			}
		}
	}()
}

// Start 实现 Component 接口
func (d *OpusDecoder) Start() error {
	d.BaseComponent.Start()
	return nil
}

// GetHealth 实现 Component 接口
func (d *OpusDecoder) GetHealth() pipeline.ComponentHealth {
	return d.BaseComponent.GetHealth()
}

// UpdateHealth 实现 Component 接口
func (d *OpusDecoder) UpdateHealth(health pipeline.ComponentHealth) {
	d.BaseComponent.UpdateHealth(health)
}
