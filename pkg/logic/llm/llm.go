package llm

// TextComponent 接口定义了文本处理组件的基本行为
type TextComponent interface {
	ProcessText(text string) string
	SetOutput(func(string) string)
}

// TextPipeline 处理文本数据的管道
type TextPipeline struct {
	components []TextComponent
}

// NewTextPipeline 创建新的文本处理管道
func NewTextPipeline() *TextPipeline {
	return &TextPipeline{}
}

// Process 处理文本数据
func (p *TextPipeline) Process(data interface{}) {
	if text, ok := data.(string); ok {
		if len(p.components) > 0 {
			p.components[0].ProcessText(text)
		}
	}
}

// Connect 连接文本处理组件
func (p *TextPipeline) Connect(components ...TextComponent) {
	if len(components) == 0 {
		return
	}

	p.components = append(p.components, components...)

	// 连接组件
	for i := 0; i < len(components)-1; i++ {
		components[i].SetOutput(components[i+1].ProcessText)
	}
}
