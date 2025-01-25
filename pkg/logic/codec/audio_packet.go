package codec

// AudioPacket 定义了音频数据包的接口
type AudioPacket interface {
	// Payload 返回音频数据
	Payload() []byte
	// Timestamp 返回音频数据的时间戳
	Timestamp() uint32
}

// RTPAudioPacket 是基于 RTP 包的 AudioPacket 实现
type RTPAudioPacket struct {
	payload   []byte
	timestamp uint32
}

// NewRTPAudioPacket 从 RTP 包创建 AudioPacket
func NewRTPAudioPacket(payload []byte, timestamp uint32) *RTPAudioPacket {
	return &RTPAudioPacket{
		payload:   payload,
		timestamp: timestamp,
	}
}

func (p *RTPAudioPacket) Payload() []byte {
	return p.payload
}

func (p *RTPAudioPacket) Timestamp() uint32 {
	return p.timestamp
}
