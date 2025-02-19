package flux

import (
	"streamlink/pkg/logic/pipeline"
)

// ProcessingChain 表示一个处理链的结果
type ProcessingChain struct {
	First pipeline.Component   // 处理链的第一个组件
	Last  pipeline.Component   // 处理链的最后一个组件
	All   []pipeline.Component // 处理链中的所有组件
}

func GenComponents(inputChain ProcessingChain, outputChain ProcessingChain, components ...pipeline.Component) []pipeline.Component {
	return append(inputChain.All, append(components, outputChain.All...)...)
}

// AudioProcessor 音频处理器接口
type AudioProcessor interface {
	ProcessInput(source pipeline.Component) ProcessingChain // 处理输入音频
	ProcessOutput(sink pipeline.Component) ProcessingChain  // 处理输出音频
}

// defaultAudioProcessor 默认的音频处理器实现，不做任何处理
type defaultAudioProcessor struct{}

// NewDefaultAudioProcessor 创建一个默认的音频处理器
func NewDefaultAudioProcessor() AudioProcessor {
	return &defaultAudioProcessor{}
}

// ProcessInput 直接返回源组件
func (p *defaultAudioProcessor) ProcessInput(source pipeline.Component) ProcessingChain {
	return ProcessingChain{
		First: nil,
		Last:  nil,
		All:   []pipeline.Component{},
	}
}

// ProcessOutput 直接返回接收组件
func (p *defaultAudioProcessor) ProcessOutput(sink pipeline.Component) ProcessingChain {
	return ProcessingChain{
		First: sink,
		Last:  sink,
		All:   []pipeline.Component{sink},
	}
}
