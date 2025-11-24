// pkg/logger/logger.go - ENHANCED VERSION
package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/term"
)

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

var levelNames = map[LogLevel]string{
	DEBUG: "DEBUG",
	INFO:  "INFO",
	WARN:  "WARN",
	ERROR: "ERROR",
}

type Logger struct {
	level  LogLevel
	mu     sync.Mutex
	file   *os.File
	logger *log.Logger
}

var (
	globalLogger *Logger
	once         sync.Once
)

// Init initializes the logger with file rotation support
func Init() {
	once.Do(func() {
		logDir := os.Getenv("LOG_DIRECTORY")
		if logDir == "" {
			logDir = "./logs"
		}

		// Create logs directory
		if err := os.MkdirAll(logDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create log directory: %v\n", err)
			return
		}

		// Determine log level
		level := INFO
		if os.Getenv("LOG_LEVEL") == "DEBUG" {
			level = DEBUG
		}

		// Create log file with timestamp
		timestamp := time.Now().Format("2006-01-02")
		logFile := filepath.Join(logDir, fmt.Sprintf("solar_%s.log", timestamp))

		file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open log file: %v\n", err)
			return
		}

		// Create multi-writer for both console and file
		multiWriter := io.MultiWriter(os.Stdout, file)

		globalLogger = &Logger{
			level:  level,
			file:   file,
			logger: log.New(multiWriter, "", 0),
		}

		Info("🚀 Solar Monitoring Logger Initialized")
		Infof("📁 Log Directory: %s", logDir)
		Infof("📊 Log Level: %s", levelNames[level])
	})
}

// logMessage formats and outputs a log message
func (l *Logger) logMessage(level LogLevel, message string) {
	if l == nil || level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	levelStr := levelNames[level]

	// Color codes for console output
	colorCode := ""
	resetCode := ""
	if isTerminal(os.Stdout) {
		colorCode = getColorCode(level)
		resetCode = "\033[0m"
	}

	output := fmt.Sprintf("%s%s [%s] %s%s\n", colorCode, timestamp, levelStr, message, resetCode)
	l.logger.Print(output)
}

// Debug logs debug message
func Debug(message string) {
	if globalLogger == nil {
		Init()
	}
	globalLogger.logMessage(DEBUG, message)
}

// Debugf logs formatted debug message
func Debugf(format string, args ...interface{}) {
	Debug(fmt.Sprintf(format, args...))
}

// Info logs info message
func Info(message string) {
	if globalLogger == nil {
		Init()
	}
	globalLogger.logMessage(INFO, message)
}

// Infof logs formatted info message
func Infof(format string, args ...interface{}) {
	Info(fmt.Sprintf(format, args...))
}

// Warn logs warning message
func Warn(message string) {
	if globalLogger == nil {
		Init()
	}
	globalLogger.logMessage(WARN, message)
}

// Warnf logs formatted warning message
func Warnf(format string, args ...interface{}) {
	Warn(fmt.Sprintf(format, args...))
}

// Error logs error message
func Error(message string) {
	if globalLogger == nil {
		Init()
	}
	globalLogger.logMessage(ERROR, message)
}

// Errorf logs formatted error message
func Errorf(format string, args ...interface{}) {
	Error(fmt.Sprintf(format, args...))
}

// Fatal logs fatal message and exits
func Fatal(message string) {
	if globalLogger == nil {
		Init()
	}
	globalLogger.logMessage(ERROR, "FATAL: "+message)
	if globalLogger.file != nil {
		globalLogger.file.Close()
	}
	os.Exit(1)
}

// WithContext logs with request context
func WithContext(requestID, context string, level LogLevel, message string) {
	if globalLogger == nil {
		Init()
	}
	msg := fmt.Sprintf("[%s] [%s] %s", requestID, context, message)
	globalLogger.logMessage(level, msg)
}

// WithContextf logs formatted message with context
func WithContextf(requestID, context string, level LogLevel, format string, args ...interface{}) {
	WithContext(requestID, context, level, fmt.Sprintf(format, args...))
}

