package connection

import (
	"fmt"
	"log"
	"time"

	"voiceagent/internal/config"
	"voiceagent/pkg/logic/codec"
	"voiceagent/pkg/logic/flux"
	"voiceagent/pkg/logic/pipeline"
	"voiceagent/pkg/logic/resampler"
	"voiceagent/pkg/server/agent"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

// webRTCAudioProcessor WebRTC音频处理器实现
type webRTCAudioProcessor struct {
	inputSampleRate  uint32
	outputSampleRate uint32
	inputChannels    uint16
	outputChannels   uint16
}

// ProcessInput 处理输入音频：Opus解码 -> 重采样
func (p *webRTCAudioProcessor) ProcessInput(source pipeline.Component) flux.ProcessingChain {
	// 创建 Opus 解码器
	decoder, err := codec.NewOpusDecoder(int(p.inputSampleRate), int(p.inputChannels))
	if err != nil {
		return flux.ProcessingChain{First: nil, Last: nil, All: []pipeline.Component{}}
	}

	// 创建重采样器 (48kHz,双声道 -> 16kHz,单声道)
	downsampler, err := resampler.NewResampler(
		int(p.inputSampleRate),
		16000,
		int(p.inputChannels),
		1,
	)
	if err != nil {
		return flux.ProcessingChain{First: nil, Last: nil, All: []pipeline.Component{}}
	}

	source.(interface{ SetIgnoreTurn(ignore bool) }).SetIgnoreTurn(true)
	decoder.SetIgnoreTurn(true)
	downsampler.SetIgnoreTurn(true)

	// 返回处理链，不做连接
	return flux.ProcessingChain{
		First: decoder,
		Last:  downsampler,
		All:   []pipeline.Component{decoder, downsampler},
	}
}

// ProcessOutput 处理输出音频：重采样 -> Opus编码
func (p *webRTCAudioProcessor) ProcessOutput(sink pipeline.Component) flux.ProcessingChain {
	// 创建重采样器 (16kHz,单声道 -> 48kHz,单声道)
	upsampler, err := resampler.NewResampler(
		16000,
		int(p.outputSampleRate),
		1,
		int(p.outputChannels),
	)
	if err != nil {
		return flux.ProcessingChain{First: sink, Last: sink, All: []pipeline.Component{sink}}
	}

	// 创建 Opus 编码器
	encoder, err := codec.NewOpusEncoder(int(p.outputSampleRate), int(p.outputChannels))
	if err != nil {
		return flux.ProcessingChain{First: sink, Last: sink, All: []pipeline.Component{sink}}
	}

	// 返回处理链，不做连接
	return flux.ProcessingChain{
		First: upsampler,
		Last:  sink,
		All:   []pipeline.Component{upsampler, encoder, sink},
	}
}

type WebRTCConnection struct {
	id              string
	peerConnection  *webrtc.PeerConnection
	config          *config.Config
	localAudioTrack *webrtc.TrackLocalStaticSample
	stopCh          chan struct{}
	source          flux.Source
	sink            flux.Sink
	voiceAgent      *agent.VoiceAgent
}

type WebRTCFactory struct {
	api          *webrtc.API
	webrtcConfig webrtc.Configuration
	udpMux       ice.UDPMux
}

func NewWebRTCFactory(api *webrtc.API, config webrtc.Configuration, udpMux ice.UDPMux) *WebRTCFactory {
	return &WebRTCFactory{api: api, webrtcConfig: config, udpMux: udpMux}
}

func (f *WebRTCFactory) CreateConnection(cfg *config.Config) (Connection, error) {
	peerConnection, err := f.api.NewPeerConnection(f.webrtcConfig)
	if err != nil {
		return nil, err
	}

	conn := &WebRTCConnection{
		id:             fmt.Sprintf("%d", time.Now().UnixNano()),
		peerConnection: peerConnection,
		config:         cfg,
		stopCh:         make(chan struct{}),
	}

	// 添加音频收发器
	transceiver, err := peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionSendrecv,
	})
	if err != nil {
		return nil, err
	}

	// 创建本地音频轨道
	conn.localAudioTrack = transceiver.Sender().Track().(*webrtc.TrackLocalStaticSample)

	// 创建 sink
	conn.sink = flux.NewWebRTCSink(conn.localAudioTrack)

	// 创建 source（但不启动）
	conn.source = flux.NewWebRTCSource(nil)

	// 设置轨道处理回调
	peerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if remoteTrack.Kind() != webrtc.RTPCodecTypeAudio {
			return
		}
		// 设置远程音轨并启动 source
		log.Printf("set remote track")
		conn.source.(interface{ SetTrack(*webrtc.TrackRemote) }).SetTrack(remoteTrack)
		if err := conn.source.Start(); err != nil {
			log.Printf("Failed to start WebRTC source: %v", err)
		}
	})

	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate != nil {
			fmt.Println("Generated ICE candidate:", candidate)
		} else {
			fmt.Println("ICE Candidate gathering finished")
		}
	})

	peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		fmt.Printf("ICE Connection State has changed: %s\n", state.String())
		if state == webrtc.ICEConnectionStateDisconnected || state == webrtc.ICEConnectionStateFailed {
			conn.Stop()
		}
	})

	// 设置回调
	conn.setupCallbacks()

	return conn, nil
}

