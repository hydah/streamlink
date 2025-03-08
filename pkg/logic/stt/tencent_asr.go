package stt

import (
	"fmt"
	"log"
	"math/rand"
	"streamlink/pkg/logger"
	"streamlink/pkg/logic/pipeline"
	"sync"
	"time"

	"github.com/tencentcloud/tencentcloud-speech-sdk-go/asr"
	"github.com/tencentcloud/tencentcloud-speech-sdk-go/common"
)

// TencentAsr 实现 Component 接口
type TencentAsr struct {
	*pipeline.BaseComponent
	appID           string
	secretID        string
	secretKey       string
	engineModelType string
	sliceSize       int
	recognizer      *asr.SpeechRecognizer
	resultChan      chan string
	resultMutex     sync.Mutex
	currentText     string
	metrics         pipeline.TurnMetrics
}

// NewTencentAsr 创建一个新的语音识别组件
func NewTencentAsr(appID, secretID, secretKey, engineModelType string, sliceSize int) *TencentAsr {
	t := &TencentAsr{
		BaseComponent:   pipeline.NewBaseComponent("TencentASR", 4000),
		appID:           appID,
		secretID:        secretID,
		secretKey:       secretKey,
		engineModelType: engineModelType,
		sliceSize:       sliceSize,
		resultChan:      make(chan string, 4000),
	}

	// 设置处理函数
	t.BaseComponent.SetProcess(t.processPacket)
	t.RegisterCommandHandler(pipeline.PacketCommandInterrupt, t.handleInterrupt)

	return t
}

func (t *TencentAsr) handleInterrupt(packet pipeline.Packet) {
	log.Printf("**%s** Received interrupt command for turn %d", t.GetName(), packet.TurnSeq)
	t.IncrTurnSeq()

	t.ForwardPacket(packet)
}

// Start 启动语音识别服务
func (t *TencentAsr) Start() error {
	if t.recognizer != nil {
		return fmt.Errorf("recognizer already started")
	}

	id := rand.Intn(1000000)
	listener := &asrListener{
		id:  id,
		asr: t,
	}

	credential := common.NewCredential(t.secretID, t.secretKey)
	t.recognizer = asr.NewSpeechRecognizer(t.appID, credential, t.engineModelType, listener)
	t.recognizer.VoiceFormat = asr.AudioFormatPCM

	if err := t.recognizer.Start(); err != nil {
		log.Printf("Failed to start recognizer: %v", err)
		t.recognizer = nil
		return fmt.Errorf("start recognizer failed: %w", err)
	}

	// 启动基础组件的处理循环
	if err := t.BaseComponent.Start(); err != nil {
		log.Printf("Failed to start base component: %v", err)
		t.recognizer.Stop()
		t.recognizer = nil
		return fmt.Errorf("start base component failed: %w", err)
	}

	return nil
}

// Stop 停止语音识别服务
func (t *TencentAsr) Stop() {
	t.BaseComponent.Stop()
	if t.recognizer != nil {
		t.recognizer.Stop()
		t.recognizer = nil
	}
	// 清理状态
	t.resultMutex.Lock()
	t.currentText = ""
	t.resultMutex.Unlock()
}

// processPacket 处理输入的数据包
func (t *TencentAsr) processPacket(packet pipeline.Packet) {
	// 处理指令
	if t.HandleCommandPacket(packet) {
		return
	}

	// 检查 recognizer 是否已初始化
	if t.recognizer == nil {
		log.Printf("**%s** Error: recognizer not initialized", t.GetName())
		t.UpdateErrorStatus(fmt.Errorf("recognizer not initialized"))
		return
	}

	switch data := packet.Data.(type) {
	case []byte:
		if err := t.recognizer.Write(data); err != nil {
			log.Printf("**%s** Failed to write audio data: %v", t.GetName(), err)
			t.UpdateErrorStatus(err)
		}

	case []int16:
		// 将 []int16 转换为 []byte
		audioBytes := make([]byte, len(data)*2)
		for i, sample := range data {
			audioBytes[i*2] = byte(sample)
			audioBytes[i*2+1] = byte(sample >> 8)
		}

		if err := t.recognizer.Write(audioBytes); err != nil {
			log.Printf("**%s** Failed to write audio data: %v", t.GetName(), err)
			t.UpdateErrorStatus(err)
		}

	default:
		t.HandleUnsupportedData(packet.Data)
	}
}

