package logger

import (
	"fmt"
	"sync"
	"time"
)

type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
	LevelDebug Level = "debug"
)

type LogEntry struct {
	Level     Level     `json:"level"`
	Message   string    `json:"message"`
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`
}

type Logger struct {
	mu       sync.RWMutex
	entries  []LogEntry
	maxSize  int
	onEntry  func(LogEntry)
}

func New(maxSize int, onEntry func(LogEntry)) *Logger {
	return &Logger{
		entries: make([]LogEntry, 0, maxSize),
		maxSize: maxSize,
		onEntry: onEntry,
	}
}

func (l *Logger) log(level Level, source, format string, args ...interface{}) {
	entry := LogEntry{
		Level:     level,
		Message:   fmt.Sprintf(format, args...),
		Source:    source,
		Timestamp: time.Now(),
	}

	l.mu.Lock()
	if len(l.entries) >= l.maxSize {
		l.entries = l.entries[1:]
	}
	l.entries = append(l.entries, entry)
	l.mu.Unlock()

	if l.onEntry != nil {
		l.onEntry(entry)
	}
}

func (l *Logger) Info(source, format string, args ...interface{}) {
	l.log(LevelInfo, source, format, args...)
}

func (l *Logger) Warn(source, format string, args ...interface{}) {
	l.log(LevelWarn, source, format, args...)
}

func (l *Logger) Error(source, format string, args ...interface{}) {
	l.log(LevelError, source, format, args...)
}

func (l *Logger) Debug(source, format string, args ...interface{}) {
	l.log(LevelDebug, source, format, args...)
}

func (l *Logger) GetEntries(last int) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if last <= 0 || last > len(l.entries) {
		last = len(l.entries)
	}

	start := len(l.entries) - last
	result := make([]LogEntry, last)
	copy(result, l.entries[start:])
	return result
}
