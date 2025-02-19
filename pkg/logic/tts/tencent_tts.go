package tts

import (
	"fmt"
	"log"
	"streamlink/pkg/logic/pipeline"
	"sync"
	"time"

	"github.com/tencentcloud/tencentcloud-speech-sdk-go/common"
	"github.com/tencentcloud/tencentcloud-speech-sdk-go/tts"
)

// TencentTTS 实现 Component 接口
type TencentTTS struct {
	*pipeline.BaseComponent
	appID       int64
	secretID    string
	secretKey   string
	voiceType   int64
	codec       string
	synthesizer *tts.SpeechWsSynthesizer
	listener    *ttsSynthesisListener
	mu          sync.Mutex
	metrics     pipeline.TurnMetrics
}

// NewTencentTTS 创建一个新的语音合成组件
func NewTencentTTS(appID int64, secretID, secretKey string, voiceType int64, codec string) *TencentTTS {
	t := &TencentTTS{
		BaseComponent: pipeline.NewBaseComponent("TencentTTS", 100),
		appID:         appID,
		secretID:      secretID,
		secretKey:     secretKey,
		voiceType:     voiceType,
		codec:         codec,
		metrics:       pipeline.TurnMetrics{},
	}

	// 设置处理函数
	t.BaseComponent.SetProcess(t.processPacket)
	t.RegisterCommandHandler(pipeline.PacketCommandInterrupt, t.handleInterrupt)

	return t
}

func (t *TencentTTS) handleInterrupt(packet pipeline.Packet) {
	// log.Printf("**%s** Received interrupt command for turn %d", t.GetName(), packet.TurnSeq)
	t.SetCurTurnSeq(packet.TurnSeq)

	t.ForwardPacket(packet)
}

// processPacket 处理输入的数据包
func (t *TencentTTS) processPacket(packet pipeline.Packet) {
	t.metrics.TurnStartTs = time.Now().UnixMilli()
	t.metrics.TurnEndTs = 0

	switch data := packet.Data.(type) {
	case string:
		log.Printf("**%s** Processing turn_seq=%d , text: %s", t.GetName(), packet.TurnSeq, data)
		t.mu.Lock()
		defer t.mu.Unlock()

		// 每次处理文本都创建新的 synthesizer
		t.listener = &ttsSynthesisListener{
			sessionID: fmt.Sprintf("%s_%d", t.GetName(), t.GetSeq()),
			data:      make([]byte, 0),
			tts:       t,
			packet:    packet,
		}

		credential := common.NewCredential(t.secretID, t.secretKey)
		t.synthesizer = tts.NewSpeechWsSynthesizer(t.appID, credential, t.listener)
		t.synthesizer.SessionId = t.listener.sessionID
		t.synthesizer.VoiceType = t.voiceType
		t.synthesizer.Codec = t.codec
		t.synthesizer.Text = data

		// 开始合成
		if err := t.synthesizer.Synthesis(); err != nil {
			log.Printf("Synthesis failed: %v", err)
			t.UpdateErrorStatus(err)
			return
		}

		// 等待合成完成
		t.synthesizer.Wait()

		// 清理资源
		t.synthesizer.CloseConn()
		t.synthesizer = nil

		// 发送处理后的数据
		t.metrics.TurnEndTs = time.Now().UnixMilli()
		previousMetrics := packet.TurnMetricStat
		if previousMetrics == nil {
			previousMetrics = make(map[string]pipeline.TurnMetrics)
		}
		previousMetrics[fmt.Sprintf("%s_%d", t.GetName(), t.GetSeq())] = t.metrics
		packet.TurnMetricKeys = append(packet.TurnMetricKeys, fmt.Sprintf("%s_%d", t.GetName(), t.GetSeq()))
		t.ForwardPacket(pipeline.Packet{
			Data:           t.listener.data,
			Seq:            t.GetSeq(),
			TurnSeq:        t.GetCurTurnSeq(),
			TurnMetricStat: previousMetrics,
			TurnMetricKeys: packet.TurnMetricKeys,
		})

	default:
		t.HandleUnsupportedData(packet.Data)
	}
}

// GetID 实现 Component 接口
func (t *TencentTTS) GetID() interface{} {
	return t.GetSeq()
}

// Stop 实现 Component 接口，扩展基础组件的 Stop 方法
func (t *TencentTTS) Stop() {
	t.BaseComponent.Stop()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.synthesizer != nil {
		t.synthesizer.CloseConn()
		t.synthesizer = nil
	}
}

// Process 为了向后兼容，保留这些方法
func (t *TencentTTS) Process(packet pipeline.Packet) {
	select {
	case t.GetInputChan() <- packet:
	default:
		log.Printf("TencentTTS: input channel full, dropping packet")
	}
}

// SetInput 设置输入通道
func (t *TencentTTS) SetInput() {
	inChan := make(chan pipeline.Packet, 100)
	t.SetInputChan(inChan)
}

func (t *TencentTTS) SetOutput(output func(pipeline.Packet)) {
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

// GetAudioData 获取已合成的音频数据
func (t *TencentTTS) GetAudioData() []byte {
	if t.listener != nil {
		return t.listener.data
	}
	return nil
}

// SetVoiceType 设置音色
func (t *TencentTTS) SetVoiceType(voiceType int64) {
	t.voiceType = voiceType
}

// SetCodec 设置音频编码格式
func (t *TencentTTS) SetCodec(codec string) {
	t.codec = codec
}

// GetHealth 实现 Component 接口
func (t *TencentTTS) GetHealth() pipeline.ComponentHealth {
	return t.BaseComponent.GetHealth()
}

// UpdateHealth 实现 Component 接口
func (t *TencentTTS) UpdateHealth(health pipeline.ComponentHealth) {
	t.BaseComponent.UpdateHealth(health)
}

// ttsSynthesisListener 实现语音合成监听器
type ttsSynthesisListener struct {
	sessionID string
	data      []byte
	index     int
	tts       *TencentTTS
	packet    pipeline.Packet
}

// OnSynthesisStart 合成开始回调
func (l *ttsSynthesisListener) OnSynthesisStart(r *tts.SpeechWsSynthesisResponse) {
	log.Printf("Synthesis started: sessionId=%s", l.sessionID)
}

// OnSynthesisEnd 合成结束回调
func (l *ttsSynthesisListener) OnSynthesisEnd(r *tts.SpeechWsSynthesisResponse) {
	log.Printf("Synthesis ended: sessionId=%s", l.sessionID)
}

// OnAudioResult 音频数据回调
func (l *ttsSynthesisListener) OnAudioResult(data []byte) {
	l.index++
	l.data = append(l.data, data...)
}

// OnTextResult 文本处理结果回调
func (l *ttsSynthesisListener) OnTextResult(r *tts.SpeechWsSynthesisResponse) {
	log.Printf("Text result received: sessionId=%s", l.sessionID)
}

// OnSynthesisFail 合成失败回调
func (l *ttsSynthesisListener) OnSynthesisFail(r *tts.SpeechWsSynthesisResponse, err error) {
	log.Printf("Synthesis failed: sessionId=%s, error=%v", l.sessionID, err)
	l.tts.UpdateErrorStatus(err)
}
