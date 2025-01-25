package server

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pion/webrtc/v4"
)

// HandleWHIP 处理 WHIP 请求
func (s *WHIPServer) HandleWHIP(c *gin.Context) {
	// 解析 SDP offer
	var offer webrtc.SessionDescription
	if err := c.BindJSON(&offer); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse offer"})
		return
	}

	log.Printf("offer: %v", offer)

	localDescription, sessionID, err := s.HandleNewConnection(&offer)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("sessionID: %s", sessionID)

	// 返回 SDP answer
	c.Header("Content-Type", "application/sdp")
	// 设置 Location header，返回会话资源的 URL
	location := fmt.Sprintf("/whip/sessions/%s", sessionID)
	c.Header("Location", location)
	c.Header("Content-Type", "application/sdp")

	// 返回 201 Created 状态码
	c.JSON(http.StatusCreated, localDescription)
}

func (s *WHIPServer) HandleDelete(c *gin.Context) {
	sessionID := c.Param("id")

	s.DelConnection(sessionID)
	c.Status(http.StatusOK)
}
