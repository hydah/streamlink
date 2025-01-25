package flux

import (
	"voiceagent/pkg/logic/pipeline"
)

// Source represents an audio source component
type Source interface {
	pipeline.Component
}

// Sink represents an audio sink component
type Sink interface {
	pipeline.Component
}