// Stats logs performance metrics
func Stats(title string, data map[string]interface{}) {
	msg := fmt.Sprintf("📊 %s", title)
	for k, v := range data {
		msg += fmt.Sprintf(" | %s=%v", k, v)
	}
	Info(msg)
}

// Perf logs performance timing
func Perf(operation string, duration time.Duration, success bool) {
	status := "✅"
	if !success {
		status = "❌"
	}
	Infof("%s %s completed in %v", status, operation, duration)
}

// Close closes the logger and flushes all data
func Close() error {
	if globalLogger != nil && globalLogger.file != nil {
		return globalLogger.file.Close()
	}
	return nil
}

// Helper functions

func getColorCode(level LogLevel) string {
	switch level {
	case DEBUG:
		return "\033[36m" // Cyan
	case INFO:
		return "\033[32m" // Green
	case WARN:
		return "\033[33m" // Yellow
	case ERROR:
		return "\033[31m" // Red
	default:
		return ""
	}
}

func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// StructuredLog logs structured data in a readable format
type StructuredLog struct {
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Context   map[string]interface{} `json:"context,omitempty"`
}

// LogStructured logs structured data
func LogStructured(level LogLevel, message string, context map[string]interface{}) {
	if globalLogger == nil {
		Init()
	}

	sl := StructuredLog{
		Timestamp: time.Now().Format(time.RFC3339),
		Level:     levelNames[level],
		Message:   message,
		Context:   context,
	}

	output := fmt.Sprintf("📦 [STRUCT] %+v", sl)
	globalLogger.logMessage(level, output)
}

// Request logging for API calls
type RequestLog struct {
	Method     string
	Path       string
	StatusCode int
	Duration   time.Duration
	BytesSent  int64
	RemoteAddr string
	UserAgent  string
}

// LogRequest logs HTTP request details
func LogRequest(req RequestLog) {
	status := "✅"
	if req.StatusCode >= 400 {
		status = "❌"
	}
	if req.StatusCode >= 300 && req.StatusCode < 400 {
		status = "↩️ "
	}

	Infof(
		"%s %s %s %d | %v | %d bytes | %s",
		status,
		req.Method,
		req.Path,
		req.StatusCode,
		req.Duration,
		req.BytesSent,
		req.RemoteAddr,
	)
}

// Mapping operation logging
type MappingLog struct {
	Operation  string // "create", "update", "delete", "test"
	SourceID   string
	FieldCount int
	Status     string // "success", "failed"
	Duration   time.Duration
	Error      string
}

// LogMapping logs mapping operations
func LogMapping(ml MappingLog) {
	status := "✅"
	if ml.Status != "success" {
		status = "❌"
	}

	msg := fmt.Sprintf(
		"%s [MAPPING] %s(%s) - %d fields in %v",
		status,
		ml.Operation,
		ml.SourceID,
		ml.FieldCount,
		ml.Duration,
	)

	if ml.Error != "" {
		msg += fmt.Sprintf(" | Error: %s", ml.Error)
	}

	if ml.Status == "success" {
		Info(msg)
	} else {
		Error(msg)
	}
}

// Data processing logging
type DataLog struct {
	SourceID        string
	RecordCount     int
	SuccessCount    int
	FailureCount    int
	AverageDuration time.Duration
}

// LogData logs data processing
func LogData(dl DataLog) {
	successRate := float64(dl.SuccessCount) / float64(dl.RecordCount) * 100
	Infof(
		"📥 [DATA] source=%s | records=%d | success=%d (%.1f%%) | avg_time=%v",
		dl.SourceID,
		dl.RecordCount,
		dl.SuccessCount,
		successRate,
		dl.AverageDuration,
	)
}

// Alert logging for critical issues
func Alert(severity string, message string) {
	emoji := "⚠️ "
	if severity == "CRITICAL" {
		emoji = "🚨"
	}
	Error(fmt.Sprintf("%s [%s] %s", emoji, severity, message))
}
