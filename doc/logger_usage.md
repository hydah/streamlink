# Using the Zap Logger in the Project

## Introduction

We have integrated the Zap logger (`go.uber.org/zap`) into the project to replace Go's standard logger. Zap provides high performance, low memory usage, and advanced logging capabilities.

## Log Levels

The Zap logger supports the following levels (in order of increasing severity):

1. `Debug` - Detailed information for debugging
2. `Info` - General information about normal operation
3. `Warn` - Warnings that don't prevent normal operation
4. `Error` - Errors that may affect the operation
5. `Fatal` - Critical errors that stop execution

## Log Format

The logs have a format like:

```
[2025-03-08T18:49:25.522+0800][INFO][server/voice_agent.go:143] TencentStreamTTS Close active synthesizer, idx=0
```

Where we use brackets `[]` as separators between fields:

1. `[2025-03-08T18:49:25.522+0800]` - Timestamp in ISO8601 format
2. `[INFO]` - Log level
3. `[server/voice_agent.go:143]` - File and line where the log originated
4. Message of the log

The log correctly shows the file and line of code where the logger was called, which makes it easier to identify the source of each message.

## How to Use the Logger

### Import the Package

```go
import "streamlink/pkg/logger"
```

### Available Functions

```go
// Log a debug message
logger.Debug("debug message with format %s", value)

// Log general information
logger.Info("important information: %v", value)

// Log a warning
logger.Warn("warning: %v", value)

// Log an error
logger.Error("an error occurred: %v", err)

// Log a fatal error (will terminate the application)
logger.Fatal("fatal error: %v", err)
```

### Configuration

The logger is initialized in `main.go` with a default level (`info`). If you need to change the log level, you can modify the initialization parameter:

```go
// To change the level to debug (without file):
logger.InitLogger("debug", "", nil)
```

#### Logging to a File

The logger can write logs to a file in addition to standard output:

```go
// To write logs to a file:
logger.InitLogger("debug", "/path/to/logfile.log", nil)
```

If a file path is specified, logs will be written to both stdout and the specified file. If the file doesn't exist, it will be created automatically.

In the configuration, you can set the log file path in the `config.yaml` file:

```yaml
log:
  level: "info"
  file: "/var/log/streamlink.log"
```

#### Log Rotation

The logger supports automatic rotation of log files, which prevents files from growing indefinitely and consuming all available disk space. The rotation is configured using the following parameters:

```yaml
log:
  level: info
  file: logs/streamlink.log
  max_size: 100     # maximum size in megabytes before rotation
  max_backups: 5    # maximum number of old log files to keep
  max_age: 30       # maximum number of days to keep old files
  compress: true    # compress rotated files
```

It can also be configured programmatically:

```go
rotateConfig := &logger.RotateConfig{
    MaxSize:    100, // 100 MB
    MaxBackups: 5,   // keep 5 backups
    MaxAge:     30,  // 30 days
    Compress:   true,
}
logger.InitLogger("debug", "/path/to/logfile.log", rotateConfig)
```

If rotation parameters are not specified, default values will be used.

### Customizing the Format

If you need to modify the format of the logs, you can edit the `pkg/logger/logger.go` file. For example, to change the separator between fields:

```go
// In the InitLogger function
encoderConfig.ConsoleSeparator = "["  // Use brackets as separator
```

### Usage Example

```go
package mycomponent

import "streamlink/pkg/logger"

func MyFunction() error {
    logger.Info("Starting function")
    
    if err := doSomething(); err != nil {
        logger.Error("Error doing something: %v", err)
        return err
    }
    
    logger.Info("Function completed successfully")
    return nil
}
```

## Migration from Standard Log

Converting from Go's standard logger to Zap:

| Standard Log | Zap Equivalent |
|--------------|-----------------|
| `log.Printf("info: %s", msg)` | `logger.Info("info: %s", msg)` |
| `log.Printf("error: %v", err)` | `logger.Error("error: %v", err)` |
| `log.Fatalf("fatal: %v", err)` | `logger.Fatal("fatal: %v", err)` |
| `log.Println("message")` | `logger.Info("message")` |

## Extensions

For more advanced use cases, you can access the underlying instances directly:

```go
// Access the Zap logger directly
zapLogger := logger.With(zap.String("component", "mycomponent"))

// Access the Sugar logger (more flexible API)
sugar := logger.Log.Sugar()
``` 