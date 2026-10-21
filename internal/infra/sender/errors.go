package sender

import "fmt"

// StatusError 表示非 2xx 响应。此时 Response 同时非 nil，body 可读。
//
// 非 2xx 也走 error 路径是刻意的：调用方必须显式判断，
// 不能靠“忘了检查 Status”静默通过。
type StatusError struct {
	Code int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("HTTP 状态码 %d", e.Code)
}

// TimeoutError 表示我们等够了（context deadline 或 socket 超时）。
type TimeoutError struct {
	Op string
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("%s 超时", e.Op)
}

// CanceledError 表示上层主动取消。调用方应放弃，不要再重试。
type CanceledError struct{}

func (e *CanceledError) Error() string {
	return "请求已取消"
}

// TransportError 表示连接 / DNS / TLS 等传输层失败。
type TransportError struct {
	Err error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("传输失败: %v", e.Err)
}

func (e *TransportError) Unwrap() error {
	return e.Err
}

// TooLargeError 表示响应体超过上限。此时不返回截断内容。
type TooLargeError struct {
	Limit int64
	Got   int64
}

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("响应体超限: %d > %d", e.Got, e.Limit)
}
