// Package logger 封装 log/slog 与滚动文件，并提供敏感字段打码工具。
//
// 约定（与设计文档 §12 一致）：
//   - 日志文件位于 data/logs/catcher.log，按大小/备份数/天数滚动；
//   - 开发模式同时写 stderr，发布模式只写文件；
//   - Cookie / Set-Cookie / Authorization / 密码 / token / session 一律不得落日志。
//
// 本包只做通用能力，不 import internal/ 或 cmd/。
package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Logger 是项目内使用的日志接口。
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	With(args ...any) Logger
}

// Options 配置 Logger。Dir 为空时只输出到 Console（或丢弃）。
type Options struct {
	Dir        string
	Level      string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Console    bool
}

type slogLogger struct {
	l *slog.Logger
}

func (s *slogLogger) Debug(msg string, args ...any) { s.l.Debug(msg, args...) }
func (s *slogLogger) Info(msg string, args ...any)  { s.l.Info(msg, args...) }
func (s *slogLogger) Warn(msg string, args ...any)  { s.l.Warn(msg, args...) }
func (s *slogLogger) Error(msg string, args ...any) { s.l.Error(msg, args...) }
func (s *slogLogger) With(args ...any) Logger {
	return &slogLogger{l: s.l.With(args...)}
}

// New 创建 Logger。返回的 io.Closer 用于关闭日志文件，调用方应在退出时调用。
func New(opts Options) (Logger, io.Closer, error) {
	level := parseLevel(opts.Level)

	var writers []io.Writer
	var file *lumberjack.Logger

	if opts.Dir != "" {
		if err := os.MkdirAll(opts.Dir, 0o750); err != nil {
			return nil, nil, fmt.Errorf("创建日志目录失败: %w", err)
		}
		file = &lumberjack.Logger{
			Filename:   filepath.Join(opts.Dir, "catcher.log"),
			MaxSize:    positive(opts.MaxSizeMB, 20),
			MaxBackups: positive(opts.MaxBackups, 5),
			MaxAge:     positive(opts.MaxAgeDays, 14),
			LocalTime:  true,
			Compress:   false,
		}
		writers = append(writers, file)
	}
	if opts.Console || len(writers) == 0 {
		writers = append(writers, os.Stderr)
	}

	var w io.Writer
	if len(writers) == 1 {
		w = writers[0]
	} else {
		w = io.MultiWriter(writers...)
	}

	handler := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			// 兜底：即使调用方误把敏感键交给 slog，也在输出前打码。
			if IsSensitiveKey(a.Key) {
				return slog.String(a.Key, "***")
			}
			return a
		},
	})

	l := slog.New(handler).With("app", "catcher")
	if file == nil {
		return &slogLogger{l: l}, io.NopCloser(nilReader{}), nil
	}
	return &slogLogger{l: l}, file, nil
}

// Discard 返回一个不输出任何内容的 Logger，用于测试或可选依赖。
func Discard() Logger {
	return &slogLogger{l: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func positive(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}

type nilReader struct{}

func (nilReader) Read([]byte) (int, error) { return 0, io.EOF }
