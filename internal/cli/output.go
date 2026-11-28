package cli

import (
	"encoding/json"
	"io"
	"time"

	"catcher/internal/dto"
	"catcher/pkg/logger"
)

// 退出码约定：3 > 1 > 0，用法错误单独用 2
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
	ExitAuth    = 3
)

// ErrorBody 是信封里的错误对象
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Envelope 是除 help 外所有命令的统一输出
type Envelope struct {
	OK    bool       `json:"ok"`
	Error *ErrorBody `json:"error,omitempty"`
	Data  any        `json:"data,omitempty"`
}

func writeJSON(w io.Writer, env Envelope) int {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(env); err != nil {
		return ExitFailure
	}
	return ExitOK
}

// writeData 输出成功信封
func writeData(w io.Writer, data any) int {
	return writeJSON(w, Envelope{OK: true, Data: data})
}

// writeFailure 输出失败信封，返回该错误码对应的退出码
func writeFailure(w io.Writer, code, message string) int {
	writeJSON(w, Envelope{
		OK:    false,
		Error: &ErrorBody{Code: code, Message: redact(message)},
	})
	return exitForCode(code)
}

// ReportFatal 在装配阶段失败时输出失败信封，供 cmd/app 使用
func ReportFatal(w io.Writer, err error) int {
	writeJSON(w, Envelope{
		OK:    false,
		Error: &ErrorBody{Code: "STORAGE", Message: redact(err.Error())},
	})
	return ExitFailure
}

// exitForCode 把业务错误码映射到退出码
// 需要重新登录的三种错误统一退出 3，其余业务失败退出 1
func exitForCode(code string) int {
	switch code {
	case "AUTH_REQUIRED", "AUTH_EXPIRED", "AUTH_FAILED":
		return ExitAuth
	default:
		return ExitFailure
	}
}

// redact 对外消息与日志使用同一套 URL 查询串打码规则
func redact(text string) string {
	return logger.RedactText(text)
}

// formatLocal 把时间转成东八区的 RFC3339，命令行输出使用本地口径
func formatLocal(t time.Time) string {
	return t.In(dto.Shanghai()).Format(time.RFC3339)
}
