package logger

import (
	"os"
	"streamlink/internal/config"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	Log   *zap.Logger
	Sugar *zap.SugaredLogger
	once  sync.Once
)

// BracketEncoder es un encoder personalizado que usa corchetes entre campos
type BracketEncoder struct {
	zapcore.Encoder
	pool buffer.Pool
}

// DefaultRotateConfig returns default rotation configuration
func DefaultLogConfig() config.LogConfig {
	return config.LogConfig{
		Level:      "info",
		File:       "logs/streamlink.log",
		MaxSize:    100, // 100 MB
		MaxBackups: 5,   // keep 5 backups
		MaxAge:     30,  // 30 days
		Compress:   true,
	}
}

// NewBracketEncoder crea un nuevo encoder personalizado
func NewBracketEncoder(config zapcore.EncoderConfig) zapcore.Encoder {
	return &BracketEncoder{
		Encoder: zapcore.NewJSONEncoder(config),
		pool:    buffer.NewPool(),
	}
}

// Clone implementa el método Clone requerido por la interfaz Encoder
func (e *BracketEncoder) Clone() zapcore.Encoder {
	return &BracketEncoder{
		Encoder: e.Encoder.Clone(),
		pool:    e.pool,
	}
}

// EncodeEntry implementa el método EncodeEntry requerido por la interfaz Encoder
func (e *BracketEncoder) EncodeEntry(entry zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	buf := e.pool.Get()

	// Timestamp
	buf.AppendString("[")
	buf.AppendString(entry.Time.Format("2006-01-02T15:04:05.000-0700"))
	buf.AppendString("]")

	// Level
	buf.AppendString("[")
	buf.AppendString(entry.Level.CapitalString())
	buf.AppendString("]")

	// Caller (file and line)
	buf.AppendString("[")
	buf.AppendString(entry.Caller.TrimmedPath())
	buf.AppendString("]")

	// Message
	buf.AppendString(" ")
	buf.AppendString(entry.Message)

	// Add a newline at the end
	buf.AppendString("\n")

	return buf, nil
}

// InitLogger initializes the global logger instance
// logLevel: "debug", "info", "warn", "error", "dpanic", "panic", "fatal"
// logFile: path to log file, if empty logs will be written to stdout only
// rotateConfig: optional configuration for log rotation, if nil default rotation settings will be used
func InitLogger(config *config.LogConfig) {
	once.Do(func() {
		// Parse log level
		level := zap.InfoLevel
		err := level.Set(config.Level)
		if err != nil {
			level = zap.InfoLevel // Default to info level if parsing fails
		}

		// Configure encoder
		encoderConfig := zap.NewProductionEncoderConfig()
		encoderConfig.TimeKey = "time"
		encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
		encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder

		// Setup output sinks
		var core zapcore.Core

		if config.File == "" {
			// Log to stdout only
			core = zapcore.NewCore(
				NewBracketEncoder(encoderConfig),
				zapcore.AddSync(os.Stdout),
				zap.NewAtomicLevelAt(level),
			)
		} else {
			// Use default rotation config if not provided
			if config == nil {
				defaultConfig := DefaultLogConfig()
				config = &defaultConfig
			}

			// Setup log rotation
			rotator := &lumberjack.Logger{
				Filename:   config.File,
				MaxSize:    config.MaxSize, // megabytes
				MaxBackups: config.MaxBackups,
				MaxAge:     config.MaxAge, // days
				Compress:   config.Compress,
			}

			// Create a multi-writer core that logs to both stdout and rotating file
			stdoutCore := zapcore.NewCore(
				NewBracketEncoder(encoderConfig),
				zapcore.AddSync(os.Stdout),
				zap.NewAtomicLevelAt(level),
			)

			fileCore := zapcore.NewCore(
				NewBracketEncoder(encoderConfig),
				zapcore.AddSync(rotator),
				zap.NewAtomicLevelAt(level),
			)

			core = zapcore.NewTee(stdoutCore, fileCore)
		}

		// Create logger
		Log = zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
		Sugar = Log.Sugar()
	})
}

// InitLoggerSimple provides backward compatibility with the previous API
func InitLoggerSimple(config *config.LogConfig) {
	InitLogger(config)
}

// Debug logs a message at debug level
func Debug(msg string, fields ...interface{}) {
	Sugar.Debugf(msg, fields...)
}

// Info logs a message at info level
func Info(msg string, fields ...interface{}) {
	Sugar.Infof(msg, fields...)
}

// Warn logs a message at warn level
func Warn(msg string, fields ...interface{}) {
	Sugar.Warnf(msg, fields...)
}

// Error logs a message at error level
func Error(msg string, fields ...interface{}) {
	Sugar.Errorf(msg, fields...)
}

// Fatal logs a message at fatal level
func Fatal(msg string, fields ...interface{}) {
	Sugar.Fatalf(msg, fields...)
}

// With returns a logger with the specified fields
func With(fields ...zap.Field) *zap.Logger {
	return Log.With(fields...)
}

// Named returns a logger with the specified name
func Named(name string) *zap.Logger {
	return Log.Named(name)
}

// WithOptions returns a logger with the specified options
func WithOptions(opts ...zap.Option) *zap.Logger {
	return Log.WithOptions(opts...)
}

// Sync flushes any buffered log entries
func Sync() error {
	return Log.Sync()
}
