package dumper

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"voiceagent/pkg/logic/pipeline"
)

// PCMDumper 结构体 (实现 Component 接口)
type PCMDumper struct {
	*pipeline.BaseComponent
	file     *os.File
	fileName string
	seq      int
}

// ffplay -ar 48000 -ch_layout stereo  -f s16le -i input.pcm
// ffplay -ar 16000 -ch_layout mono  -f s16le -i output.pcm
func NewPCMDumper(fileName string) (*PCMDumper, error) {
	// 确保 pcm_dumps 目录存在
	dir := filepath.Dir(fileName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create pcm_dumps directory: %v", err)
	}

	// 创建 PCM 文件
	file, err := os.OpenFile(fileName,
		os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create PCM file: %v", err)
	}

	dumper := &PCMDumper{
		BaseComponent: pipeline.NewBaseComponent("PCMDumper", 100),
		file:          file,
		fileName:      fileName,
		seq:           0,
	}

	// 设置处理函数
	dumper.BaseComponent.SetProcess(dumper.processPacket)
	dumper.RegisterCommandHandler(pipeline.PacketCommandInterrupt, dumper.handleInterrupt)
	return dumper, nil
}

func (d *PCMDumper) handleInterrupt(packet pipeline.Packet) {
	log.Printf("**%s** Received interrupt command for turn %d", d.GetName(), packet.TurnSeq)
	d.SetCurTurnSeq(packet.TurnSeq)

	d.ForwardPacket(packet)
}

// processPacket 处理输入的数据包
func (d *PCMDumper) processPacket(packet pipeline.Packet) {
	// 处理指令
	if d.HandleCommandPacket(packet) {
		return
	}

	if data, ok := packet.Data.([]int16); ok {
		// 将 PCM 数据写入文件
		pcmBytes := make([]byte, len(data)*2)
		for i, sample := range data {
			pcmBytes[i*2] = byte(sample)
			pcmBytes[i*2+1] = byte(sample >> 8)
		}
		if _, err := d.file.Write(pcmBytes); err != nil {
			log.Printf("**%s** Failed to write PCM data: %v", d.GetName(), err)
			d.UpdateErrorStatus(err)
		}

		// 转发数据包
		d.ForwardPacket(packet)
		d.seq++
	} else {
		d.HandleUnsupportedData(packet.Data)
	}
}

// GetID 实现 Component 接口
func (d *PCMDumper) GetID() interface{} {
	return d.seq
}

// Stop 实现 Component 接口，扩展基础组件的 Stop 方法
func (d *PCMDumper) Stop() {
	d.BaseComponent.Stop()
	d.file.Close()
}

// 为了向后兼容，保留这些方法
func (d *PCMDumper) Process(packet pipeline.Packet) {
	select {
	case d.GetInputChan() <- packet:
	default:
		fmt.Printf("PCMDumper: input channel full, dropping packet\n")
	}
}

func (d *PCMDumper) SetOutput(output func(pipeline.Packet)) {
	// 创建一个适配器函数，将 channel 输出转换为函数调用
	d.SetOutputChan(make(chan pipeline.Packet, 100))
	go func() {
		for packet := range d.GetOutputChan() {
			if output != nil {
				output(packet)
			}
		}
	}()
}

// Start 实现 Component 接口
func (d *PCMDumper) Start() error {
	d.BaseComponent.Start()
	return nil
}

// GetHealth 实现 Component 接口
func (d *PCMDumper) GetHealth() pipeline.ComponentHealth {
	return d.BaseComponent.GetHealth()
}

// UpdateHealth 实现 Component 接口
func (d *PCMDumper) UpdateHealth(health pipeline.ComponentHealth) {
	d.BaseComponent.UpdateHealth(health)
}
