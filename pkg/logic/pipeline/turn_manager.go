package pipeline

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// InterruptType 定义打断类型
type InterruptType int

const (
	InterruptTypeNone     InterruptType = iota
	InterruptTypeCommand                // 用户指令打断
	InterruptTypeSemantic               // 语义打断
)

// TurnState 定义轮次状态
type TurnState int

const (
	TurnStateActive  TurnState = iota
	TurnStatePending           // 等待语义判断
	TurnStateComplete
	TurnStateInterrupted
)

// TurnInfo 保存轮次相关信息
type TurnInfo struct {
	TurnSeq        int
	StartTime      time.Time
	LastUpdateTime time.Time
	Text           string // ASR 识别出的文本
	State          TurnState
	InterruptType  InterruptType
}

// TurnManagerConfig 配置
type TurnManagerConfig struct {
	SilenceTimeout    time.Duration // 静音超时时间，超过这个时间认为句子结束
	MaxTurnDuration   time.Duration // 最大轮次持续时间
	MinSentenceLength int           // 最小句子长度
	PunctuationMarks  []string      // 表示句子结束的标点符号
}

// DefaultTurnManagerConfig 返回默认配置
func DefaultTurnManagerConfig() TurnManagerConfig {
	return TurnManagerConfig{
		SilenceTimeout:    2 * time.Second,
		MaxTurnDuration:   30 * time.Second,
		MinSentenceLength: 4,
		PunctuationMarks:  []string{"。", "？", "！", ".", "?", "!"},
	}
}

// TurnManager 组件
type TurnManager struct {
	*BaseComponent
	currentTurn    *TurnInfo
	previousTurn   *TurnInfo
	config         TurnManagerConfig
	sentenceBuffer string
	lastUpdateTime time.Time
	metrics        TurnMetrics
}

// NewTurnManager 创建新的 TurnManager
func NewTurnManager(config TurnManagerConfig) *TurnManager {
	tm := &TurnManager{
		BaseComponent:  NewBaseComponent("TurnManager", 100),
		config:         config,
		lastUpdateTime: time.Now(),
	}
	tm.SetProcess(tm.processPacket)
	// register command handler
	tm.RegisterCommandHandler(PacketCommandInterrupt, tm.handleCommandInterrupt)
	return tm
}

// processPacket 处理输入的数据包
func (tm *TurnManager) processPacket(packet Packet) {
	// 1. 处理 ASR 结果
	if text, ok := packet.Data.(string); ok {
		// log.Printf("**%s** handle asr string: %v", tm.GetName(), packet)
		tm.handleASRResult(text, packet)
		return
	}

	log.Printf("**%s** forward packet:%v", tm.GetName(), packet)
	// 2. 转发其他类型的包
	tm.ForwardPacket(packet)
}

func (tm *TurnManager) handleASRResult(text string, packet Packet) {
	// 更新时间戳
	tm.lastUpdateTime = time.Now()
	tm.metrics.TurnStartTs = time.Now().UnixMilli()
	tm.metrics.TurnEndTs = 0

	// 更新句子缓存
	tm.sentenceBuffer += text

	// 检查是否需要创建新轮次
	if tm.shouldCreateNewTurn() {
		tm.IncrTurnSeq()

		log.Printf("TurnManager: start new turn, seq: %d, cur text: %s", tm.GetCurTurnSeq(), tm.sentenceBuffer)

		// 1. 先发送语义打断指令
		if !tm.GetIgnoreTurn() {
			tm.broadcastInterrupt(tm.GetCurTurnSeq(), InterruptTypeSemantic)
		}

		// 2. 等待一小段时间让打断指令传播
		// time.Sleep(100 * time.Millisecond)

		// 3. 发送当前缓存的完整句子
		tm.metrics.TurnEndTs = time.Now().UnixMilli()
		if tm.sentenceBuffer != "" {
			previousMetrics := packet.TurnMetricStat
			previousMetrics[fmt.Sprintf("%s_%d", tm.GetName(), tm.GetSeq())] = tm.metrics
			packet.TurnMetricKeys = append(packet.TurnMetricKeys, fmt.Sprintf("%s_%d", tm.GetName(), tm.GetSeq()))

			tm.ForwardPacket(Packet{
				Data:           tm.sentenceBuffer,
				Seq:            tm.GetSeq(),
				TurnSeq:        tm.GetCurTurnSeq(),
				TurnMetricStat: previousMetrics,
				TurnMetricKeys: packet.TurnMetricKeys,
			})
		}

		// 4. 创建新轮次
		tm.createNewTurn(tm.GetCurTurnSeq())
	}
}

