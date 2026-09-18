// Package model 定义 sender 的请求报文与响应报文
package model

import (
	"net/http"
	"time"
)

// Request 请求报文
type Request struct {
	Method  string
	URL     string
	Header  map[string]string
	Body    []byte
	Timeout time.Duration // 这个是为雨课堂做准备的（轮询）
}

// Response 响应报文
type Response struct {
	Status     int
	Header     http.Header
	Raw        []byte
	SetCookies []string
	Location   string
	ReceivedAt time.Time
}
