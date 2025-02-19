package connection

import "streamlink/internal/config"

// Connection 定义了所有类型连接的通用接口
type Connection interface {
	// Start 启动连接的处理
	Start() error
	// Stop 停止连接并清理资源
	Stop()
	// GetID 返回连接的唯一标识符
	GetID() string
}

// ConnectionFactory 定义了创建不同类型连接的工厂接口
type ConnectionFactory interface {
	// CreateConnection 创建一个新的连接
	CreateConnection(cfg *config.Config) (Connection, error)
}