func (c *WebRTCConnection) setupCallbacks() {
	c.peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		log.Printf("[%s] ICE Connection State changed: %s\n", c.id, state.String())
	})

	c.peerConnection.OnSignalingStateChange(func(state webrtc.SignalingState) {
		log.Printf("[%s] Signaling State changed: %s\n", c.id, state.String())
	})

	c.peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("[%s] Connection State changed: %s\n", c.id, state.String())
	})

	c.peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate != nil {
			log.Println("Local ICE candidate:", candidate.String())
		}
	})

	c.peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
		log.Printf("[%s] New DataChannel: %s\n", c.id, d.Label())
	})
}

func (c *WebRTCConnection) Start() error {
	log.Println("WebRTC Connection start")

	if c.source == nil {
		return fmt.Errorf("no audio source available")
	}

	// 创建 VoiceAgent，传入 source 和 sink
	// 设置 WebRTC 专用的音频处理器
	processor := &webRTCAudioProcessor{
		inputSampleRate:  48000, // WebRTC 标准采样率
		outputSampleRate: 48000,
		inputChannels:    2, // 双声道输入
		outputChannels:   2, // 双声道输出
	}
	c.voiceAgent = agent.NewVoiceAgent(c.config, c.source, c.sink, processor)

	// 启动 VoiceAgent
	if err := c.voiceAgent.Start(); err != nil {
		return err
	}

	return nil
}

// Stop 停止连接
func (c *WebRTCConnection) Stop() {
	select {
	case <-c.stopCh:
		return
	default:
		close(c.stopCh)
		if c.source != nil {
			c.source.Stop()
		}
		if c.sink != nil {
			c.sink.Stop()
		}
		if c.voiceAgent != nil {
			c.voiceAgent.Stop()
		}
		if c.peerConnection != nil {
			c.peerConnection.Close()
		}
	}
}

func (c *WebRTCConnection) GetID() string {
	return c.id
}

// WebRTC 特有的方法
func (c *WebRTCConnection) SetRemoteDescription(offer webrtc.SessionDescription) error {
	return c.peerConnection.SetRemoteDescription(offer)
}

func (c *WebRTCConnection) CreateAnswer() (*webrtc.SessionDescription, error) {
	answer, err := c.peerConnection.CreateAnswer(&webrtc.AnswerOptions{})
	if err != nil {
		return nil, err
	}

	if err = c.peerConnection.SetLocalDescription(answer); err != nil {
		return nil, err
	}

	// 等待 ICE 候选者收集完成
	<-webrtc.GatheringCompletePromise(c.peerConnection)

	return c.peerConnection.LocalDescription(), nil
}