func (tm *TurnManager) shouldCreateNewTurn() bool {
	// 1. 检查是否有结束标点
	for _, mark := range tm.config.PunctuationMarks {
		if strings.Contains(tm.sentenceBuffer, mark) {
			return true
		}
	}

	// 2. 检查静音时间
	if time.Since(tm.lastUpdateTime) > tm.config.SilenceTimeout {
		return true
	}

	// 3. 检查轮次持续时间
	if tm.currentTurn != nil && time.Since(tm.currentTurn.StartTime) > tm.config.MaxTurnDuration {
		return true
	}

	return false
}

func (tm *TurnManager) handleCommandInterrupt(packet Packet) {
	tm.IncrTurnSeq()

	// 1. 先发送命令打断指令
	tm.broadcastInterrupt(tm.GetCurTurnSeq(), InterruptTypeCommand)

	// 2. 等待一小段时间让打断指令传播
	time.Sleep(20 * time.Millisecond)

	// 3. 如果有未处理的文本，作为新轮次的开始发送
	if tm.sentenceBuffer != "" {
		tm.ForwardPacket(Packet{
			Data:    tm.sentenceBuffer,
			Seq:     0,
			TurnSeq: tm.GetCurTurnSeq(),
			Command: PacketCommandNone,
		})
	}

	// 4. 创建新轮次
	tm.createNewTurn(tm.GetCurTurnSeq())
}

func (tm *TurnManager) createNewTurn(turnSeq int) {
	// 保存当前轮次信息
	if tm.currentTurn != nil {
		tm.previousTurn = tm.currentTurn
		tm.currentTurn.State = TurnStateComplete
	}

	// 创建新轮次
	tm.currentTurn = &TurnInfo{
		TurnSeq:        turnSeq,
		StartTime:      time.Now(),
		LastUpdateTime: time.Now(),
		State:          TurnStateActive,
	}

	// 清空缓存
	tm.sentenceBuffer = ""
	// log.Printf("TurnManager: Created new turn %d", turnSeq)
}

func (tm *TurnManager) broadcastInterrupt(turnSeq int, interruptType InterruptType) {
	packet := Packet{
		Command: PacketCommandInterrupt,
		TurnSeq: turnSeq,
	}
	tm.ForwardPacket(packet)
	log.Printf("TurnManager: Broadcasting interrupt type=%v for turn %d", interruptType, turnSeq)
}

// GetID 实现 Component 接口
func (tm *TurnManager) GetID() interface{} {
	return tm.GetName()
}

// Process 实现 Component 接口
func (tm *TurnManager) Process(packet Packet) {
	select {
	case tm.GetInputChan() <- packet:
	default:
		log.Printf("**%s** Input channel full, dropping packet", tm.GetName())
	}
}

// SetOutput 实现 Component 接口
func (tm *TurnManager) SetOutput(output func(Packet)) {
	outChan := make(chan Packet, 100)
	tm.SetOutputChan(outChan)
	go func() {
		for packet := range tm.GetOutputChan() {
			if output != nil {
				output(packet)
			}
		}
	}()
}

// Start 实现 Component 接口
func (tm *TurnManager) Start() error {
	tm.BaseComponent.Start()
	return nil
}

// GetHealth 实现 Component 接口
func (tm *TurnManager) GetHealth() ComponentHealth {
	return tm.BaseComponent.GetHealth()
}

// UpdateHealth 实现 Component 接口
func (tm *TurnManager) UpdateHealth(health ComponentHealth) {
	tm.BaseComponent.UpdateHealth(health)
}

// GetCurrentTurn 获取当前轮次信息
func (tm *TurnManager) GetCurrentTurn() *TurnInfo {
	return tm.currentTurn
}

// GetPreviousTurn 获取上一轮次信息
func (tm *TurnManager) GetPreviousTurn() *TurnInfo {
	return tm.previousTurn
}
