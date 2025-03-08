package tts

import (
	"fmt"
	"log"
	"sort"
	"streamlink/pkg/logger"
	"streamlink/pkg/logic/pipeline"
	"sync"
	"time"
)

// TencentStreamTTS 实现 Component 接口
type TencentStreamTTS struct {
	*pipeline.BaseComponent
	appID                int64
	secretID             string
	secretKey            string
	voiceType            int64
	codec                string
	primarySynthesizer   *FlowingSpeechSynthesizer // 主TTS合成器
	backupSynthesizer    *FlowingSpeechSynthesizer // 备用TTS合成器
	activeSynthesizerIdx int                       // 当前活跃的合成器索引 (0=主, 1=备用)
	listener             *tts2SynthesisListener
	mu                   sync.Mutex
	metrics              pipeline.TurnMetrics
	// 自定义延迟指标
	firstTokenLatencyMs int64 // 首token延迟(毫秒)
	totalLatencyMs      int64 // 总延迟(毫秒)
}

// NewTencentStreamTTS 创建一个新的语音合成组件
func NewTencentStreamTTS(appID int64, secretID, secretKey string, voiceType int64, codec string) *TencentStreamTTS {
	t := &TencentStreamTTS{
		BaseComponent:        pipeline.NewBaseComponent("TencentStreamTTS", 100),
		appID:                appID,
		secretID:             secretID,
		secretKey:            secretKey,
		voiceType:            voiceType,
		codec:                codec,
		activeSynthesizerIdx: -1, // 初始使用主TTS合成器
	}

	// 设置处理函数
	t.BaseComponent.SetProcess(t.processPacket)
	t.RegisterCommandHandler(pipeline.PacketCommandInterrupt, t.handleInterrupt)

	return t
}

// Start 启动语音合成服务
func (t *TencentStreamTTS) Start() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 创建监听器
	t.listener = &tts2SynthesisListener{
		tts:             t,
		turnStartTimes:  make(map[int]time.Time),
		turnFirstTokens: make(map[int]time.Time),
		processedTurns:  make(map[int]bool),
	}

	// 创建凭证
	credential := &Credential{
		SecretID:  t.secretID,
		SecretKey: t.secretKey,
	}

	// 创建主TTS合成器
	t.primarySynthesizer = NewFlowingSpeechSynthesizer(t.appID, fmt.Sprintf("TTS_Flow_0_%d", time.Now().UnixMicro()), credential, t.listener)
	t.primarySynthesizer.SetVoiceType(t.voiceType)
	t.primarySynthesizer.SetCodec(t.codec)
	t.primarySynthesizer.SetSampleRate(16000)
	t.primarySynthesizer.SetVolume(0)
	t.primarySynthesizer.SetSpeed(1)
	t.primarySynthesizer.SetEnableSubtitle(false)

	// 创建备用TTS合成器
	t.backupSynthesizer = NewFlowingSpeechSynthesizer(t.appID, fmt.Sprintf("TTS_Flow_1_%d", time.Now().UnixMicro()), credential, t.listener)
	t.backupSynthesizer.SetVoiceType(t.voiceType)
	t.backupSynthesizer.SetCodec(t.codec)
	t.backupSynthesizer.SetSampleRate(16000)
	t.backupSynthesizer.SetVolume(0)
	t.backupSynthesizer.SetSpeed(1)
	t.backupSynthesizer.SetEnableSubtitle(false)

	// 启动主合成器
	if err := t.primarySynthesizer.Start(); err != nil {
		log.Printf("Start primary synthesizer failed: %v", err)
		return fmt.Errorf("start primary synthesizer failed: %v", err)
	}

	// 启动备用合成器
	if err := t.backupSynthesizer.Start(); err != nil {
		t.primarySynthesizer.Stop()
		log.Printf("Start backup synthesizer failed: %v", err)
		return fmt.Errorf("start backup synthesizer failed: %v", err)
	}

	// 等待两个合成器都就绪
	if !t.primarySynthesizer.WaitReady(5000) {
		t.primarySynthesizer.Stop()
		t.backupSynthesizer.Stop()
		return fmt.Errorf("wait primary synthesizer ready timeout")
	}

	if !t.backupSynthesizer.WaitReady(5000) {
		t.primarySynthesizer.Stop()
		t.backupSynthesizer.Stop()
		return fmt.Errorf("wait backup synthesizer ready timeout")
	}

	// 启动基础组件
	if err := t.BaseComponent.Start(); err != nil {
		t.primarySynthesizer.Stop()
		t.backupSynthesizer.Stop()
		return fmt.Errorf("start base component failed: %v", err)
	}

	t.keepAcive()

	return nil
}

