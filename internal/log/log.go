// Package log — strukturlaşdırılmış logging.
//
// Sadə level-li logger. Go 1.21+ üçün slog mövcuddur, amma biz öz minimal
// implementasiyamızı yazırıq — kontrol bizdə qalır, dependency yoxdur.
//
// Format:
//
//	text:  2026-05-21T13:45:12Z INFO  [runtime] container başladı id=abc123
//	json:  {"ts":"...","level":"INFO","module":"runtime","msg":"...","id":"abc123"}
package log

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Level — log səviyyəsi.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	}
	return "?"
}

// ParseLevel — string-dən level.
func ParseLevel(s string) Level {
	switch strings.ToUpper(s) {
	case "DEBUG":
		return LevelDebug
	case "WARN":
		return LevelWarn
	case "ERROR":
		return LevelError
	default:
		return LevelInfo
	}
}

// Format — output format-ı.
type Format int

const (
	FormatText Format = iota
	FormatJSON
)

// Logger — bir log instance-i.
type Logger struct {
	mu     sync.Mutex
	out    io.Writer
	level  Level
	format Format
	module string
}

// default logger — paket səviyyəsində.
var defaultLogger = &Logger{
	out:    os.Stderr,
	level:  LevelInfo,
	format: FormatText,
}

// Configure — default logger-i quraşdırır.
//
// Environment dəyişənləri ilə də idarə olunur:
//
//	AZC_LOG_LEVEL  → DEBUG|INFO|WARN|ERROR
//	AZC_LOG_FORMAT → text|json
func Configure(level Level, format Format) {
	defaultLogger.level = level
	defaultLogger.format = format
}

// ConfigureFromEnv — env dəyişənlərinə görə quraşdırır.
func ConfigureFromEnv() {
	if v := os.Getenv("AZC_LOG_LEVEL"); v != "" {
		defaultLogger.level = ParseLevel(v)
	}
	if v := os.Getenv("AZC_LOG_FORMAT"); strings.ToLower(v) == "json" {
		defaultLogger.format = FormatJSON
	}
}

// Module — yeni logger, müəyyən modul üçün.
//
// İstifadə:
//
//	var log = mylog.Module("runtime")
//	log.Info("container başladı", "id", containerID)
func Module(name string) *Logger {
	return &Logger{
		out:    defaultLogger.out,
		level:  defaultLogger.level,
		format: defaultLogger.format,
		module: name,
	}
}

// Debug/Info/Warn/Error — log yaz.
//
// keyvals: alternating key/value pairs.
//
//	log.Info("mesaj", "key1", val1, "key2", val2)
func (l *Logger) Debug(msg string, keyvals ...interface{}) { l.log(LevelDebug, msg, keyvals) }
func (l *Logger) Info(msg string, keyvals ...interface{})  { l.log(LevelInfo, msg, keyvals) }
func (l *Logger) Warn(msg string, keyvals ...interface{})  { l.log(LevelWarn, msg, keyvals) }
func (l *Logger) Error(msg string, keyvals ...interface{}) { l.log(LevelError, msg, keyvals) }

// log — daxili yazma.
func (l *Logger) log(level Level, msg string, keyvals []interface{}) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	module := l.module
	if module == "" {
		module = "main"
	}

	if l.format == FormatJSON {
		l.writeJSON(now, level, module, msg, keyvals)
	} else {
		l.writeText(now, level, module, msg, keyvals)
	}
}

func (l *Logger) writeText(ts string, level Level, module, msg string, keyvals []interface{}) {
	var sb strings.Builder
	sb.WriteString(ts)
	sb.WriteString(" ")
	sb.WriteString(fmt.Sprintf("%-5s", level.String()))
	sb.WriteString(" [")
	sb.WriteString(module)
	sb.WriteString("] ")
	sb.WriteString(msg)

	for i := 0; i < len(keyvals); i += 2 {
		sb.WriteString(" ")
		sb.WriteString(fmt.Sprintf("%v", keyvals[i]))
		sb.WriteString("=")
		if i+1 < len(keyvals) {
			sb.WriteString(fmt.Sprintf("%v", keyvals[i+1]))
		} else {
			sb.WriteString("?")
		}
	}
	sb.WriteString("\n")
	l.out.Write([]byte(sb.String()))
}

func (l *Logger) writeJSON(ts string, level Level, module, msg string, keyvals []interface{}) {
	entry := map[string]interface{}{
		"ts":     ts,
		"level":  level.String(),
		"module": module,
		"msg":    msg,
	}
	for i := 0; i+1 < len(keyvals); i += 2 {
		entry[fmt.Sprintf("%v", keyvals[i])] = keyvals[i+1]
	}
	data, _ := json.Marshal(entry)
	data = append(data, '\n')
	l.out.Write(data)
}

// Package-level convenience funksiyalar.
func Debug(msg string, kv ...interface{}) { defaultLogger.Debug(msg, kv...) }
func Info(msg string, kv ...interface{})  { defaultLogger.Info(msg, kv...) }
func Warn(msg string, kv ...interface{})  { defaultLogger.Warn(msg, kv...) }
func Error(msg string, kv ...interface{}) { defaultLogger.Error(msg, kv...) }
