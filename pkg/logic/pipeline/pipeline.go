package pipeline

import (
	"fmt"
	"streamlink/pkg/logger"
	"strings"
	"sync"
	"time"
)

// Pipeline 处理数据的管道
type Pipeline struct {
	components []Component
	source     Component
	stopCh     chan struct{}
	// 新增健康监控相关字段
	healthCheckInterval time.Duration
	healthCheckTicker   *time.Ticker
	lastHealthCheck     map[interface{}]ComponentHealth
	healthLock          sync.RWMutex
}

// NewPipeline 创建新的处理管道
func NewPipeline() *Pipeline {
	return &Pipeline{
		stopCh:              make(chan struct{}),
		healthCheckInterval: 30 * time.Second, // 默认每30秒检查一次
		lastHealthCheck:     make(map[interface{}]ComponentHealth),
	}
}

// NewPipelineWithSource 创建新的处理管道并设置音频源
func NewPipelineWithSource(source Component) *Pipeline {
	p := NewPipeline()
	p.source = source
	return p
}

// Process 处理数据
func (p *Pipeline) Process(data interface{}) {
	if len(p.components) == 0 {
		return
	}

	// 创建初始数据包
	packet := Packet{
		Data:    data,
		Seq:     0,
		Src:     nil,
		TurnSeq: 0,
		Command: PacketCommandNone,
	}

	// 非阻塞地发送到第一个组件
	select {
	case p.components[0].GetInputChan() <- packet:
	default:
		logger.Error("Pipeline: first component's input channel full, dropping packet")
	}
}

// SendInterrupt 发送打断指令
func (p *Pipeline) SendInterrupt(turnSeq int) {
	if len(p.components) == 0 {
		return
	}

	// 创建打断指令包
	packet := Packet{
		Data:    nil,
		Seq:     0,
		Src:     nil,
		TurnSeq: turnSeq,
		Command: PacketCommandInterrupt,
	}

	// 非阻塞地发送到第一个组件
	select {
	case p.components[0].GetInputChan() <- packet:
	default:
		logger.Error("Pipeline: first component's input channel full, dropping interrupt packet")
	}
}

// SetSource 设置音频源组件
func (p *Pipeline) SetSource(source Component) {
	p.source = source
}

// Start 启动 pipeline，连接音频源和其他组件
func (p *Pipeline) Start() error {
	if p.source == nil {
		return fmt.Errorf("no source component set")
	}

	if len(p.components) == 0 {
		return fmt.Errorf("no components to connect")
	}

	// 启动所有组件
	for _, comp := range p.components {
		if err := comp.Start(); err != nil {
			// 如果启动失败，停止已经启动的组件
			p.source.Stop()
			for _, c := range p.components {
				if c != comp {
					c.Stop()
				}
			}
			return fmt.Errorf("failed to start component %s: %v",
				comp.(interface{ GetName() string }).GetName(), err)
		}
		logger.Info("Start component: %s", comp.(interface{ GetName() string }).GetName())
	}

	// 启动健康检查
	p.StartHealthCheck()

	return nil
}

// Connect 连接组件（不包括音频源）
func (p *Pipeline) Connect(components ...Component) error {
	if len(components) == 0 {
		return fmt.Errorf("no components to connect")
	}

	logger.Info("Initializing pipeline with %d components", len(components))
	p.components = components

	// 连接音频源到第一个组件
	p.source.Connect(p.components[0])

	// 连接组件：将前一个组件的输出连接到后一个组件的输入
	for i := 0; i < len(p.components)-1; i++ {
		current := p.components[i]
		next := p.components[i+1]
		current.Connect(next)
	}

	return nil
}

// Stop 停止所有组件
func (p *Pipeline) Stop() {
	if p.healthCheckTicker != nil {
		p.healthCheckTicker.Stop()
	}
	close(p.stopCh)
	if p.source != nil {
		p.source.Stop()
	}
	for _, component := range p.components {
		component.Stop()
	}
}

// StartHealthCheck 启动健康检查
func (p *Pipeline) StartHealthCheck() {
	p.healthCheckTicker = time.NewTicker(p.healthCheckInterval)
	go func() {
		for {
			select {
			case <-p.stopCh:
				return
			case <-p.healthCheckTicker.C:
				p.checkComponentsHealth()
			}
		}
	}()
}

// checkComponentsHealth 检查所有组件的健康状态
func (p *Pipeline) checkComponentsHealth() {
	p.healthLock.Lock()
	defer p.healthLock.Unlock()

	var healthInfo []string
	var stateChanges []string
	var droppedInfo []string

	for _, comp := range p.components {
		health := comp.GetHealth()
		lastHealth, exists := p.lastHealthCheck[comp.GetID()]

		// 检查组件状态变化
		if !exists || lastHealth.State != health.State {
			stateChanges = append(stateChanges, fmt.Sprintf("%s:%s->%s",
				comp.(interface{ GetName() string }).GetName(), lastHealth.State, health.State))
		}

		// 检查是否有丢包
		if exists && health.DroppedCount > lastHealth.DroppedCount {
			droppedInfo = append(droppedInfo, fmt.Sprintf("%s:+%d",
				comp.(interface{ GetName() string }).GetName(), health.DroppedCount-lastHealth.DroppedCount))
		}

		// 收集组件健康信息
		healthInfo = append(healthInfo, fmt.Sprintf("[%s]: state=%s in=%d out=%d proc=%d drop=%d err=%v",
			comp.(interface{ GetName() string }).GetName(),
			health.State,
			health.InputQueueSize,
			health.OutputQueueSize,
			health.ProcessedCount,
			health.DroppedCount,
			health.LastError != nil))

		// 更新最后检查的状态
		p.lastHealthCheck[comp.GetID()] = health
	}

	// 构建完整的健康状态日志
	var logParts []string
	logParts = append(logParts, fmt.Sprintf("Components:\n%s", strings.Join(healthInfo, "\n")))
	if len(stateChanges) > 0 {
		logParts = append(logParts, fmt.Sprintf("StateChanges:\n%s", strings.Join(stateChanges, "\n")))
	}
	if len(droppedInfo) > 0 {
		logParts = append(logParts, fmt.Sprintf("Dropped:\n%s", strings.Join(droppedInfo, "\n")))
	}

	// 输出单条日志
	logger.Info("Pipeline Stats:\n%s", strings.Join(logParts, "\n\n"))
}

// GetComponentHealth 获取指定组件的健康状态
func (p *Pipeline) GetComponentHealth(id interface{}) (ComponentHealth, bool) {
	p.healthLock.RLock()
	defer p.healthLock.RUnlock()

	health, exists := p.lastHealthCheck[id]
	return health, exists
}

// GetAllComponentsHealth 获取所有组件的健康状态
func (p *Pipeline) GetAllComponentsHealth() map[interface{}]ComponentHealth {
	p.healthLock.RLock()
	defer p.healthLock.RUnlock()

	result := make(map[interface{}]ComponentHealth)
	for id, health := range p.lastHealthCheck {
		result[id] = health
	}
	return result
}

// SetHealthCheckInterval 设置健康检查间隔
func (p *Pipeline) SetHealthCheckInterval(interval time.Duration) {
	p.healthCheckInterval = interval
	if p.healthCheckTicker != nil {
		p.healthCheckTicker.Reset(interval)
	}
}
