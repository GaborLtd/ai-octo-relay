package logx

import (
	"log"
	"os"
	"strings"
)

type Level int

const (
	Trace Level = iota
	Debug
	Info
	Warn
	Error
)

type Logger struct {
	level  Level
	logger *log.Logger
}

func New(level string) *Logger {
	return &Logger{
		level:  parseLevel(level),
		logger: log.New(os.Stdout, "", log.LstdFlags),
	}
}

func parseLevel(input string) Level {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "trace":
		return Trace
	case "debug":
		return Debug
	case "warn":
		return Warn
	case "error":
		return Error
	default:
		return Info
	}
}

func (l *Logger) Tracef(format string, args ...any) {
	l.logf(Trace, "TRACE", format, args...)
}

func (l *Logger) Debugf(format string, args ...any) {
	l.logf(Debug, "DEBUG", format, args...)
}

func (l *Logger) Infof(format string, args ...any) {
	l.logf(Info, "INFO", format, args...)
}

func (l *Logger) Warnf(format string, args ...any) {
	l.logf(Warn, "WARN", format, args...)
}

func (l *Logger) Errorf(format string, args ...any) {
	l.logf(Error, "ERROR", format, args...)
}

func (l *Logger) logf(level Level, label, format string, args ...any) {
	if level < l.level {
		return
	}
	l.logger.Printf("["+label+"] "+format, args...)
}
