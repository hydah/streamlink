package tts

import (
	"fmt"
	"log"
	"streamlink/pkg/logic/pipeline"
	"sync"
	"time"
)

// TencentTTS2 实现 Component 接口
type TencentTTS2 struct {
	*pipeline.BaseComponent
	appID       int64
	secretID    string
	secretKey   string
	voiceType   int64
	codec       string
	synthesizer *FlowingSpeechSynthesizer
	listener    *tts2SynthesisListener
	mu          sync.Mutex
	metrics     pipeline.TurnMetrics
}

// NewTencentTTS2 创建一个新的语音合成组件
func NewTencentTTS2(appID int64, secretID, secretKey string, voiceType int64, codec string) *TencentTTS2 {
	t := &TencentTTS2{
		BaseComponent: pipeline.NewBaseComponent("TencentTTS2", 100),
		appID:         appID,
		secretID:      secretID,
		secretKey:     secretKey,
		voiceType:     voiceType,
		codec:         codec,
	}

	// 设置处理函数
	t.BaseComponent.SetProcess(t.processPacket)
	t.RegisterCommandHandler(pipeline.PacketCommandInterrupt, t.handleInterrupt)

	return t
}

// Start 启动语音合成服务
func (t *TencentTTS2) Start() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 创建监听器
	t.listener = &tts2SynthesisListener{
		tts: t,
	}

	// 创建凭证
	credential := &Credential{
		SecretID:  t.secretID,
		SecretKey: t.secretKey,
	}

	// 创建合成器
	t.synthesizer = NewFlowingSpeechSynthesizer(t.appID, credential, t.listener)
	t.synthesizer.SetVoiceType(t.voiceType)
	t.synthesizer.SetCodec(t.codec)
	t.synthesizer.SetSampleRate(16000)
	t.synthesizer.SetVolume(0)
	t.synthesizer.SetSpeed(0)

	// 启动合成器
	if err := t.synthesizer.Start(); err != nil {
		log.Printf("Start synthesizer failed: %v", err)
		return fmt.Errorf("start synthesizer failed: %v", err)
	}

	// 等待就绪
	if !t.synthesizer.WaitReady(5000) {
		t.synthesizer.Stop()
		t.synthesizer = nil
		return fmt.Errorf("wait synthesizer ready timeout")
	}

	// 启动基础组件
	if err := t.BaseComponent.Start(); err != nil {
		t.synthesizer.Stop()
		t.synthesizer = nil
		return fmt.Errorf("start base component failed: %v", err)
	}

	return nil
}

func (t *TencentTTS2) handleInterrupt(packet pipeline.Packet) {
	log.Printf("**%s** Received interrupt command for turn %d", t.GetName(), packet.TurnSeq)
	t.SetCurTurnSeq(packet.TurnSeq)

	t.ForwardPacket(packet)
}

// processPacket 处理输入的数据包
func (t *TencentTTS2) processPacket(packet pipeline.Packet) {
	t.metrics.TurnStartTs = time.Now().UnixMilli()
	t.metrics.TurnEndTs = 0

	switch data := packet.Data.(type) {
	case string:
		log.Printf("**%s** Processing turn_seq=%d , text: %s", t.GetName(), packet.TurnSeq, data)
		t.mu.Lock()
		defer t.mu.Unlock()

		// 检查合成器状态
		if t.synthesizer == nil {
			t.UpdateErrorStatus(fmt.Errorf("synthesizer not initialized"))
			return
		}

		// 更新监听器状态
		t.listener.Reset(fmt.Sprintf("%s_%d", t.GetName(), t.GetSeq()), packet)

		// 发送合成请求
		if err := t.synthesizer.Process(data, "ACTION_SYNTHESIS"); err != nil {
			log.Printf("Synthesis failed: %v", err)
			t.UpdateErrorStatus(err)
			return
		}

	default:
		t.HandleUnsupportedData(packet.Data)
	}
}

// GetID 实现 Component 接口
func (t *TencentTTS2) GetID() interface{} {
	return t.GetSeq()
}

// Stop 实现 Component 接口，扩展基础组件的 Stop 方法
func (t *TencentTTS2) Stop() {
	// 发送完成请求
	if err := t.synthesizer.Complete("ACTION_COMPLETE"); err != nil {
		log.Printf("Complete synthesis failed: %v", err)
		t.UpdateErrorStatus(err)
		return
	}

	// 等待合成完成
	t.synthesizer.Wait()

	t.BaseComponent.Stop()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.synthesizer != nil {
		t.synthesizer.Stop()
		t.synthesizer = nil
	}
}

