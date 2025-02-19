package main

import (
	"fmt"
	"log"

	"streamlink/internal/config"
	"streamlink/pkg/server"

	"github.com/gin-gonic/gin"
)

func main() {
	// 设置 gin 为 release 模式，关闭调试信息
	gin.SetMode(gin.ReleaseMode)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	// 加载配置
	config, err := config.LoadConfig("config/config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 创建 Gin 引擎
	r := gin.Default()

	// 提供静态文件服务
	r.StaticFile("/", "./static/index.html")
	r.Static("/static", "./static")

	server := server.NewVoiceAgentServer()
	if err := server.Init(config); err != nil {
		log.Fatalf("Failed to initialize WHIP server: %v", err)
	}

	// 设置 WHIP 端点
	r.POST("/whip", server.HandleWHIP)
	// 会话管理端点
	r.DELETE("/whip/sessions/:id", server.HandleDelete)

	log.Printf("Starting VoiceAgent server on :%d\n", config.Server.HTTPPort)
	if err := r.Run(fmt.Sprintf(":%d", config.Server.HTTPPort)); err != nil {
		panic(err)
	}
}
