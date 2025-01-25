package testcase

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
	"voiceagent/internal/protocol/wav"

	"github.com/hraban/opus"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
)

const (
	serverURL = "http://localhost:8080/whip"
	frameSize = 960 // 20ms at 48kHz
)

// AudioConfig 音频配置
type AudioConfig struct {
	FilePath   string
	SampleRate int
	Channels   int
}

// WAVFormat WAV 文件格式信息
type WAVFormat struct {
	AudioFormat   uint16
	NumChannels   uint16
	SampleRate    uint32
	ByteRate      uint32
	BlockAlign    uint16
	BitsPerSample uint16
}

// WHIPClient 表示一个 WHIP 客户端
type WHIPClient struct {
	peerConnection *webrtc.PeerConnection
	audioTrack     *webrtc.TrackLocalStaticSample
	location       string // WHIP 资源的位置
	oggWriter      *oggwriter.OggWriter
}

// NewWHIPClient 创建一个新的 WHIP 客户端
func NewWHIPClient() (*WHIPClient, error) {
	// 创建一个新的 RTCPeerConnection
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	// 设置 MediaEngine
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("failed to register default codecs: %v", err)
	}

	// 创建 API
	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))

	// 使用 API 创建 PeerConnection
	peerConnection, err := api.NewPeerConnection(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create peer connection: %v", err)
	}

	// 创建音频轨道
	audioTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		"pion",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create audio track: %v", err)
	}

	// 添加音频轨道到 peer connection
	if _, err = peerConnection.AddTrack(audioTrack); err != nil {
		return nil, fmt.Errorf("failed to add track: %v", err)
	}

	return &WHIPClient{
		peerConnection: peerConnection,
		audioTrack:     audioTrack,
	}, nil
}

// Connect 连接到 WHIP 服务器
func (c *WHIPClient) Connect() error {
	// 创建 offer
	offer, err := c.peerConnection.CreateOffer(nil)
	if err != nil {
		return fmt.Errorf("failed to create offer: %v", err)
	}

	// 设置本地描述
	if err = c.peerConnection.SetLocalDescription(offer); err != nil {
		return fmt.Errorf("failed to set local description: %v", err)
	}

	// 发送 offer 到服务器
	offerJSON, err := json.Marshal(offer)
	if err != nil {
		return fmt.Errorf("failed to marshal offer: %v", err)
	}

	resp, err := http.Post(serverURL, "application/json", bytes.NewBuffer(offerJSON))
	if err != nil {
		return fmt.Errorf("failed to send offer: %v", err)
	}
	defer resp.Body.Close()

	// 保存 location header
	c.location = resp.Header.Get("Location")

	// 读取并设置远程描述
	var answer webrtc.SessionDescription
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		return fmt.Errorf("failed to decode answer: %v", err)
	}

	if err = c.peerConnection.SetRemoteDescription(answer); err != nil {
		return fmt.Errorf("failed to set remote description: %v", err)
	}

	return nil
}

// SendAudioFile 发送音频文件
func (c *WHIPClient) SendAudioFile(config AudioConfig) error {
	// 打开音频文件
	file, err := os.Open(config.FilePath)
	if err != nil {
		return fmt.Errorf("failed to open audio file: %v", err)
	}
	defer file.Close()

	// 创建 WAV 读取器
	wavReader, err := wav.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create WAV reader: %v", err)
	}

	// 验证音频格式
	format := wavReader.GetFormat()
	if int(format.SampleRate) != config.SampleRate {
		return fmt.Errorf("unexpected sample rate: %d (expected %d)", format.SampleRate, config.SampleRate)
	}
	if int(format.NumChannels) != config.Channels {
		return fmt.Errorf("unexpected number of channels: %d (expected %d)", format.NumChannels, config.Channels)
	}

	// 创建 Opus 编码器
	enc, err := opus.NewEncoder(config.SampleRate, config.Channels, opus.AppVoIP)
	if err != nil {
		return fmt.Errorf("failed to create opus encoder: %v", err)
	}

	// 分配缓冲区
	pcmBuf := make([]int16, frameSize*config.Channels)
	opusBuf := make([]byte, 2048)

	for {
		// 读取 PCM 数据
		n, err := wavReader.ReadSamples(pcmBuf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read audio file: %v", err)
		}

		// 如果读取的数据不足一帧，用静音填充
		if n < len(pcmBuf) {
			for i := n; i < len(pcmBuf); i++ {
				pcmBuf[i] = 0
			}
		}

		// 编码为 Opus
		n, err = enc.Encode(pcmBuf, opusBuf)
		if err != nil {
			return fmt.Errorf("failed to encode audio: %v", err)
		}

		// 发送编码后的数据
		if err := c.audioTrack.WriteSample(media.Sample{
			Data:     opusBuf[:n],
			Duration: time.Millisecond * 20,
		}); err != nil {
			return fmt.Errorf("failed to write sample: %v", err)
		}

		time.Sleep(time.Millisecond * 20)
	}

	return nil
}

// ReceiveAudio 开始接收音频并保存到文件
func (c *WHIPClient) ReceiveAudio(filename string, sampleRate, channels int) error {
	var err error
	c.oggWriter, err = oggwriter.New(filename, uint32(sampleRate), uint16(channels))
	if err != nil {
		return fmt.Errorf("failed to create OGG writer: %v", err)
	}

	// 处理远程音轨
	c.peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("Got remote track: %s\n", track.Codec().MimeType)
		for {
			// 读取 RTP 包
			rtp, _, err := track.ReadRTP()
			if err != nil {
				if err == io.EOF {
					return
				}
				log.Printf("Failed to read RTP: %v", err)
				continue
			}

			// 写入 OGG 文件
			if err := c.oggWriter.WriteRTP(rtp); err != nil {
				log.Printf("Failed to write to OGG file: %v", err)
			}
		}
	})

	return nil
}

// Close 关闭连接
func (c *WHIPClient) Close() error {
	if c.oggWriter != nil {
		if err := c.oggWriter.Close(); err != nil {
			log.Printf("Failed to close OGG writer: %v", err)
		}
	}

	if c.location != "" {
		// 发送 DELETE 请求
		req, err := http.NewRequest(http.MethodDelete, "http://localhost:8080"+c.location, nil)
		if err != nil {
			return fmt.Errorf("failed to create delete request: %v", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send delete request: %v", err)
		}
		resp.Body.Close()
	}

	return c.peerConnection.Close()
}
