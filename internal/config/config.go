package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type LogConfig struct {
	Level      string `yaml:"level"`
	File       string `yaml:"file"`
	MaxSize    int    `yaml:"max_size"`    // maximum size in megabytes before rotation
	MaxBackups int    `yaml:"max_backups"` // maximum number of old log files to retain
	MaxAge     int    `yaml:"max_age"`     // maximum number of days to retain old files
	Compress   bool   `yaml:"compress"`    // compress rotated files
}

type ServerConfig struct {
	HTTPPort          int      `yaml:"http_port"`
	UDPPort           int      `yaml:"udp_port"`
	PublicIP          []string `yaml:"public_ip"`
	LowLatency        bool     `yaml:"low_latency"`
	Interrupt         bool     `yaml:"interrupt"`
	SemanticInterrupt bool     `yaml:"semantic_interrupt"`
}

type LLMConfig struct {
	Type   string `yaml:"type"`
	OpenAI struct {
		APIKey      string  `yaml:"api_key"`
		BaseURL     string  `yaml:"base_url"`
		Model       string  `yaml:"model"`
		Temperature float64 `yaml:"temperature"`
		MaxTokens   int     `yaml:"max_tokens"`
	} `yaml:"openai"`
}

type ASRConfig struct {
	Type       string `yaml:"type"`
	TencentASR struct {
		AppID           string `yaml:"app_id"`
		SecretID        string `yaml:"secret_id"`
		SecretKey       string `yaml:"secret_key"`
		EngineModelType string `yaml:"engine_model_type"`
		SliceSize       int    `yaml:"slice_size"`
	} `yaml:"tencent_asr"`
}

type TTSConfig struct {
	Type       string `yaml:"type"`
	TencentTTS struct {
		AppID     string `yaml:"app_id"`
		SecretID  string `yaml:"secret_id"`
		SecretKey string `yaml:"secret_key"`
		VoiceType int64  `yaml:"voice_type"`
		Codec     string `yaml:"codec"`
	} `yaml:"tencent_tts"`
}

type Config struct {
	Server ServerConfig `yaml:"server"`
	Log    LogConfig    `yaml:"log"`
	LLM    LLMConfig    `yaml:"llm"`
	ASR    ASRConfig    `yaml:"asr"`
	TTS    TTSConfig    `yaml:"tts"`
}

func LoadConfig(path string) (*Config, error) {
	config := &Config{}

	file, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %v", err)
	}

	err = yaml.Unmarshal(file, config)
	if err != nil {
		return nil, fmt.Errorf("error parsing config file: %v", err)
	}

	return config, nil
}
