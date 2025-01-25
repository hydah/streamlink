package dumper

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"voiceagent/pkg/logic/codec"
	"voiceagent/pkg/logic/pipeline"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
)

// OggDumper 结构体 (实现 Component 接口)
type OggDumper struct {
	*pipeline.BaseComponent
	oggFile *oggwriter.OggWriter
	seq     int
}

func NewOggDumper(sampleRateIn uint32, channelsIn uint16, fileName string) (*OggDumper, error) {
	// 确保目录存在
	dir := filepath.Dir(fileName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %v", dir, err)
	}

	// 创建 OGG 文件
	oggFile, err := oggwriter.New(fileName, sampleRateIn, channelsIn)
	if err != nil {
		return nil, fmt.Errorf("failed to create OGG file: %v", err)
	}

	d := &OggDumper{
		BaseComponent: pipeline.NewBaseComponent("OggDumper", 100),
		oggFile:       oggFile,
		seq:           0,
	}

	// 设置处理函数
	d.BaseComponent.SetProcess(d.processPacket)
	d.RegisterCommandHandler(pipeline.PacketCommandInterrupt, d.handleInterrupt)

	return d, nil
}

func (d *OggDumper) handleInterrupt(packet pipeline.Packet) {
	log.Printf("**%s** Received interrupt command for turn %d", d.GetName(), packet.TurnSeq)

	d.SetCurTurnSeq(packet.TurnSeq)

	d.ForwardPacket(packet)
}

// processPacket 处理输入的数据包
func (d *OggDumper) processPacket(packet pipeline.Packet) {
	// 处理指令
	if d.HandleCommandPacket(packet) {
		return
	}

	switch data := packet.Data.(type) {
	case *rtp.Packet:
		if err := d.oggFile.WriteRTP(data); err != nil {
			log.Printf("**%s** Failed to write RTP to OGG: %v", d.GetName(), err)
			d.UpdateErrorStatus(err)
			return
		}
		// 转发数据包
		d.SendPacket(data, d)
	case codec.AudioPacket:
		// 创建 RTP 包
		rtpPacket := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    111, // Opus 的默认 payload type
				SequenceNumber: uint16(d.seq),
				Timestamp:      data.Timestamp(),
				SSRC:           0,
			},
			Payload: data.Payload(),
		}

		if err := d.oggFile.WriteRTP(rtpPacket); err != nil {
			log.Printf("**%s** Failed to write AudioPacket to OGG: %v", d.GetName(), err)
			d.UpdateErrorStatus(err)
			return
		}
		// 转发数据包
		d.SendPacket(data, d)
	default:
		d.HandleUnsupportedData(packet.Data)
	}
}

// GetID 实现 Component 接口
func (d *OggDumper) GetID() interface{} {
	return d.GetSeq()
}

// Stop 实现 Component 接口，扩展基础组件的 Stop 方法
func (d *OggDumper) Stop() {
	d.BaseComponent.Stop()
	if d.oggFile != nil {
		d.oggFile.Close()
		d.oggFile = nil
	}
}

// 为了向后兼容，保留这些方法
func (d *OggDumper) Process(packet pipeline.Packet) {
	select {
	case d.GetInputChan() <- packet:
	default:
		log.Printf("**%s** Input channel full, dropping packet", d.GetName())
	}
}

func (d *OggDumper) SetOutput(output func(pipeline.Packet)) {
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

// Close 关闭 OGG 文件
func (d *OggDumper) Close() error {
	if d.oggFile != nil {
		return d.oggFile.Close()
	}
	return nil
}

// Start 实现 Component 接口
func (d *OggDumper) Start() error {
	d.BaseComponent.Start()
	return nil
}

// GetHealth 实现 Component 接口
func (d *OggDumper) GetHealth() pipeline.ComponentHealth {
	return d.BaseComponent.GetHealth()
}

// UpdateHealth 实现 Component 接口
func (d *OggDumper) UpdateHealth(health pipeline.ComponentHealth) {
	d.BaseComponent.UpdateHealth(health)
}
