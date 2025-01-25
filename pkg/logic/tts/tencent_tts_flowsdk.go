package tts

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	_PROTOCOL = "wss://"
	_HOST     = "tts.cloud.tencent.com"
	_PATH     = "/stream_wsv2"
	_ACTION   = "TextToStreamAudioWSv2"
)

// FlowingSpeechSynthesisListener 语音合成监听器接口
type FlowingSpeechSynthesisListener interface {
	OnSynthesisStart(sessionID string)
	OnSynthesisEnd()
	OnAudioResult(audioBytes []byte)
	OnTextResult(response map[string]interface{})
	OnSynthesisFail(response map[string]interface{})
}

// FlowingSpeechSynthesizer 流式语音合成器
type FlowingSpeechSynthesizer struct {
	appID            int64
	credential       *Credential
	status           int
	ws               *websocket.Conn
	wst              *sync.WaitGroup
	listener         FlowingSpeechSynthesisListener
	ready            bool
	voiceType        int64
	codec            string
	sampleRate       int
	volume           int
	speed            int
	sessionID        string
	stopCh           chan struct{}
	enableSubtitle   bool
	emotionCategory  string
	emotionIntensity int
}

// Credential 认证信息
type Credential struct {
	SecretID  string
	SecretKey string
}

// NewFlowingSpeechSynthesizer 创建新的流式语音合成器
func NewFlowingSpeechSynthesizer(appID int64, credential *Credential, listener FlowingSpeechSynthesisListener) *FlowingSpeechSynthesizer {
	return &FlowingSpeechSynthesizer{
		appID:            appID,
		credential:       credential,
		status:           0, // NOTOPEN
		listener:         listener,
		ready:            false,
		voiceType:        0,
		codec:            "pcm",
		sampleRate:       16000,
		volume:           10,
		speed:            0,
		wst:              &sync.WaitGroup{},
		stopCh:           make(chan struct{}),
		enableSubtitle:   true,
		emotionCategory:  "",
		emotionIntensity: 100,
	}
}

// SetVoiceType 设置音色
func (s *FlowingSpeechSynthesizer) SetVoiceType(voiceType int64) {
	s.voiceType = voiceType
}

// SetEmotionCategory 设置情感类型
func (s *FlowingSpeechSynthesizer) SetEmotionCategory(emotionCategory string) {
	s.emotionCategory = emotionCategory
}

// SetEmotionIntensity 设置情感强度
func (s *FlowingSpeechSynthesizer) SetEmotionIntensity(emotionIntensity int) {
	s.emotionIntensity = emotionIntensity
}

// SetCodec 设置音频编码
func (s *FlowingSpeechSynthesizer) SetCodec(codec string) {
	s.codec = codec
}

// SetSampleRate 设置采样率
func (s *FlowingSpeechSynthesizer) SetSampleRate(sampleRate int) {
	s.sampleRate = sampleRate
}

// SetSpeed 设置语速
func (s *FlowingSpeechSynthesizer) SetSpeed(speed int) {
	s.speed = speed
}

// SetVolume 设置音量
func (s *FlowingSpeechSynthesizer) SetVolume(volume int) {
	s.volume = volume
}

// SetEnableSubtitle 设置是否启用字幕
func (s *FlowingSpeechSynthesizer) SetEnableSubtitle(enableSubtitle bool) {
	s.enableSubtitle = enableSubtitle
}

// genSignature 生成签名
func (s *FlowingSpeechSynthesizer) genSignature(params map[string]interface{}) string {
	// 按键排序
	var keys []string
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 构建签名字符串
	signStr := "GET" + _HOST + _PATH + "?"
	for _, k := range keys {
		signStr += k + "=" + fmt.Sprint(params[k]) + "&"
	}
	signStr = signStr[:len(signStr)-1]

	// 计算 HMAC-SHA1
	h := hmac.New(sha1.New, []byte(s.credential.SecretKey))
	h.Write([]byte(signStr))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return signature
}

// genParams 生成请求参数
func (s *FlowingSpeechSynthesizer) genParams(sessionID string) map[string]interface{} {
	s.sessionID = sessionID
	params := make(map[string]interface{})
	params["Action"] = _ACTION
	params["AppId"] = s.appID
	params["SecretId"] = s.credential.SecretID
	params["ModelType"] = 1
	params["VoiceType"] = s.voiceType
	params["Codec"] = s.codec
	params["SampleRate"] = s.sampleRate
	params["Speed"] = s.speed
	params["Volume"] = s.volume
	params["SessionId"] = s.sessionID
	params["EnableSubtitle"] = s.enableSubtitle

	if s.emotionCategory != "" {
		params["EmotionCategory"] = s.emotionCategory
		params["EmotionIntensity"] = s.emotionIntensity
	}

	timestamp := time.Now().Unix()
	params["Timestamp"] = timestamp
	params["Expired"] = timestamp + 24*60*60

	return params
}

