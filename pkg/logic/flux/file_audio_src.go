package flux

import (
	"fmt"
	"io"
	"log"
	"os"
	"streamlink/internal/protocol/wav"
	"streamlink/pkg/logic/codec"
	"streamlink/pkg/logic/pipeline"
	"time"
)

// FileAudioSource 结构体 (实现 Source 接口)
type FileAudioSource struct {
	*pipeline.BaseComponent
	filePath   string
	sampleRate int
	reader     *wav.Reader
	file       *os.File
	seq        int
	stopCh     chan struct{}
	frameSize  int // 每帧的采样点数
	isRunning  bool
}

// NewFileAudioSource 创建新的文件音频源
func NewFileAudioSource(filePath string, sampleRate int) Source {
	source := &FileAudioSource{
		BaseComponent: pipeline.NewBaseComponent("FileAudioSource", 100),
		filePath:      filePath,
		sampleRate:    sampleRate,
		seq:           0,
		stopCh:        make(chan struct{}),
		frameSize:     960, // 20ms at 48kHz
		isRunning:     false,
	}

	return source
}

// Start 启动音频源
func (s *FileAudioSource) Start() error {
	if s.isRunning {
		return nil
	}
	log.Printf("Start component: %s", s.GetName())

	// 打开 WAV 文件
	file, err := os.Open(s.filePath)
	if err != nil {
		return fmt.Errorf("failed to open WAV file: %v", err)
	}
	s.file = file

	// 创建 WAV 读取器
	reader, err := wav.NewReader(file)
	if err != nil {
		file.Close()
		return fmt.Errorf("failed to create WAV reader: %v", err)
	}
	s.reader = reader

	// 验证音频格式
	format := reader.GetFormat()
	if int(format.SampleRate) != s.sampleRate {
		return fmt.Errorf("unexpected sample rate: %d (expected %d)", format.SampleRate, s.sampleRate)
	}

	s.isRunning = true
	go s.readLoop()
	return nil
}

// readLoop 循环读取音频数据
func (s *FileAudioSource) readLoop() {
	defer func() {
		s.isRunning = false
		if s.reader != nil {
			s.reader.Close()
			s.reader = nil
		}
		if s.file != nil {
			s.file.Close()
			s.file = nil
		}
	}()

	// 分配缓冲区
	pcmBuf := make([]int16, s.frameSize*int(s.reader.GetFormat().NumChannels))

	for {
		select {
		case <-s.stopCh:
			return
		default:
			// 读取 PCM 数据
			n, err := s.reader.ReadSamples(pcmBuf)
			if err != nil && err != io.EOF {
				log.Printf("**%s** Failed to read WAV data: %v", s.GetName(), err)
				s.UpdateErrorStatus(err)
				return
			}

			// 如果读取的数据不足一帧，用静音填充
			if n < len(pcmBuf) {
				for i := n; i < len(pcmBuf); i++ {
					pcmBuf[i] = 0
				}
			}

			// change pcmBuf to byte slice
			byteBuf := make([]byte, len(pcmBuf)*2)
			for i, v := range pcmBuf {
				byteBuf[i*2] = byte(v)
				byteBuf[i*2+1] = byte(v >> 8)
			}

			// 发送数据包
			s.SendPacket(codec.NewRTPAudioPacket(byteBuf, uint32(s.seq)), s)

			// 控制发送速度，模拟实时音频流
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// Stop 停止音频源
func (s *FileAudioSource) Stop() {
	if !s.isRunning {
		return
	}
	close(s.stopCh)
	s.BaseComponent.Stop()
}

// GetID 实现 Component 接口
func (s *FileAudioSource) GetID() interface{} {
	return s.GetSeq()
}

// Process 实现 Component 接口
func (s *FileAudioSource) Process(packet pipeline.Packet) {
	// 音频源不处理输入
}

// SetOutput 实现 Component 接口
func (s *FileAudioSource) SetOutput(output func(pipeline.Packet)) {
	outChan := make(chan pipeline.Packet, 100)
	s.SetOutputChan(outChan)
	go func() {
		for packet := range s.GetOutputChan() {
			if output != nil {
				output(packet)
			}
		}
	}()
}

// GetHealth 实现 Component 接口
func (s *FileAudioSource) GetHealth() pipeline.ComponentHealth {
	return s.BaseComponent.GetHealth()
}

// UpdateHealth 实现 Component 接口
func (s *FileAudioSource) UpdateHealth(health pipeline.ComponentHealth) {
	s.BaseComponent.UpdateHealth(health)
}
