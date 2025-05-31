package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/sirupsen/logrus"
)

type Logger struct {
	*logrus.Logger
}

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARNING
	ERROR
	FATAL
)

var (
	defaultLogger *Logger
	logFile       *os.File
)

func init() {
	defaultLogger = NewLogger()
}

func NewLogger() *Logger {
	logger := logrus.New()

	// Create logs directory if it doesn't exist
	logsDir := "logs"
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		log.Printf("Failed to create logs directory: %v", err)
	}

	// Create log file with timestamp
	timestamp := time.Now().Format("2006-01-02")
	logFilePath := filepath.Join(logsDir, fmt.Sprintf("blockchain-%s.log", timestamp))

	var err error
	logFile, err = os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Printf("Failed to open log file: %v", err)
		logger.SetOutput(os.Stdout)
	} else {
		// Write to both file and stdout
		multiWriter := io.MultiWriter(os.Stdout, logFile)
		logger.SetOutput(multiWriter)
	}

	// Set custom formatter
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
		CallerPrettyfier: func(f *runtime.Frame) (string, string) {
			filename := filepath.Base(f.File)
			return fmt.Sprintf("%s()", f.Function), fmt.Sprintf("%s:%d", filename, f.Line)
		},
	})

	logger.SetReportCaller(true)
	logger.SetLevel(logrus.InfoLevel)

	return &Logger{Logger: logger}
}

func SetLevel(level LogLevel) {
	var logrusLevel logrus.Level
	switch level {
	case DEBUG:
		logrusLevel = logrus.DebugLevel
	case INFO:
		logrusLevel = logrus.InfoLevel
	case WARNING:
		logrusLevel = logrus.WarnLevel
	case ERROR:
		logrusLevel = logrus.ErrorLevel
	case FATAL:
		logrusLevel = logrus.FatalLevel
	default:
		logrusLevel = logrus.InfoLevel
	}
	defaultLogger.SetLevel(logrusLevel)
}

func GetLogger() *Logger {
	return defaultLogger
}

func Debug(args ...interface{}) {
	defaultLogger.Debug(args...)
}

func Debugf(format string, args ...interface{}) {
	defaultLogger.Debugf(format, args...)
}

func Info(args ...interface{}) {
	defaultLogger.Info(args...)
}

func Infof(format string, args ...interface{}) {
	defaultLogger.Infof(format, args...)
}

func Warning(args ...interface{}) {
	defaultLogger.Warning(args...)
}

func Warningf(format string, args ...interface{}) {
	defaultLogger.Warningf(format, args...)
}

func Error(args ...interface{}) {
	defaultLogger.Error(args...)
}

func Errorf(format string, args ...interface{}) {
	defaultLogger.Errorf(format, args...)
}

func Fatal(args ...interface{}) {
	defaultLogger.Fatal(args...)
}

func Fatalf(format string, args ...interface{}) {
	defaultLogger.Fatalf(format, args...)
}

func WithFields(fields map[string]interface{}) *logrus.Entry {
	return defaultLogger.WithFields(fields)
}

func WithField(key string, value interface{}) *logrus.Entry {
	return defaultLogger.WithField(key, value)
}

func Close() {
	if logFile != nil {
		logFile.Close()
	}
}

// Security logging functions
func LogSecurityEvent(event string, details map[string]interface{}) {
	WithFields(map[string]interface{}{
		"security_event": event,
		"details":        details,
		"timestamp":      time.Now().Unix(),
	}).Warning("Security event detected")
}

func LogTransactionEvent(txHash string, from, to string, amount string, status string) {
	WithFields(map[string]interface{}{
		"tx_hash": txHash,
		"from":    from,
		"to":      to,
		"amount":  amount,
		"status":  status,
	}).Info("Transaction processed")
}

func LogBlockEvent(blockNumber uint64, hash string, txCount int, minerAddr string) {
	WithFields(map[string]interface{}{
		"block_number": blockNumber,
		"block_hash":   hash,
		"tx_count":     txCount,
		"miner":        minerAddr,
	}).Info("Block processed")
}

func LogNetworkEvent(event string, peerAddr string, details map[string]interface{}) {
	WithFields(map[string]interface{}{
		"network_event": event,
		"peer_address":  peerAddr,
		"details":       details,
	}).Info("Network event")
}
