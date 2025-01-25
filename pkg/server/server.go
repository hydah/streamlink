package server

import (
	"log"
	"net"
	"sync"

	"voiceagent/internal/config"
	"voiceagent/pkg/server/connection"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

type WHIPServer struct {
	api           *webrtc.API
	connections   sync.Map
	webrtcConfig  webrtc.Configuration
	udpMux        ice.UDPMux
	config        *config.Config
	webrtcFactory *connection.WebRTCFactory
}

func NewVoiceAgentServer() *WHIPServer {
	return &WHIPServer{}
}

func (s *WHIPServer) Init(config *config.Config) error {
	s.config = config

	// 1. 创建 UDP 监听器，使用配置中的端口
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   net.IPv4(0, 0, 0, 0),
		Port: config.Server.UDPPort,
	})
	if err != nil {
		return err
	}

	log.Printf("Listening for media on UDP port %d\n", config.Server.UDPPort)

	// 2. 创建 UDP Mux
	udpMux := webrtc.NewICEUDPMux(nil, udpConn)
	s.udpMux = udpMux

	// 3. 配置 SettingEngine
	settingEngine := webrtc.SettingEngine{}
	settingEngine.SetICEUDPMux(udpMux)
	// 设置网络类型为 UDP
	settingEngine.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})

	// settingEngine.SetLite(true) // 启用 ICE Lite 模式

	// 4. 创建 ICE Lite 模式的 WebRTC 配置
	s.webrtcConfig = webrtc.Configuration{
		ICEServers:         []webrtc.ICEServer{},
		ICETransportPolicy: webrtc.ICETransportPolicyAll,
		BundlePolicy:       webrtc.BundlePolicyMaxBundle,
		RTCPMuxPolicy:      webrtc.RTCPMuxPolicyRequire,
		// ICE Lite 模式
		SDPSemantics: webrtc.SDPSemanticsUnifiedPlanWithFallback,
	}

	// 5. 创建 API 实例
	s.api = webrtc.NewAPI(
		webrtc.WithSettingEngine(settingEngine),
	)

	s.webrtcFactory = connection.NewWebRTCFactory(s.api, s.webrtcConfig, s.udpMux)
	return nil
}

func (s *WHIPServer) HandleNewConnection(offer *webrtc.SessionDescription) (*webrtc.SessionDescription, string, error) {
	// 使用工厂创建新连接
	conn, err := s.webrtcFactory.CreateConnection(s.config)
	if err != nil {
		return nil, "", err
	}

	// 类型断言为 WebRTCConnection 以访问特定方法
	webrtcConn := conn.(*connection.WebRTCConnection)

	// 设置远程描述
	if err := webrtcConn.SetRemoteDescription(*offer); err != nil {
		conn.Stop()
		return nil, "", err
	}

	// 创建应答
	answer, err := webrtcConn.CreateAnswer()
	if err != nil {
		conn.Stop()
		return nil, "", err
	}
	log.Printf("answer: %v", answer)

	// 启动连接
	if err := conn.Start(); err != nil {
		conn.Stop()
		return nil, "", err
	}

	// 保存连接
	s.connections.Store(conn.GetID(), conn)

	return answer, conn.GetID(), nil
}

func (s *WHIPServer) DelConnection(id string) {
	if conn, exists := s.connections.LoadAndDelete(id); exists {
		conn.(connection.Connection).Stop()
	}
}