// createQueryString 创建查询字符串
func (s *FlowingSpeechSynthesizer) createQueryString(params map[string]interface{}) string {
	// 按键排序
	var keys []string
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 构建 URL
	var buf bytes.Buffer
	buf.WriteString(_PROTOCOL + _HOST + _PATH + "?")
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte('&')
		}
		buf.WriteString(url.QueryEscape(k))
		buf.WriteByte('=')
		buf.WriteString(url.QueryEscape(fmt.Sprint(params[k])))
	}

	return buf.String()
}

// newWSRequestMessage 创建 WebSocket 请求消息
func (s *FlowingSpeechSynthesizer) newWSRequestMessage(action, data string) map[string]interface{} {
	return map[string]interface{}{
		"session_id": s.sessionID,
		"message_id": strconv.FormatInt(time.Now().UnixNano(), 10),
		"action":     action,
		"data":       data,
	}
}

// doSend 发送数据
func (s *FlowingSpeechSynthesizer) doSend(action, text string) error {
	msg := s.newWSRequestMessage(action, text)
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return s.ws.WriteMessage(websocket.TextMessage, data)
}

// Process 处理合成请求
func (s *FlowingSpeechSynthesizer) Process(text string, action string) error {
	log.Printf("process: action=%s data=%s", action, text)
	return s.doSend(action, text)
}

// Complete 完成合成
func (s *FlowingSpeechSynthesizer) Complete(action string) error {
	log.Printf("complete: action=%s", action)
	return s.doSend(action, "")
}

// WaitReady 等待就绪
func (s *FlowingSpeechSynthesizer) WaitReady(timeoutMs int) bool {
	start := time.Now()
	for {
		if s.ready {
			return true
		}
		if time.Since(start) > time.Duration(timeoutMs)*time.Millisecond {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// Start 启动合成器
func (s *FlowingSpeechSynthesizer) Start() error {
	log.Println("synthesizer start: begin")

	// 生成参数和签名
	sessionID := strconv.FormatInt(time.Now().UnixNano(), 10)
	params := s.genParams(sessionID)
	signature := s.genSignature(params)

	// 构建 WebSocket URL
	reqURL := s.createQueryString(params)
	reqURL += "&Signature=" + url.QueryEscape(signature)
	log.Printf("reqURL: %s", reqURL)
	// 连接 WebSocket
	dialer := websocket.Dialer{}
	ws, _, err := dialer.Dial(reqURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to WebSocket: %v", err)
	}

	s.ws = ws
	s.status = 1 // STARTED

	// 启动消息处理
	s.wst.Add(1)
	go s.messageLoop()

	s.listener.OnSynthesisStart(sessionID)
	log.Println("synthesizer start: end")
	return nil
}

// messageLoop WebSocket 消息处理循环
func (s *FlowingSpeechSynthesizer) messageLoop() {
	defer s.wst.Done()
	defer s.ws.Close()

	for {
		select {
		case <-s.stopCh:
			return
		default:
			messageType, data, err := s.ws.ReadMessage()
			if err != nil {
				if !strings.Contains(err.Error(), "websocket: close") {
					log.Printf("read message error: %v", err)
				}
				return
			}

			switch messageType {
			case websocket.BinaryMessage:
				s.listener.OnAudioResult(data)

			case websocket.TextMessage:
				var resp map[string]interface{}

				if err := json.Unmarshal(data, &resp); err != nil {
					log.Printf("unmarshal response error: %v", err)
					continue
				}

				// log.Printf("resp: %+v", resp)
				if code, ok := resp["code"].(float64); ok && code != 0 {
					log.Printf("server synthesis fail: %v", resp)
					s.listener.OnSynthesisFail(resp)
					return
				}

				if final, ok := resp["final"].(float64); ok && final == 1 {
					log.Println("recv FINAL frame")
					s.status = 3 // FINAL
					s.listener.OnSynthesisEnd()
					return
				}

				if ready, ok := resp["ready"].(float64); ok && ready == 1 {
					log.Println("recv READY frame")
					s.ready = true
					continue
				}

				if heatbeat, ok := resp["heartbeat"].(float64); ok && heatbeat == 1 {
					log.Println("recv HEARTBEAT frame")
					continue
				}

				// 处理文本结果消息
				// log.Printf("resp result: %+v", resp["result"])
				if result, ok := resp["result"].(map[string]interface{}); ok {
					if subtitles, ok := result["subtitles"].([]interface{}); ok {
						if len(subtitles) > 0 {
							s.listener.OnTextResult(resp)
						}
					}
				}
			}
		}
	}
}

// Wait 等待合成完成
func (s *FlowingSpeechSynthesizer) Wait() {
	s.wst.Wait()
}

// Stop 停止合成器
func (s *FlowingSpeechSynthesizer) Stop() {
	close(s.stopCh)
	if s.ws != nil {
		s.ws.Close()
	}
}
