// internal/logging/logging.go
package logging

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type LogLevel string

const (
	LogLevelInfo  LogLevel = "INFO"
	LogLevelError LogLevel = "ERROR"
)

type LogEntry struct {
	Time      time.Time `json:"time"`
	Level     LogLevel  `json:"level"`
	Message   string    `json:"message"`
	OS        string    `json:"os"`
	Operation string    `json:"operation,omitempty"`
	Path      string    `json:"path,omitempty"`
	Error     string    `json:"error,omitempty"`
}

var logger *log.Logger

func InitLogger(logDir string) error {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %v", err)
	}

	logFile := filepath.Join(logDir, fmt.Sprintf("gofindmyfonts-%s.log", time.Now().Format("2006-01-02")))
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %v", err)
	}

	logger = log.New(file, "", 0)
	return nil
}

func logMessage(level LogLevel, msg string, op string, path string, err error) {
	entry := LogEntry{
		Time:      time.Now(),
		Level:     level,
		Message:   msg,
		OS:        runtime.GOOS,
		Operation: op,
		Path:      path,
	}

	if err != nil {
		entry.Error = err.Error()
	}

	jsonEntry, err := json.Marshal(entry)
	if err != nil {
		log.Printf("Error marshaling log entry: %v", err)
		return
	}

	if logger != nil {
		logger.Println(string(jsonEntry))
	}
	// Also print to stdout
	log.Println(string(jsonEntry))
}

func Info(msg string, op string, path string) {
	logMessage(LogLevelInfo, msg, op, path, nil)
}

func Error(msg string, op string, path string, err error) {
	logMessage(LogLevelError, msg, op, path, err)
}
