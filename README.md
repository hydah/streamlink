# StreamLink: WebRTC 实时语音交互系统

StreamLink 是一个实时语音交互系统，它集成了 WebRTC、语音识别（ASR）、大语言模型（LLM）和语音合成（TTS）等功能，实现了流畅的人机语音对话体验。

## 主要特性

- **WebRTC 集成**：支持实时音频流传输
- **语音识别**：集成腾讯云 ASR 服务，提供高精度的语音转文字功能
- **语言模型**：集成 DeepSeek API，提供自然语言处理能力
- **语音合成**：使用腾讯云 TTS 服务，实现高质量的文字转语音
- **流水线架构**：基于组件的灵活流水线处理系统
- **音频处理**：
  - 采样率转换
  - 声道转换（单声道/立体声）
  - Opus 编解码支持
  - PCM/WAV 格式支持

## 系统架构

### 核心组件

1. **流水线系统**
   - 模块化组件架构
   - 基于通道的异步处理
   - 健康监控和错误处理
   - 基于回合的对话管理

2. **音频处理**
   - 文件音频源/接收器
   - WebRTC 音频处理
   - 音频格式转换
   - 音频转储功能

3. **语音处理**
   - ASR：语音转文字
   - LLM：自然语言理解和响应生成
   - TTS：文字转语音

### 组件接口

- `Component`：所有流水线组件的基础接口
- `Source`：音频输入接口
- `Sink`：音频输出接口
- `AudioProcessor`：音频处理接口

## 环境配置

### 前置要求

- Go 1.19 或更高版本
- 腾讯云账号（已开通 ASR 和 TTS 服务）
- DeepSeek API 访问权限

### 环境变量

创建 `.env` 文件并配置以下内容：

```env
SILICON_API_KEY='your-deepseek-api-key'
TENCENTTTS_APP_ID="your-tencent-appid"
TENCENTTTS_SECRET_ID="your-tencent-secret-id"
TENCENTTTS_SECRET_KEY="your-tencent-secret-key"
TENCENTASR_APP_ID="your-tencent-appid"
TENCENTASR_SECRET_ID="your-tencent-secret-id"
TENCENTASR_SECRET_KEY="your-tencent-secret-key"
```

### 安装步骤

1. 克隆仓库：
   ```bash
   git clone [repository-url]
   ```

2. 安装依赖：
   ```bash
   # 安装 Go 依赖
   go mod download

   
   # 安装系统依赖(opus 编解码器)
   # Debian/Ubuntu:
   sudo apt-get install pkg-config libopus-dev libopusfile-dev

   # Mac OS X:
   brew install pkg-config opus opusfile
   ```

3. 构建项目：
   ```bash
   go build
   ```

## 使用说明

### 运行测试

```bash
go test ./... -v
```

### 支持的音频格式

- 输入格式：PCM、WAV、Opus
- 输出格式：PCM、WAV、Opus、MP3
- 采样率：8kHz、16kHz、48kHz
- 声道：单声道、立体声

### 使用示例

```go
// 创建语音代理实例
agent := NewVoiceAgent(config, source, sink, processor)

// 启动代理
err := agent.Start()
if err != nil {
    log.Fatal(err)
}

// 处理音频
// ... (音频处理逻辑)

// 停止代理
agent.Stop()
```

## 开发指南

### 添加新组件

1. 实现 `Component` 接口
2. 添加必要的音频处理逻辑
3. 在流水线中注册组件

### 流水线配置

流水线可以配置不同的组件：

```go
pipe := pipeline.NewPipelineWithSource(source)
components := flux.GenComponents(inputChain, outputChain, 
    asr, turnManager, llm, tts)
pipe.Connect(components...)
```

## 许可证

详见 LICENSE 文件

## 贡献指南

1. Fork 本仓库
2. 创建特性分支
3. 提交更改
4. 推送到分支
5. 创建 Pull Request

# 关于我
我在腾讯云负责TRTC 后台的部分工作。目前在探索 RTC+AI的方向。以下是我的个人微信，欢迎添加好友（备注来意）
![f86a9a0a48722314c6ba3bd79677fdc7](https://github.com/user-attachments/assets/18e09570-c010-4bb7-8bbb-12697bb3e0b8)
