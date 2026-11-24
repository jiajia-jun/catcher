package sender

import "fmt"

// StatusError 表示非 2xx 响应，此时 Response 同时非 nil，body 可读
//
// 非 2xx 响应走 error 路径，调用方通过错误判断状态码，
// 响应状态与业务结果在调用点显式分开
type StatusError struct {
	Code int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("HTTP 状态码 %d", e.Code)
}

// TimeoutError 表示等待超时（context deadline 或 socket 超时）
type TimeoutError struct {
	Op string
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("%s 超时", e.Op)
}

// CanceledError 表示上层主动取消，调用方放弃本次请求
type CanceledError struct{}

func (e *CanceledError) Error() string {
	return "请求已取消"
}

// TransportError 表示连接 / DNS / TLS 等传输层失败
type TransportError struct {
	Err error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("传输失败: %v", e.Err)
}

func (e *TransportError) Unwrap() error {
	return e.Err
}

// TooLargeError 表示响应体超过上限，调用方只拿到错误
type TooLargeError struct {
	Limit int64
	Got   int64
}

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("响应体超限: %d > %d", e.Got, e.Limit)
}