func (t *TencentStreamTTS) keepAcive() {
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			t.mu.Lock()
			if t.activeSynthesizerIdx == 0 {
				if t.backupSynthesizer != nil {
					t.backupSynthesizer.Process("。", "ACTION_RESET")
				}
			} else {
				if t.primarySynthesizer != nil {
					t.primarySynthesizer.Process("。", "ACTION_RESET")
				}
			}
			t.mu.Unlock()
		}
	}()
}

func (t *TencentStreamTTS) handleInterrupt(packet pipeline.Packet) {
	logger.Info("**%s** Received interrupt command for turn %d", t.GetName(), packet.TurnSeq)
	t.SetCurTurnSeq(packet.TurnSeq)

	t.ForwardPacket(packet)

	t.mu.Lock()
	defer t.mu.Unlock()

	// 确定当前活跃的合成器和备用合成器
	activeSynthesizer := t.primarySynthesizer

	if t.activeSynthesizerIdx == -1 {
		t.activeSynthesizerIdx = 0
		return
	}

	if t.activeSynthesizerIdx == 1 {
		activeSynthesizer = t.backupSynthesizer
	}

	// 关闭当前活跃的合成器连接
	if activeSynthesizer != nil {
		activeSynthesizer.Complete("ACTION_COMPLETE")
		activeSynthesizer.Stop()
		logger.Info("**%s** Close active synthesizer, idx=%d", t.GetName(), t.activeSynthesizerIdx)
	}

	// 切换活跃合成器
	t.activeSynthesizerIdx = 1 - t.activeSynthesizerIdx

	// 重新启动之前活跃的合成器，使其变为新的备用合成器
	credential := &Credential{
		SecretID:  t.secretID,
		SecretKey: t.secretKey,
	}

	if t.activeSynthesizerIdx == 0 {
		// 重建备用合成器（之前是主合成器）
		t.backupSynthesizer = NewFlowingSpeechSynthesizer(t.appID, fmt.Sprintf("TTS_Flow_1_%d", time.Now().UnixMicro()), credential, t.listener)
		t.backupSynthesizer.SetVoiceType(t.voiceType)
		t.backupSynthesizer.SetCodec(t.codec)
		t.backupSynthesizer.SetSampleRate(16000)
		t.backupSynthesizer.SetVolume(0)
		t.backupSynthesizer.SetSpeed(1)
		t.backupSynthesizer.SetEnableSubtitle(false)

		go func() {
			// 启动新的备用合成器
			if err := t.backupSynthesizer.Start(); err != nil {
				log.Printf("Start backup synthesizer failed: %v", err)
			}

			// 等待备用合成器就绪
			if !t.backupSynthesizer.WaitReady(5000) {
				log.Printf("Wait backup synthesizer ready timeout")
				t.backupSynthesizer = nil
			}
		}()
	} else {
		// 重建主合成器（之前是备用合成器）
		t.primarySynthesizer = NewFlowingSpeechSynthesizer(t.appID, fmt.Sprintf("TTS_Flow_0_%d", time.Now().UnixMicro()), credential, t.listener)
		t.primarySynthesizer.SetVoiceType(t.voiceType)
		t.primarySynthesizer.SetCodec(t.codec)
		t.primarySynthesizer.SetSampleRate(16000)
		t.primarySynthesizer.SetVolume(0)
		t.primarySynthesizer.SetSpeed(0)
		t.primarySynthesizer.SetEnableSubtitle(false)

		go func() {
			// 启动新的主合成器
			if err := t.primarySynthesizer.Start(); err != nil {
				log.Printf("Start primary synthesizer failed: %v", err)
			}

			// 等待主合成器就绪
			if !t.primarySynthesizer.WaitReady(5000) {
				log.Printf("Wait primary synthesizer ready timeout")
				t.primarySynthesizer = nil
			}
		}()
	}
	logger.Info("**%s** Switch synthesizer to %d", t.GetName(), t.activeSynthesizerIdx)
}

func (t *TencentStreamTTS) getActiveSynthesizer() *FlowingSpeechSynthesizer {
	if t.activeSynthesizerIdx == 0 {
		return t.primarySynthesizer
	}
	return t.backupSynthesizer
}