// GetID 实现 Component 接口
func (t *TencentAsr) GetID() interface{} {
	return t.GetSeq()
}

// GetResult 获取当前识别结果
func (t *TencentAsr) GetResult() string {
	t.resultMutex.Lock()
	defer t.resultMutex.Unlock()
	return t.currentText
}

// GetResultChan 获取结果通道
func (t *TencentAsr) GetResultChan() <-chan string {
	return t.resultChan
}

// 为了向后兼容，保留这些方法
func (t *TencentAsr) Process(packet pipeline.Packet) {
	select {
	case t.GetInputChan() <- packet:
	default:
		log.Printf("TencentAsr: input channel full, dropping packet")
	}
}

func (t *TencentAsr) SetInput() {
	inChan := make(chan pipeline.Packet, 100)
	t.SetInputChan(inChan)
}

func (t *TencentAsr) SetOutput(output func(pipeline.Packet)) {
	go func() {
		for packet := range t.GetOutputChan() {
			if output != nil {
				output(packet)
			}
		}
	}()
}

// asrListener 实现语音识别监听器
type asrListener struct {
	id  int
	asr *TencentAsr
}

func (l *asrListener) OnRecognitionStart(response *asr.SpeechRecognitionResponse) {
	logger.Info("**%s** Recognition started: voice_id=%s", l.asr.GetName(), response.VoiceID)
}

func (l *asrListener) OnSentenceBegin(response *asr.SpeechRecognitionResponse) {
	logger.Info("**%s** Sentence begin: voice_id=%s", l.asr.GetName(), response.VoiceID)
	l.asr.metrics.TurnStartTs = time.Now().UnixMilli()
	l.asr.metrics.TurnEndTs = 0
}

func (l *asrListener) OnRecognitionResultChange(response *asr.SpeechRecognitionResponse) {
	resultText := fmt.Sprintf("%v", response.Result)

	// 更新当前文本
	l.asr.resultMutex.Lock()
	l.asr.currentText = resultText
	l.asr.resultMutex.Unlock()

	// 发送结果到通道
	select {
	case l.asr.resultChan <- resultText:
	default:
		// 如果通道已满，丢弃旧的结果
		select {
		case <-l.asr.resultChan:
		default:
		}
		l.asr.resultChan <- resultText
		l.asr.UpdateDroppedStatus()
	}
}

func (l *asrListener) OnSentenceEnd(response *asr.SpeechRecognitionResponse) {
	resultText := fmt.Sprintf("%v", response.Result.VoiceTextStr)
	logger.Info("**%s** Sentence end: voice_id=%s, text=%s", l.asr.GetName(), response.VoiceID, resultText)

	l.asr.metrics.TurnEndTs = time.Now().UnixMilli()

	// 发送识别结果到输出通道
	l.asr.ForwardPacket(pipeline.Packet{
		Data:    resultText,
		Seq:     l.asr.GetSeq(),
		Src:     l.asr,
		TurnSeq: l.asr.GetCurTurnSeq(),
		TurnMetricStat: map[string]pipeline.TurnMetrics{
			fmt.Sprintf("%s_%d", l.asr.GetName(), l.asr.GetSeq()): l.asr.metrics,
		},
		TurnMetricKeys: []string{fmt.Sprintf("%s_%d", l.asr.GetName(), l.asr.GetSeq())},
	})
	l.asr.IncrSeq()
}

func (l *asrListener) OnRecognitionComplete(response *asr.SpeechRecognitionResponse) {
	logger.Info("**%s** Recognition complete: voice_id=%s", l.asr.GetName(), response.VoiceID)
}

func (l *asrListener) OnFail(response *asr.SpeechRecognitionResponse, err error) {
	logger.Error("**%s** Recognition failed: voice_id=%s, error=%v", l.asr.GetName(), response.VoiceID, err)
	l.asr.UpdateErrorStatus(err)
}

// GetHealth 实现 Component 接口
func (t *TencentAsr) GetHealth() pipeline.ComponentHealth {
	return t.BaseComponent.GetHealth()
}

// UpdateHealth 实现 Component 接口
func (t *TencentAsr) UpdateHealth(health pipeline.ComponentHealth) {
	t.BaseComponent.UpdateHealth(health)
}
