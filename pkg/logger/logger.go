package logger

import (
	"fmt"
	"log"
	"os"
	"time"
)

var (
	infoLogger  *log.Logger
	errorLogger *log.Logger
	debugLogger *log.Logger
	isDebug     bool
)

// Init initializes the logger
func Init() {
	logLevel := os.Getenv("LOG_LEVEL")
	isDebug = logLevel == "DEBUG"

	infoLogger = log.New(os.Stdout, "[INFO] ", log.LstdFlags)
	errorLogger = log.New(os.Stderr, "[ERROR] ", log.LstdFlags)
	debugLogger = log.New(os.Stdout, "[DEBUG] ", log.LstdFlags)
}

// Info logs informational messages
func Info(message string) {
	if infoLogger != nil {
		infoLogger.Println(message)
	}
}

// Error logs error messages
func Error(message string) {
	if errorLogger != nil {
		errorLogger.Println(message)
	}
}

// Debug logs debug messages
func Debug(message string) {
	if isDebug && debugLogger != nil {
		debugLogger.Println(message)
	}
}

// Warn logs warning messages
func Warn(message string) {
	if infoLogger != nil {
		infoLogger.Printf("[WARN] %s\n", message)
	}
}

// Fatal logs fatal error and exits
func Fatal(message string) {
	if errorLogger != nil {
		errorLogger.Println(message)
	}
	os.Exit(1)
}

// Infof logs formatted informational message
func Infof(format string, args ...interface{}) {
	Info(fmt.Sprintf(format, args...))
}

// Errorf logs formatted error message
func Errorf(format string, args ...interface{}) {
	Error(fmt.Sprintf(format, args...))
}

// Debugf logs formatted debug message
func Debugf(format string, args ...interface{}) {
	Debug(fmt.Sprintf(format, args...))
}

// WithFields logs with additional context
func WithFields(fields map[string]interface{}, message string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fieldsStr := ""
	for k, v := range fields {
		fieldsStr += fmt.Sprintf(" %s=%v", k, v)
	}
	Info(fmt.Sprintf("[%s]%s %s", timestamp, fieldsStr, message))
}