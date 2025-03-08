package dumper

import (
	"fmt"
	"os"
	"path/filepath"
	"streamlink/internal/protocol/wav"
	"streamlink/pkg/logger"
	"streamlink/pkg/logic/pipeline"
)

// WAVDumper 结构体 (实现 Component 接口)
type WAVDumper struct {
	*pipeline.BaseComponent
	file     *os.File
	fileName string
	writer   *wav.Writer
	format   wav.WAVFormat
	seq      int
	dataSize uint32
}

// NewWAVDumper 创建新的 WAV 转储器
// ffplay -ar 48000 -ac 2 test.wav
func NewWAVDumper(fileName string, sampleRate uint32, channels uint16) (*WAVDumper, error) {
	// 确保目录存在
	dir := filepath.Dir(fileName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %v", err)
	}

	// 创建 WAV 格式
	format := wav.WAVFormat{
		AudioFormat:   1, // PCM
		NumChannels:   channels,
		SampleRate:    sampleRate,
		BitsPerSample: 16,
		BlockAlign:    channels * 2,                      // channels * (BitsPerSample / 8)
		ByteRate:      sampleRate * uint32(channels) * 2, // SampleRate * NumChannels * (BitsPerSample / 8)
	}

	// 创建 WAV 写入器
	file, err := os.Create(fileName)
	if err != nil {
		return nil, fmt.Errorf("failed to create WAV file: %v", err)
	}

	writer, err := wav.NewWriter(file, format)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to create WAV writer: %v", err)
	}

	dumper := &WAVDumper{
		BaseComponent: pipeline.NewBaseComponent("WAVDumper", 100),
		file:          file,
		fileName:      fileName,
		writer:        writer,
		format:        format,
		seq:           0,
		dataSize:      0,
	}

	// 设置处理函数
	dumper.BaseComponent.SetProcess(dumper.processPacket)
	dumper.RegisterCommandHandler(pipeline.PacketCommandInterrupt, dumper.handleInterrupt)

	return dumper, nil
}

func (d *WAVDumper) handleInterrupt(packet pipeline.Packet) {
	logger.Info("**%s** Received interrupt command for turn %d", d.GetName(), packet.TurnSeq)
	d.SetCurTurnSeq(packet.TurnSeq)

	d.ForwardPacket(packet)
}

// processPacket 处理输入的数据包
func (d *WAVDumper) processPacket(packet pipeline.Packet) {
	// 处理指令
	if d.HandleCommandPacket(packet) {
		return
	}

	if data, ok := packet.Data.([]int16); ok {
		// 写入 WAV 数据
		if err := d.writer.WriteSamples(data); err != nil {
			logger.Error("**%s** Failed to write WAV data: %v", d.GetName(), err)
			d.UpdateErrorStatus(err)
		}

		// 更新数据大小
		d.dataSize += uint32(len(data) * 2) // 每个采样点 2 字节

		// 转发数据包
		d.ForwardPacket(packet)
		d.seq++
	} else {
		d.HandleUnsupportedData(packet.Data)
	}
}

// GetID 实现 Component 接口
func (d *WAVDumper) GetID() interface{} {
	return d.seq
}

// Stop 实现 Component 接口，扩展基础组件的 Stop 方法
func (d *WAVDumper) Stop() {
	d.BaseComponent.Stop()
	if d.writer != nil {
		d.writer.Close()
		d.writer = nil
	}
	if d.file != nil {
		d.file.Close()
		d.file = nil
	}
}

// 为了向后兼容，保留这些方法
func (d *WAVDumper) Process(packet pipeline.Packet) {
	select {
	case d.GetInputChan() <- packet:
	default:
		logger.Error("**%s** Input channel full, dropping packet", d.GetName())
	}
}

func (d *WAVDumper) SetOutput(output func(pipeline.Packet)) {
	outChan := make(chan pipeline.Packet, 100)
	d.SetOutputChan(outChan)
	go func() {
		for packet := range d.GetOutputChan() {
			if output != nil {
				output(packet)
			}
		}
	}()
}

// Start 实现 Component 接口
func (d *WAVDumper) Start() error {
	d.BaseComponent.Start()
	return nil
}

// GetHealth 实现 Component 接口
func (d *WAVDumper) GetHealth() pipeline.ComponentHealth {
	return d.BaseComponent.GetHealth()
}

// UpdateHealth 实现 Component 接口
func (d *WAVDumper) UpdateHealth(health pipeline.ComponentHealth) {
	d.BaseComponent.UpdateHealth(health)
}

// GetDataSize 获取已写入的数据大小
func (d *WAVDumper) GetDataSize() uint32 {
	return d.dataSize
}

// GetFormat 获取 WAV 格式信息
func (d *WAVDumper) GetFormat() wav.WAVFormat {
	return d.format
}
