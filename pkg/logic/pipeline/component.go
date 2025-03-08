package pipeline

import (
	"fmt"
	"streamlink/pkg/logger"
	"sync"
	"time"
)

type TurnMetrics struct {
	TurnStartTs int64
	TurnEndTs   int64
}

// Packet 定义了通用的数据包结构
type Packet struct {
	Data           interface{} // 可以是 []int16, string, *rtp.Packet 等
	Seq            int
	Src            interface{}
	TurnSeq        int
	TurnMetricStat map[string]TurnMetrics
	TurnMetricKeys []string
	Command        PacketCommand // 用于特殊指令，如打断
}

// PacketCommand 定义了数据包的特殊指令
type PacketCommand int

const (
	PacketCommandNone      PacketCommand = iota // 普通数据包
	PacketCommandInterrupt                      // 打断指令
)

// GenInterruptPacket 生成一个打断指令包
func GenInterruptPacket(turnSeq int) *Packet {
	return &Packet{
		Data:    nil,
		Seq:     0,
		Src:     nil,
		TurnSeq: turnSeq,
		Command: PacketCommandInterrupt,
	}
}

// ComponentState 定义组件的运行状态
type ComponentState int

const (
	ComponentStateInitial ComponentState = iota
	ComponentStateStarting
	ComponentStateRunning
	ComponentStateWarning
	ComponentStateStopping
	ComponentStateStopped
	ComponentStateError
)

// String 返回状态的字符串表示
func (s ComponentState) String() string {
	switch s {
	case ComponentStateInitial:
		return "Initial"
	case ComponentStateStarting:
		return "Starting"
	case ComponentStateRunning:
		return "Running"
	case ComponentStateWarning:
		return "Warning"
	case ComponentStateStopping:
		return "Stopping"
	case ComponentStateStopped:
		return "Stopped"
	case ComponentStateError:
		return "Error"
	default:
		return "Unknown"
	}
}

// ComponentHealth 定义组件的健康信息
type ComponentHealth struct {
	State           ComponentState `json:"state"`
	LastError       error          `json:"last_error,omitempty"`
	LastErrorTime   time.Time      `json:"last_error_time,omitempty"`
	ProcessedCount  int64          `json:"processed_count"`
	DroppedCount    int64          `json:"dropped_count"`
	InputQueueSize  int            `json:"input_queue_size"`
	OutputQueueSize int            `json:"output_queue_size"`
	StartTime       time.Time      `json:"start_time"`
	LastUpdateTime  time.Time      `json:"last_update_time"`
}

// Component 接口定义了组件的基本行为
type Component interface {
	Process(packet Packet)
	SetOutput(func(Packet))
	GetID() interface{}

	// 新增异步处理相关的方法
	Start() error               // 启动组件的处理循环
	Stop()                      // 停止组件
	GetInputChan() chan Packet  // 获取组件的输入 channel
	GetOutputChan() chan Packet // 获取组件的输出 channel
	SetInputChan(chan Packet)   // 设置组件的输入 channel
	SetOutputChan(chan Packet)  // 设置组件的输出 channel

	// 新增健康检查相关方法
	GetHealth() ComponentHealth          // 获取组件健康状态
	UpdateHealth(health ComponentHealth) // 更新组件健康状态

	// Connect 连接到下一个组件，返回下一个组件以支持链式调用
	Connect(next Component) Component
}

// BaseComponent 提供了基础的 channel 处理逻辑
type BaseComponent struct {
	inputChan    chan Packet
	outputChan   chan Packet
	stopCh       chan struct{}
	process      func(Packet) // 实际的处理函数
	name         string       // 组件名称
	curTurnSeq   int
	turnStartTs  int64
	seq          int
	ignoreTurn   bool
	useInterrupt bool

	// 健康监控相关字段
	health     ComponentHealth
	healthLock sync.RWMutex

	// 指令处理器映射
	commandHandlers map[PacketCommand]func(Packet)
	handlersLock    sync.RWMutex
}

// NewBaseComponent 创建一个新的基础组件
func NewBaseComponent(name string, bufferSize int) *BaseComponent {
	now := time.Now()
	return &BaseComponent{
		outputChan: make(chan Packet, bufferSize),
		stopCh:     make(chan struct{}),
		name:       name,
		health: ComponentHealth{
			State:          ComponentStateInitial,
			StartTime:      now,
			LastUpdateTime: now,
		},
		curTurnSeq:      0,
		seq:             0,
		ignoreTurn:      false,
		useInterrupt:    false,
		commandHandlers: make(map[PacketCommand]func(Packet)),
	}
}

