package flux

import (
	"fmt"
	"io"
	"log"
	"time"
	"voiceagent/pkg/logic/codec"
	"voiceagent/pkg/logic/pipeline"

	"github.com/pion/webrtc/v4"
)

// WebRTCSource 实现 audio.Source 接口
type WebRTCSource struct {
	*pipeline.BaseComponent
	track *webrtc.TrackRemote
	seq   int
}

// NewWebRTCSource 创建一个新的 WebRTC 音频源
func NewWebRTCSource(track *webrtc.TrackRemote) *WebRTCSource {
	s := &WebRTCSource{
		BaseComponent: pipeline.NewBaseComponent("WebRTCSource", 100),
		track:         track,
		seq:           0,
	}
	return s
}

// SetTrack 设置远程音轨
func (s *WebRTCSource) SetTrack(track *webrtc.TrackRemote) {
	s.track = track
}

// Start 启动音频源
func (s *WebRTCSource) Start() error {
	if s.track == nil {
		return fmt.Errorf("track not set")
	}

	// 更新组件状态
	s.UpdateHealth(pipeline.ComponentHealth{
		State:          pipeline.ComponentStateRunning,
		LastUpdateTime: time.Now(),
	})

	log.Printf("Started src component **%s**", s.GetName())
	// 启动 RTP 包读取循环
	go s.readLoop()

	return nil
}

func (s *WebRTCSource) readLoop() {
	defer func() {
		s.UpdateHealth(pipeline.ComponentHealth{
			State:          pipeline.ComponentStateStopped,
			LastUpdateTime: time.Now(),
		})
	}()

	for {
		select {
		case <-s.BaseComponent.GetStopCh():
			return
		default:
			rtpPacket, _, err := s.track.ReadRTP()
			if err != nil {
				if err == io.EOF {
					return
				}
				log.Printf("**%s** Failed to read RTP packet: %v", s.GetName(), err)
				s.UpdateErrorStatus(err)
				continue
			}

			// 将 RTP 包转换为 AudioPacket
			audioPacket := codec.NewRTPAudioPacket(rtpPacket.Payload, rtpPacket.Timestamp)

			// 发送数据包
			s.SendPacket(audioPacket, s)

			// 更新健康状态
			health := s.GetHealth()
			health.ProcessedCount++
			health.LastUpdateTime = time.Now()
			s.UpdateHealth(health)
		}
	}
}

// Stop 实现 Component 接口
func (s *WebRTCSource) Stop() {
	s.BaseComponent.Stop()
}

// GetID 实现 Component 接口
func (s *WebRTCSource) GetID() interface{} {
	return s.GetName()
}

// Process 实现 Component 接口
func (s *WebRTCSource) Process(packet pipeline.Packet) {
	// WebRTCSource 不需要处理输入包
}

// SetOutput 实现 Component 接口
func (s *WebRTCSource) SetOutput(output func(pipeline.Packet)) {
	// WebRTCSource 直接使用 BaseComponent 的输出通道
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

// GetInputChan implements pipeline.Component interface
func (s *WebRTCSource) GetInputChan() chan pipeline.Packet {
	return s.BaseComponent.GetInputChan()
}

// GetOutputChan implements pipeline.Component interface
func (s *WebRTCSource) GetOutputChan() chan pipeline.Packet {
	return s.BaseComponent.GetOutputChan()
}

// SetInputChan implements pipeline.Component interface
func (s *WebRTCSource) SetInputChan(ch chan pipeline.Packet) {
	s.BaseComponent.SetInputChan(ch)
}

// SetOutputChan implements pipeline.Component interface
func (s *WebRTCSource) SetOutputChan(ch chan pipeline.Packet) {
	s.BaseComponent.SetOutputChan(ch)
}

// GetHealth implements pipeline.Component interface
func (s *WebRTCSource) GetHealth() pipeline.ComponentHealth {
	return s.BaseComponent.GetHealth()
}

// UpdateHealth implements pipeline.Component interface
func (s *WebRTCSource) UpdateHealth(health pipeline.ComponentHealth) {
	s.BaseComponent.UpdateHealth(health)
}