// processPacket 处理输入的数据包
func (t *TencentStreamTTS) processPacket(packet pipeline.Packet) {
	t.metrics.TurnStartTs = time.Now().UnixMilli()
	t.metrics.TurnEndTs = 0
	// 重置延迟指标
	t.firstTokenLatencyMs = 0
	t.totalLatencyMs = 0

	switch data := packet.Data.(type) {
	case string:
		if packet.TurnSeq < t.GetCurTurnSeq() {
			logger.Info("**%s** Skip turn_seq=%d , text: %s", t.GetName(), packet.TurnSeq, data)
			return
		}

		// log.Printf("**%s** Processing turn_seq=%d , text: %s", t.GetName(), packet.TurnSeq, data)
		t.mu.Lock()
		defer t.mu.Unlock()

		// 获取当前活跃的合成器
		activeSynthesizer := t.getActiveSynthesizer()
		// 检查合成器状态
		if activeSynthesizer == nil {
			t.UpdateErrorStatus(fmt.Errorf("active synthesizer not initialized"))
			return
		}
		logger.Debug("**%s** Active synthesizer: %v, idx=%d", t.GetName(), activeSynthesizer, t.activeSynthesizerIdx)

		// 更新监听器状态
		t.listener.Reset(activeSynthesizer.GetSessionID(), packet)

		// 发送合成请求
		if err := activeSynthesizer.Process(data, "ACTION_SYNTHESIS"); err != nil {
			logger.Error("Synthesis failed: %v", err)
			t.UpdateErrorStatus(err)
			return
		}

	default:
		t.HandleUnsupportedData(packet.Data)
	}
}

// GetID 实现 Component 接口
func (t *TencentStreamTTS) GetID() interface{} {
	return t.GetSeq()
}

// Stop 实现 Component 接口，扩展基础组件的 Stop 方法
func (t *TencentStreamTTS) Stop() {
	t.mu.Lock()

	// 获取当前活跃的合成器
	var activeSynthesizer *FlowingSpeechSynthesizer
	if t.activeSynthesizerIdx == 0 {
		activeSynthesizer = t.primarySynthesizer
	} else {
		activeSynthesizer = t.backupSynthesizer
	}

	// 尝试完成活跃的合成器
	if activeSynthesizer != nil {
		if err := activeSynthesizer.Complete("ACTION_COMPLETE"); err != nil {
			logger.Error("Complete active synthesis failed: %v", err)
			t.UpdateErrorStatus(err)
		} else {
			// 等待合成完成
			activeSynthesizer.Wait()
		}
	}
	t.mu.Unlock()

	t.BaseComponent.Stop()

	t.mu.Lock()
	defer t.mu.Unlock()

	// 停止所有合成器
	if t.primarySynthesizer != nil {
		t.primarySynthesizer.Stop()
		t.primarySynthesizer = nil
	}
	if t.backupSynthesizer != nil {
		t.backupSynthesizer.Stop()
		t.backupSynthesizer = nil
	}
}

// 为了向后兼容，保留这些方法
func (t *TencentStreamTTS) Process(packet pipeline.Packet) {
	select {
	case t.GetInputChan() <- packet:
	default:
		logger.Error("TencentStreamTTS: input channel full, dropping packet")
	}
}