func (b *BaseComponent) GetInputChan() chan Packet {
	return b.inputChan
}

func (b *BaseComponent) SetOutputChan(ch chan Packet) {
	b.outputChan = ch
}

func (b *BaseComponent) Start() error {
	go b.processLoop()
	return nil
}

func (b *BaseComponent) Stop() {
	close(b.stopCh)
}

// RegisterCommandHandler 注册指令处理函数
func (b *BaseComponent) RegisterCommandHandler(cmd PacketCommand, handler func(Packet)) {
	b.handlersLock.Lock()
	defer b.handlersLock.Unlock()
	b.commandHandlers[cmd] = handler
}

// UnregisterCommandHandler 注销指令处理函数
func (b *BaseComponent) UnregisterCommandHandler(cmd PacketCommand) {
	b.handlersLock.Lock()
	defer b.handlersLock.Unlock()
	delete(b.commandHandlers, cmd)
}

// processLoop 是组件的主处理循环
func (b *BaseComponent) processLoop() {
	// 更新状态为运行中
	b.healthLock.Lock()
	b.health.State = ComponentStateRunning
	b.healthLock.Unlock()

	for {
		select {
		case <-b.stopCh:
			b.healthLock.Lock()
			b.health.State = ComponentStateStopped
			b.healthLock.Unlock()
			return
		case packet := <-b.inputChan:
			b.healthLock.Lock()
			b.health.ProcessedCount++
			b.healthLock.Unlock()

			// handle command
			if packet.Command != PacketCommandNone {
				b.HandleCommandPacket(packet)
				continue
			}

			// if b.GetName() != "Resampler_48000Hz_2Ch->16000Hz_1Ch" && b.GetName() != "TencentASR" && b.GetName() != "OpusDecoder" {
			// 	log.Printf("**%s** Process packet. turn_seq=%d", b.GetName(), packet.TurnSeq)
			// }
			// handle data
			if !b.ignoreTurn && packet.TurnSeq < b.curTurnSeq {
				logger.Error("**%s** Drop packet. packet turn_seq=%d, cur_turn_seq=%d", b.GetName(), packet.TurnSeq, b.curTurnSeq)
				// drop current packet
				b.UpdateDroppedStatus()
				continue
			}
			if b.process != nil {
				b.process(packet)
			}
		}
	}
}

func (b *BaseComponent) GetUseInterrupt() bool {
	return b.useInterrupt
}

func (b *BaseComponent) SetUseInterrupt(useInterrupt bool) {
	b.useInterrupt = useInterrupt
}

func (b *BaseComponent) GetIgnoreTurn() bool {
	return b.ignoreTurn
}

func (b *BaseComponent) SetIgnoreTurn(ignore bool) {
	b.ignoreTurn = ignore
}

func (b *BaseComponent) SetInputChan(ch chan Packet) {
	b.inputChan = ch
}

// SetProcess 设置组件的处理函数
func (b *BaseComponent) SetProcess(process func(Packet)) {
	b.process = process
}

// GetOutputChan 获取输出通道
func (b *BaseComponent) GetOutputChan() chan Packet {
	return b.outputChan
}

func (b *BaseComponent) GetCurTurnSeq() int {
	return b.curTurnSeq
}
func (b *BaseComponent) IncrTurnSeq() {
	b.curTurnSeq++
}

func (b *BaseComponent) SetTurnStartTs(turnStartTs int64) {
	b.turnStartTs = turnStartTs
}

func (b *BaseComponent) GetTurnStartTs() int64 {
	return b.turnStartTs
}

func (b *BaseComponent) SetCurTurnSeq(turnSeq int) {
	b.curTurnSeq = turnSeq
}

func (b *BaseComponent) GetSeq() int {
	return b.seq
}

func (b *BaseComponent) IncrSeq() {
	b.seq++
}

// GetHealth 实现 Component 接口
func (b *BaseComponent) GetHealth() ComponentHealth {
	b.healthLock.RLock()
	defer b.healthLock.RUnlock()

	// 更新队列大小
	b.health.InputQueueSize = len(b.inputChan)
	b.health.OutputQueueSize = len(b.outputChan)
	b.health.LastUpdateTime = time.Now()

	return b.health
}

// UpdateHealth 实现 Component 接口
func (b *BaseComponent) UpdateHealth(health ComponentHealth) {
	b.healthLock.Lock()
	defer b.healthLock.Unlock()
	b.health = health
}

