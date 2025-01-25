package testcase

import (
	"fmt"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

/*
# 生成 48kHz 立体声测试文件

	ffmpeg -f lavfi -i "sine=frequency=1000:duration=5" \
	  -ar 48000 \
	  -ac 2 \
	  -acodec pcm_s16le \
	  -y \
	  test_48k_stereo.wav

# 生成 48kHz 单声道测试文件

	ffmpeg -f lavfi -i "sine=frequency=1000:duration=5" \
	  -ar 48000 \
	  -ac 1 \
	  -acodec pcm_s16le \
	  -y \
	  test_48k_mono.wav
*/

func TestPushPull(t *testing.T) {
	tests := []struct {
		name             string
		audioFile        string
		sampleRate       int
		channels         int
		outputFile       string
		outputSampleRate int
		outputChannels   int
	}{
		{
			name:             "48kHz_stereo_to_mono",
			audioFile:        "../testdata/test_48k_stereo.wav",
			sampleRate:       48000,
			channels:         2,
			outputFile:       "../testdump/received_48k_stereo_to_mono.ogg",
			outputSampleRate: 48000,
			outputChannels:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建输出目录
			if err := os.MkdirAll("testdump", 0755); err != nil {
				t.Fatalf("Failed to create output directory: %v", err)
			}

			// 创建 WHIP 客户端
			client, err := NewWHIPClient()
			if err != nil {
				t.Fatalf("Failed to create WHIP client: %v", err)
			}
			defer client.Close()

			// 设置接收音频
			if err := client.ReceiveAudio(tt.outputFile, tt.outputSampleRate, tt.outputChannels); err != nil {
				t.Fatalf("Failed to setup audio receiver: %v", err)
			}

			// 添加轨道状态回调
			connected := make(chan struct{})
			client.peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
				log.Printf("ICE Connection State has changed: %s\n", state.String())
				if state == webrtc.ICEConnectionStateConnected {
					close(connected)
				}
			})

			// 连接到服务器
			if err := client.Connect(); err != nil {
				t.Fatalf("Failed to connect to server: %v", err)
			}

			log.Printf("[%s] Successfully connected to WHIP server", tt.name)

			// 等待连接建立，设置超时
			select {
			case <-connected:
				log.Printf("[%s] ICE connection established", tt.name)
			case <-time.After(10 * time.Second):
				t.Fatal("Timeout waiting for ICE connection")
			}

			// 发送音频文件
			config := AudioConfig{
				FilePath:   tt.audioFile,
				SampleRate: tt.sampleRate,
				Channels:   tt.channels,
			}
			if err := client.SendAudioFile(config); err != nil {
				t.Fatalf("Failed to send audio file: %v", err)
			}

			log.Printf("[%s] Successfully sent audio file: %s", tt.name, tt.audioFile)

			// 等待一段时间以接收音频
			time.Sleep(3 * time.Second)

			// 验证输出文件是否生成
			if _, err := os.Stat(tt.outputFile); os.IsNotExist(err) {
				t.Errorf("Output file was not created: %s", tt.outputFile)
			}
		})
	}
}

// TestConnectionStability 测试连接稳定性
func TestConnectionStability(t *testing.T) {
	client, err := NewWHIPClient()
	if err != nil {
		t.Fatalf("Failed to create WHIP client: %v", err)
	}
	defer client.Close()

	// 设置接收音频
	if err := client.ReceiveAudio("../testdump/stability_test.ogg", 48000, 1); err != nil {
		t.Fatalf("Failed to setup audio receiver: %v", err)
	}

	// 添加轨道状态回调
	stateChanges := make([]webrtc.ICEConnectionState, 0)
	client.peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		log.Printf("ICE Connection State has changed: %s\n", state.String())
		stateChanges = append(stateChanges, state)
	})

	// 连接到服务器
	if err := client.Connect(); err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}

	// 发送长时间的音频
	config := AudioConfig{
		FilePath:   "../testdata/test_48k_stereo.wav",
		SampleRate: 48000,
		Channels:   2,
	}

	// 在后台发送音频
	done := make(chan error)
	go func() {
		done <- client.SendAudioFile(config)
	}()

	// 等待并检查状态变化
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Failed to send audio: %v", err)
		}
	case <-timer.C:
		t.Log("Test completed successfully after 30 seconds")
	}

	// 检查连接状态变化
	t.Logf("Connection state changes: %v", stateChanges)
	for _, state := range stateChanges {
		if state == webrtc.ICEConnectionStateFailed {
			t.Error("Connection entered failed state during test")
		}
	}
}

// TestConcurrentConnections 测试并发连接
func TestConcurrentConnections(t *testing.T) {
	numClients := 3
	var wg sync.WaitGroup
	errors := make(chan error, numClients)

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			client, err := NewWHIPClient()
			if err != nil {
				errors <- fmt.Errorf("client %d: failed to create WHIP client: %v", clientID, err)
				return
			}
			defer client.Close()

			outputFile := fmt.Sprintf("../testdump/concurrent_test_%d.ogg", clientID)
			if err := client.ReceiveAudio(outputFile, 48000, 1); err != nil {
				errors <- fmt.Errorf("client %d: failed to setup audio receiver: %v", clientID, err)
				return
			}

			connected := make(chan struct{})
			client.peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
				log.Printf("Client %d: ICE Connection State has changed: %s\n", clientID, state.String())
				if state == webrtc.ICEConnectionStateConnected {
					close(connected)
				}
			})

			if err := client.Connect(); err != nil {
				errors <- fmt.Errorf("client %d: failed to connect: %v", clientID, err)
				return
			}

			select {
			case <-connected:
				log.Printf("Client %d: ICE connection established", clientID)
			case <-time.After(10 * time.Second):
				errors <- fmt.Errorf("client %d: timeout waiting for ICE connection", clientID)
				return
			}

			config := AudioConfig{
				FilePath:   "../testdata/test_48k_stereo.wav",
				SampleRate: 48000,
				Channels:   2,
			}
			if err := client.SendAudioFile(config); err != nil {
				errors <- fmt.Errorf("client %d: failed to send audio: %v", clientID, err)
				return
			}
		}(i)
	}

	// 等待所有客户端完成
	wg.Wait()
	close(errors)

	// 检查错误
	for err := range errors {
		t.Error(err)
	}
}
