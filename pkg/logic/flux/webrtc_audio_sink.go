package flux

import (
	"fmt"
	"log"
	"streamlink/pkg/logic/codec"
	"streamlink/pkg/logic/pipeline"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// WebRTCSink 结构体 (实现 Component 接口)
type WebRTCSink struct {
	*pipeline.BaseComponent
	track *webrtc.TrackLocalStaticSample
	seq   int
}

func NewWebRTCSink(track *webrtc.TrackLocalStaticSample) *WebRTCSink {
	sink := &WebRTCSink{
		BaseComponent: pipeline.NewBaseComponent("WebRTCSink", 5*60*50),
		track:         track,
		seq:           0,
	}

	// 设置处理函数
	sink.BaseComponent.SetProcess(sink.processPacket)
	sink.RegisterCommandHandler(pipeline.PacketCommandInterrupt, sink.handleInterrupt)

	return sink
}

func (s *WebRTCSink) handleInterrupt(packet pipeline.Packet) {
	log.Printf("**%s** Received interrupt command for turn %d", s.GetName(), packet.TurnSeq)
	s.SetCurTurnSeq(packet.TurnSeq)
}

// processPacket 处理输入的数据包
func (s *WebRTCSink) processPacket(packet pipeline.Packet) {
	switch data := packet.Data.(type) {
	case codec.AudioPacket:
		// 写入音频数据
		if err := s.track.WriteSample(media.Sample{
			Data:     data.Payload(),
			Duration: time.Millisecond * 20,
		}); err != nil {
			log.Printf("**%s** Failed to write sample: %v", s.GetName(), err)
			s.UpdateErrorStatus(err)
		}
	default:
		s.HandleUnsupportedData(packet.Data)
	}
}

// GetID 实现 Component 接口
func (s *WebRTCSink) GetID() interface{} {
	return s.GetSeq()
}

// Stop 实现 Component 接口
func (s *WebRTCSink) Stop() {
	s.BaseComponent.Stop()
}

// Start 实现 Component 接口
func (s *WebRTCSink) Start() error {
	if s.track == nil {
		return fmt.Errorf("track not set")
	}

	// 更新组件状态
	s.UpdateHealth(pipeline.ComponentHealth{
		State:          pipeline.ComponentStateRunning,
		LastUpdateTime: time.Now(),
	})

	return s.BaseComponent.Start()
}

// GetInputChan implements pipeline.Component interface
func (s *WebRTCSink) GetInputChan() chan pipeline.Packet {
	return s.BaseComponent.GetInputChan()
}

// GetOutputChan implements pipeline.Component interface
func (s *WebRTCSink) GetOutputChan() chan pipeline.Packet {
	return s.BaseComponent.GetOutputChan()
}

// SetInputChan implements pipeline.Component interface
func (s *WebRTCSink) SetInputChan(ch chan pipeline.Packet) {
	s.BaseComponent.SetInputChan(ch)
}

// SetOutputChan implements pipeline.Component interface
func (s *WebRTCSink) SetOutputChan(ch chan pipeline.Packet) {
	s.BaseComponent.SetOutputChan(ch)
}

// GetHealth implements pipeline.Component interface
func (s *WebRTCSink) GetHealth() pipeline.ComponentHealth {
	return s.BaseComponent.GetHealth()
}

// UpdateHealth implements pipeline.Component interface
func (s *WebRTCSink) UpdateHealth(health pipeline.ComponentHealth) {
	s.BaseComponent.UpdateHealth(health)
}

// Process 实现 audio.Sink 接口
func (s *WebRTCSink) Process(packet pipeline.Packet) {
	select {
	case s.GetInputChan() <- packet:
	default:
		log.Printf("**%s** Input channel full, dropping packet", s.GetName())
	}
}

// SetOutput 实现 audio.Sink 接口
func (s *WebRTCSink) SetOutput(output func(pipeline.Packet)) {
	outChan := make(chan pipeline.Packet, 100)
	s.SetOutputChan(outChan)
	go func() {
		for packet := range outChan {
			if output != nil {
				output(packet)
			}
		}
	}()
}