// 为了向后兼容，保留这些方法
func (t *TencentTTS2) Process(packet pipeline.Packet) {
	select {
	case t.GetInputChan() <- packet:
	default:
		log.Printf("TencentTTS2: input channel full, dropping packet")
	}
}

func (t *TencentTTS2) SetOutput(output func(pipeline.Packet)) {
	outChan := make(chan pipeline.Packet, 100)
	t.SetOutputChan(outChan)
	go func() {
		for packet := range t.GetOutputChan() {
			if output != nil {
				output(packet)
			}
		}
	}()
}

// SetVoiceType 设置音色
func (t *TencentTTS2) SetVoiceType(voiceType int64) {
	t.voiceType = voiceType
	if t.synthesizer != nil {
		t.synthesizer.SetVoiceType(voiceType)
	}
}

// SetCodec 设置音频编码格式
func (t *TencentTTS2) SetCodec(codec string) {
	t.codec = codec
	if t.synthesizer != nil {
		t.synthesizer.SetCodec(codec)
	}
}

// GetHealth 实现 Component 接口
func (t *TencentTTS2) GetHealth() pipeline.ComponentHealth {
	return t.BaseComponent.GetHealth()
}

// UpdateHealth 实现 Component 接口
func (t *TencentTTS2) UpdateHealth(health pipeline.ComponentHealth) {
	t.BaseComponent.UpdateHealth(health)
}

// tts2SynthesisListener 实现语音合成监听器
type tts2SynthesisListener struct {
	mu        sync.Mutex
	sessionID string
	data      []byte
	tts       *TencentTTS2
	packet    pipeline.Packet
}

// Reset 重置监听器状态
func (l *tts2SynthesisListener) Reset(sessionID string, packet pipeline.Packet) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sessionID = sessionID
	l.data = make([]byte, 0)
	l.packet = packet
}

// OnSynthesisStart 合成开始回调
func (l *tts2SynthesisListener) OnSynthesisStart(sessionID string) {
	log.Printf("Synthesis started: sessionId=%s", l.sessionID)
}

// OnSynthesisEnd 合成结束回调
func (l *tts2SynthesisListener) OnSynthesisEnd() {
	l.mu.Lock()
	defer l.mu.Unlock()

	log.Printf("Synthesis ended: sessionId=%s", l.sessionID)

	// 发送处理后的数据
	l.tts.metrics.TurnEndTs = time.Now().UnixMilli()
	previousMetrics := l.packet.TurnMetricStat
	if previousMetrics == nil {
		previousMetrics = make(map[string]pipeline.TurnMetrics)
	}
	previousMetrics[fmt.Sprintf("%s_%d", l.tts.GetName(), l.tts.GetSeq())] = l.tts.metrics
	l.packet.TurnMetricKeys = append(l.packet.TurnMetricKeys, fmt.Sprintf("%s_%d", l.tts.GetName(), l.tts.GetSeq()))

	l.tts.ForwardPacket(pipeline.Packet{
		Data:           l.data,
		Seq:            l.tts.GetSeq(),
		TurnSeq:        l.tts.GetCurTurnSeq(),
		TurnMetricStat: previousMetrics,
		TurnMetricKeys: l.packet.TurnMetricKeys,
	})
}

// OnAudioResult 音频数据回调
func (l *tts2SynthesisListener) OnAudioResult(audioBytes []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// fmt.Printf("**%s** OnAudioResult: %d bytes", l.tts.GetName(), len(audioBytes))
	l.tts.ForwardPacket(pipeline.Packet{
		Data:    audioBytes,
		Seq:     l.tts.GetSeq(),
		TurnSeq: l.tts.GetCurTurnSeq(),
	})
}

// OnTextResult 文本处理结果回调
func (l *tts2SynthesisListener) OnTextResult(response map[string]interface{}) {
	log.Printf("Text result received: sessionId=%s", l.sessionID)
}

// OnSynthesisFail 合成失败回调
func (l *tts2SynthesisListener) OnSynthesisFail(response map[string]interface{}) {
	log.Printf("Synthesis failed: sessionId=%s, error=%v", l.sessionID, response)
	l.tts.UpdateErrorStatus(fmt.Errorf("synthesis failed: %v", response))
}