func (t *TencentStreamTTS) SetOutput(output func(pipeline.Packet)) {
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
func (t *TencentStreamTTS) SetVoiceType(voiceType int64) {
	t.voiceType = voiceType
	if t.primarySynthesizer != nil {
		t.primarySynthesizer.SetVoiceType(voiceType)
	}
	if t.backupSynthesizer != nil {
		t.backupSynthesizer.SetVoiceType(voiceType)
	}
}

// SetCodec 设置音频编码格式
func (t *TencentStreamTTS) SetCodec(codec string) {
	t.codec = codec
	if t.primarySynthesizer != nil {
		t.primarySynthesizer.SetCodec(codec)
	}
	if t.backupSynthesizer != nil {
		t.backupSynthesizer.SetCodec(codec)
	}
}

// GetHealth 实现 Component 接口
func (t *TencentStreamTTS) GetHealth() pipeline.ComponentHealth {
	return t.BaseComponent.GetHealth()
}

// UpdateHealth 实现 Component 接口
func (t *TencentStreamTTS) UpdateHealth(health pipeline.ComponentHealth) {
	t.BaseComponent.UpdateHealth(health)
}

// tts2SynthesisListener 实现语音合成监听器
type tts2SynthesisListener struct {
	mu        sync.Mutex
	sessionID string
	// data           []byte
	tts            *TencentStreamTTS
	packet         pipeline.Packet
	turnSeq        int
	startTime      time.Time // 当前packet处理开始时间
	firstTokenTime time.Time // 当前packet首个音频数据接收时间
	hasFirstToken  bool      // 当前packet是否已接收首个音频数据

	// 按turn序列号记录的计时信息
	turnStartTimes  map[int]time.Time // 每个turn序列的真正开始时间
	turnFirstTokens map[int]time.Time // 每个turn序列的首个token时间
	processedTurns  map[int]bool      // 跟踪哪些turn已经处理完成
}

// Reset 重置监听器状态
func (l *tts2SynthesisListener) Reset(sessionID string, packet pipeline.Packet) {
	logger.Debug("**%s** Reset turn_seq=%d, content=%v", l.tts.GetName(), packet.TurnSeq, packet.Data)
	l.mu.Lock()
	defer l.mu.Unlock()

	l.sessionID = sessionID
	l.packet = packet
	l.turnSeq = packet.TurnSeq
	l.startTime = time.Now() // 当前packet的处理时间
	l.hasFirstToken = false

	// 只有当这是该turn序列号的第一个packet时，才记录turn的开始时间
	if _, exists := l.turnStartTimes[packet.TurnSeq]; !exists {
		now := time.Now()
		l.turnStartTimes[packet.TurnSeq] = now
		logger.Info("**%s** New turn %d started at %v", l.tts.GetName(), packet.TurnSeq, now.UnixMilli())
	}
}

// OnSynthesisStart 合成开始回调
func (l *tts2SynthesisListener) OnSynthesisStart(sessionID string) {
	logger.Info("%s Synthesis started", sessionID)
}

// OnSynthesisEnd 合成结束回调
func (l *tts2SynthesisListener) OnSynthesisEnd() {
	l.mu.Lock()
	defer l.mu.Unlock()

	logger.Info("%s Synthesis ended", l.sessionID)

	// 获取turn的开始时间
	startTime, ok := l.turnStartTimes[l.turnSeq]
	if !ok {
		// 如果没有找到开始时间，使用当前时间减去1秒作为估计值
		startTime = time.Now().Add(-1 * time.Second)
		logger.Warn("**%s** Warning: No start time found for turn %d, using estimate", l.tts.GetName(), l.turnSeq)
	}

	// 计算总耗时（从turn开始到合成结束）
	endTime := time.Now()
	totalDuration := endTime.Sub(startTime)
	l.tts.totalLatencyMs = totalDuration.Milliseconds()
	l.tts.metrics.TurnEndTs = endTime.UnixMilli()

	// 获取首token时间（如果有）
	firstTokenTime, hasFirstToken := l.turnFirstTokens[l.turnSeq]
	var firstTokenLatency time.Duration
	if hasFirstToken {
		firstTokenLatency = firstTokenTime.Sub(startTime)
	} else {
		// 如果没有记录首token，可能是因为没有有效音频数据
		logger.Warn("**%s** Warning: No first token recorded for turn %d", l.tts.GetName(), l.turnSeq)
	}

	// 输出性能指标
	logger.Info("[TurnSeq: %d]  **%s**  %s, Turn completed: total duration=%v, first token=%v",
		l.turnSeq, l.tts.GetName(), l.sessionID, totalDuration, firstTokenLatency)

	// 标记该turn已处理完成
	l.processedTurns[l.turnSeq] = true

	// 发送处理后的数据
	previousMetrics := l.packet.TurnMetricStat
	if previousMetrics == nil {
		previousMetrics = make(map[string]pipeline.TurnMetrics)
	}

	metricKey := fmt.Sprintf("%s_%d", l.tts.GetName(), l.tts.GetSeq())
	previousMetrics[metricKey] = l.tts.metrics
	l.packet.TurnMetricKeys = append(l.packet.TurnMetricKeys, metricKey)

	// 为指标添加自定义字段信息
	metricKey = fmt.Sprintf("%s_%d_first_token_ms", l.tts.GetName(), l.tts.GetSeq())
	l.packet.TurnMetricKeys = append(l.packet.TurnMetricKeys, metricKey)

	metricKey = fmt.Sprintf("%s_%d_total_ms", l.tts.GetName(), l.tts.GetSeq())
	l.packet.TurnMetricKeys = append(l.packet.TurnMetricKeys, metricKey)

	// 清理旧的turn记录，只保留最近10个
	l.cleanupOldTurnRecords(10)

	if l.turnSeq < l.tts.GetCurTurnSeq() {
		logger.Info("**%s** Skip turn_seq=%d ", l.tts.GetName(), l.turnSeq)
		return
	}
}

// cleanupOldTurnRecords 清理旧的turn记录，只保留最近的N个
func (l *tts2SynthesisListener) cleanupOldTurnRecords(keepCount int) {
	// 如果记录数量小于保留阈值，不需要清理
	if len(l.processedTurns) <= keepCount {
		return
	}

	// 获取所有已处理的turn序列号
	var turns []int
	for turn := range l.processedTurns {
		turns = append(turns, turn)
	}

	// 按序列号排序
	sort.Ints(turns)

	// 计算需要删除的数量
	removeCount := len(turns) - keepCount
	if removeCount <= 0 {
		return
	}

	// 删除旧的记录
	for i := 0; i < removeCount; i++ {
		oldTurn := turns[i]
		delete(l.turnStartTimes, oldTurn)
		delete(l.turnFirstTokens, oldTurn)
		delete(l.processedTurns, oldTurn)
	}

	logger.Info("**%s** Cleaned up %d old turn records", l.tts.GetName(), removeCount)
}

// OnAudioResult 音频数据回调
func (l *tts2SynthesisListener) OnAudioResult(audioBytes []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 只处理有效的音频数据
	if len(audioBytes) > 0 {
		// 如果这是该turn的首个有效音频数据
		if _, exists := l.turnFirstTokens[l.turnSeq]; !exists {
			now := time.Now()
			l.turnFirstTokens[l.turnSeq] = now

			// 计算真正的首token延迟（从turn开始到首个token）
			if startTime, ok := l.turnStartTimes[l.turnSeq]; ok {
				firstTokenLatency := now.Sub(startTime)
				l.tts.firstTokenLatencyMs = firstTokenLatency.Milliseconds()
				logger.Info("[TurnSeq: %d] **%s**  %s, First audio token received latency: %v",
					l.turnSeq, l.tts.GetName(), l.sessionID, firstTokenLatency)
			}
		}

		// 当前packet的首token记录（用于调试）
		if !l.hasFirstToken {
			l.firstTokenTime = time.Now()
			l.hasFirstToken = true
		}
	}

	// fmt.Printf("**%s** OnAudioResult: %d bytes", l.tts.GetName(), len(audioBytes))
	// if len(audioBytes) == 0 {
	// 	l.selfTurnSeq++
	// 	log.Printf("**%s** len is zero, selfTurnSeq=%d, turnSeq=%d", l.tts.GetName(), l.selfTurnSeq, l.turnSeq)
	// }

	// 转发音频数据
	l.tts.ForwardPacket(pipeline.Packet{
		Data:    audioBytes,
		Seq:     l.tts.GetSeq(),
		TurnSeq: l.turnSeq,
	})
}

// OnTextResult 文本处理结果回调
func (l *tts2SynthesisListener) OnTextResult(response map[string]interface{}) {
	logger.Info("Text result received: sessionId=%s", l.sessionID)
}

// OnSynthesisFail 合成失败回调
func (l *tts2SynthesisListener) OnSynthesisFail(response map[string]interface{}) {
	logger.Error("Synthesis failed: sessionId=%s, error=%v", l.sessionID, response)
	l.tts.UpdateErrorStatus(fmt.Errorf("synthesis failed: %v", response))
}

// SwitchSynthesizer 手动切换当前活跃的合成器
// 这可以用于故障恢复或主动切换测试
func (t *TencentStreamTTS) SwitchSynthesizer() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	logger.Info("**%s** Manually switching synthesizer from %d to %d", t.GetName(), t.activeSynthesizerIdx, 1-t.activeSynthesizerIdx)

	// 切换活跃合成器
	t.activeSynthesizerIdx = 1 - t.activeSynthesizerIdx

	// 检查新活跃合成器是否可用
	var activeSynthesizer *FlowingSpeechSynthesizer
	if t.activeSynthesizerIdx == 0 {
		activeSynthesizer = t.primarySynthesizer
	} else {
		activeSynthesizer = t.backupSynthesizer
	}

	if activeSynthesizer == nil {
		return fmt.Errorf("switched to unavailable synthesizer")
	}

	return nil
}
