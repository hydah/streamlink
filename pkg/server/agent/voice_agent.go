package agent

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"voiceagent/internal/config"
	"voiceagent/pkg/logic/flux"
	"voiceagent/pkg/logic/llm"
	"voiceagent/pkg/logic/pipeline"
	"voiceagent/pkg/logic/stt"
	"voiceagent/pkg/logic/tts"
)

// VoiceAgent 处理语音对话的代理
type VoiceAgent struct {
	config      *config.Config
	source      flux.Source
	sink        flux.Sink
	pipeline    *pipeline.Pipeline
	asr         *stt.TencentAsr
	llm         *llm.DeepSeek
	tts         *tts.TencentTTS2
	stopCh      chan struct{}
	processor   flux.AudioProcessor
	turnManager *pipeline.TurnManager
}

// NewVoiceAgent 创建一个新的语音代理
func NewVoiceAgent(config *config.Config, source flux.Source, sink flux.Sink, processor flux.AudioProcessor) *VoiceAgent {
	// 如果没有提供处理器，使用默认处理器
	if processor == nil {
		processor = flux.NewDefaultAudioProcessor()
	}

	// 创建 ASR 实例
	appIDStr := config.ASR.TencentASR.AppID
	if appIDStr != "" && appIDStr[0] == '$' {
		appIDStr = os.Getenv(appIDStr[1:])
	}
	secretID := config.ASR.TencentASR.SecretID
	if secretID != "" && secretID[0] == '$' {
		secretID = os.Getenv(secretID[1:])
	}
	secretKey := config.ASR.TencentASR.SecretKey
	if secretKey != "" && secretKey[0] == '$' {
		secretKey = os.Getenv(secretKey[1:])
	}
	asr := stt.NewTencentAsr(
		appIDStr,
		secretID,
		secretKey,
		config.ASR.TencentASR.EngineModelType,
		config.ASR.TencentASR.SliceSize,
	)

	// 创建 LLM 实例
	apiKey := config.LLM.OpenAI.APIKey
	if apiKey != "" && apiKey[0] == '$' {
		apiKey = os.Getenv(apiKey[1:])
	}
	baseURL := config.LLM.OpenAI.BaseURL
	if baseURL != "" && baseURL[0] == '$' {
		baseURL = os.Getenv(baseURL[1:])
	}
	llmInstance := llm.NewDeepSeek(
		apiKey,
		baseURL,
	)

	// 创建 TTS 实例
	appIDStr = config.TTS.TencentTTS.AppID
	if appIDStr != "" && appIDStr[0] == '$' {
		appIDStr = os.Getenv(appIDStr[1:])
	}
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		log.Printf("Failed to parse appID: %v", err)
		appID = 0
	}
	secretID = config.TTS.TencentTTS.SecretID
	if secretID != "" && secretID[0] == '$' {
		secretID = os.Getenv(secretID[1:])
	}
	secretKey = config.TTS.TencentTTS.SecretKey
	if secretKey != "" && secretKey[0] == '$' {
		secretKey = os.Getenv(secretKey[1:])
	}
	tts := tts.NewTencentTTS2(
		appID,
		secretID,
		secretKey,
		config.TTS.TencentTTS.VoiceType,
		config.TTS.TencentTTS.Codec,
	)

	return &VoiceAgent{
		config:    config,
		source:    source,
		sink:      sink,
		asr:       asr,
		llm:       llmInstance,
		tts:       tts,
		stopCh:    make(chan struct{}),
		processor: processor,
	}
}

// SetAudioProcessor 设置音频处理器, 不允许设置为nil
func (v *VoiceAgent) SetAudioProcessor(processor flux.AudioProcessor) error {
	if processor == nil {
		return fmt.Errorf("processor is nil")
	}
	v.processor = processor
	return nil
}

// Start 启动语音代理
func (v *VoiceAgent) Start() error {
	// 创建 Pipeline
	pipe := pipeline.NewPipelineWithSource(v.source)

	// 创建 TurnManager
	v.turnManager = pipeline.NewTurnManager(pipeline.DefaultTurnManagerConfig())
	v.turnManager.SetIgnoreTurn(true)

	// 获取基础组件
	components := flux.GenComponents(v.processor.ProcessInput(v.source),
		v.processor.ProcessOutput(v.sink),
		v.asr, v.turnManager, v.llm, v.tts)

	if err := pipe.Connect(components...); err != nil {
		log.Println("Failed to connect output chain:", err)
		return err
	}

	// 启动 pipeline
	if err := pipe.Start(); err != nil {
		log.Println("Failed to start pipeline:", err)
		return err
	}

	v.pipeline = pipe

	return nil
}

// Stop 停止语音代理
func (v *VoiceAgent) Stop() {
	select {
	case <-v.stopCh:
		return
	default:
		close(v.stopCh)
		v.asr.Stop()
	}
}

// Interrupt 发送打断指令
func (v *VoiceAgent) Interrupt() {
	if v.pipeline != nil {
		// 使用 0 作为 turnSeq，让 TurnManager 自己管理序号
		v.pipeline.SendInterrupt(0)
	}
}

// GetCurrentTurn 获取当前轮次信息
func (v *VoiceAgent) GetCurrentTurn() *pipeline.TurnInfo {
	if v.turnManager != nil {
		return v.turnManager.GetCurrentTurn()
	}
	return nil
}

// GetPreviousTurn 获取上一轮次信息
func (v *VoiceAgent) GetPreviousTurn() *pipeline.TurnInfo {
	if v.turnManager != nil {
		return v.turnManager.GetPreviousTurn()
	}
	return nil
}