// ForwardPacket 转发数据包到输出通道
func (b *BaseComponent) ForwardPacket(packet Packet) {
	// if b.GetName() != "Resampler_48000Hz_2Ch->16000Hz_1Ch" {
	// 	log.Printf("**%s** Forward packet. turn_seq=%d", b.GetName(), packet.TurnSeq)
	// }
	outChan := b.GetOutputChan()
	if outChan != nil {
		select {
		case outChan <- packet:
		default:
			logger.Error("%s: output channel full, dropping packet", b.name)
			b.UpdateDroppedStatus()
		}
	}
}

// SendPacket 发送新的数据包到输出通道
func (b *BaseComponent) SendPacket(data interface{}, src interface{}) {
	outChan := b.GetOutputChan()
	if outChan != nil {
		select {
		case outChan <- Packet{
			Data:    data,
			Seq:     b.seq,
			Src:     src,
			TurnSeq: b.curTurnSeq,
		}:
		default:
			logger.Error("%s: output channel full, dropping packet", b.name)
			b.UpdateDroppedStatus()
		}
	}
	b.IncrSeq()
}

// HandleCommandPacket 处理指令包
func (b *BaseComponent) HandleCommandPacket(packet Packet) bool {
	// handle command
	b.handlersLock.RLock()
	handler, exists := b.commandHandlers[packet.Command]
	b.handlersLock.RUnlock()
	if exists {
		handler(packet)
		return true
	}

	return false
}

// UpdateErrorStatus 更新错误状态
func (b *BaseComponent) UpdateErrorStatus(err error) {
	b.healthLock.Lock()
	defer b.healthLock.Unlock()
	b.health.LastError = err
	b.health.LastErrorTime = time.Now()
}

// UpdateDroppedStatus 更新丢包状态
func (b *BaseComponent) UpdateDroppedStatus() {
	b.healthLock.Lock()
	defer b.healthLock.Unlock()
	b.health.DroppedCount++
}

// HandleUnsupportedData 处理不支持的数据类型
func (b *BaseComponent) HandleUnsupportedData(data interface{}) {
	err := fmt.Errorf("%s: unsupported data type: %T", b.name, data)
	logger.Error("%v", err)
	b.UpdateErrorStatus(err)
}

// GetName 获取组件名称
func (b *BaseComponent) GetName() string {
	return b.name
}

// GetStopCh 获取停止通道
func (b *BaseComponent) GetStopCh() chan struct{} {
	return b.stopCh
}

// Connect 连接到下一个组件，返回下一个组件以支持链式调用
func (b *BaseComponent) Connect(next Component) Component {
	// 直接将当前组件的输出通道设置为下一个组件的输入通道
	logger.Info("Connect component %s[out cap: %d] to %s",
		b.GetName(),
		cap(b.GetOutputChan()),
		next.(interface{ GetName() string }).GetName(),
	)

	next.SetInputChan(b.GetOutputChan())
	return next
}

// ComponentAdapter 用于将现有的基于函数调用的组件适配到新的基于 channel 的接口
type ComponentAdapter struct {
	*BaseComponent
	component Component    // 被适配的原始组件
	output    func(Packet) // 原始组件的输出函数
}

// NewComponentAdapter 创建一个新的组件适配器
func NewComponentAdapter(component Component, bufferSize int) *ComponentAdapter {
	adapter := &ComponentAdapter{
		BaseComponent: NewBaseComponent("", bufferSize),
		component:     component,
	}

	// 设置适配器的处理函数
	adapter.process = adapter.handlePacket

	// 保存原始组件的输出函数
	component.SetOutput(func(p Packet) {
		if adapter.outputChan != nil {
			select {
			case adapter.outputChan <- p:
			default:
				logger.Error("ComponentAdapter: output channel full, dropping packet")
			}
		}
	})

	return adapter
}

// handlePacket 处理输入的数据包
func (a *ComponentAdapter) handlePacket(packet Packet) {
	// 调用原始组件的处理方法
	a.component.Process(packet)
}

// GetID 代理到原始组件的 GetID 方法
func (a *ComponentAdapter) GetID() interface{} {
	return a.component.GetID()
}

// WrapExistingComponent 辅助函数，用于包装现有组件
func WrapExistingComponent(component Component) Component {
	return NewComponentAdapter(component, 100)
}

// Process 实现 Component 接口
func (a *ComponentAdapter) Process(packet Packet) {
	// 将数据包发送到输入通道
	select {
	case a.inputChan <- packet:
	default:
		logger.Error("ComponentAdapter: input channel full, dropping packet")
	}
}

// SetOutput 实现 Component 接口
func (a *ComponentAdapter) SetOutput(output func(Packet)) {
	a.output = output
}
