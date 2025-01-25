# StramLink: A Voice Agent with WebRTC

A real-time voice interaction system that combines WebRTC, Speech Recognition (ASR), Large Language Models (LLM), and Text-to-Speech (TTS) capabilities.

## Features

- **WebRTC Integration**: Real-time audio streaming with WebRTC support
- **Speech Recognition**: Tencent Cloud ASR integration for accurate speech-to-text conversion
- **Language Model**: Integration with DeepSeek API for natural language processing
- **Text-to-Speech**: Tencent Cloud TTS for high-quality speech synthesis
- **Pipeline Architecture**: Flexible component-based pipeline for audio processing
- **Audio Processing**:
  - Sample rate conversion (resampling)
  - Channel conversion (mono/stereo)
  - Opus codec support
  - PCM/WAV format support

## Architecture

### Core Components

1. **Pipeline System**
   - Modular component architecture
   - Asynchronous processing with channels
   - Health monitoring and error handling
   - Turn-based conversation management

2. **Audio Processing**
   - File audio source/sink
   - WebRTC audio handling
   - Audio format conversion
   - Audio dumping capabilities

3. **Voice Processing**
   - ASR: Speech-to-text conversion
   - LLM: Natural language understanding and response generation
   - TTS: Text-to-speech synthesis

### Component Interfaces

- `Component`: Base interface for all pipeline components
- `Source`: Audio input interface
- `Sink`: Audio output interface
- `AudioProcessor`: Audio processing interface

## Setup

### Prerequisites

- Go 1.19 or later
- Tencent Cloud account with ASR and TTS services enabled
- DeepSeek API access

### Environment Variables

Create a `.env` file with the following configurations:

```env
SILICON_API_KEY='your-deepseek-api-key'
TENCENTTTS_APP_ID="your-tencent-appid"
TENCENTTTS_SECRET_ID="your-tencent-secret-id"
TENCENTTTS_SECRET_KEY="your-tencent-secret-key"
TENCENTASR_APP_ID="your-tencent-appid"
TENCENTASR_SECRET_ID="your-tencent-secret-id"
TENCENTASR_SECRET_KEY="your-tencent-secret-key"
```

### Installation

1. Clone the repository:
   ```bash
   git clone [repository-url]
   ```

2. Install dependencies:
   ```bash
   go mod download
   ```

3. Build the project:
   ```bash
   go build
   ```

## Usage

### Running Tests

```bash
go test ./... -v
```

### Audio Format Support

- Input formats: PCM, WAV, Opus
- Output formats: PCM, WAV, Opus, MP3
- Sample rates: 8kHz, 16kHz, 48kHz
- Channels: Mono, Stereo

### Example Usage

```go
// Create a new voice agent
agent := NewVoiceAgent(config, source, sink, processor)

// Start the agent
err := agent.Start()
if err != nil {
    log.Fatal(err)
}

// Process audio
// ... (audio processing)

// Stop the agent
agent.Stop()
```

## Development

### Adding New Components

1. Implement the `Component` interface
2. Add necessary audio processing logic
3. Register the component in the pipeline

### Pipeline Configuration

The pipeline can be configured with different components:

```go
pipe := pipeline.NewPipelineWithSource(source)
components := flux.GenComponents(inputChain, outputChain, 
    asr, turnManager, llm, tts)
pipe.Connect(components...)
```

## License

[License Type] - See LICENSE file for details

## Contributing

1. Fork the repository
2. Create your feature branch
3. Commit your changes
4. Push to the branch
5. Create a new Pull Request

