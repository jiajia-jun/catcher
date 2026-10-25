// Package yuketang 实现雨课堂的扫码登录、会话续期与作业抓取
//
// 接口依据 docs/接口文档/雨课堂.md。几条刻意保留的陷阱：
//   - ② 未扫码时是 nginx 504 或超时，属于「继续等」，不是错误
//   - ② token 作废是 400 + code 50400，需要重新取码
//   - ③ 的 body 是 JSON，Content-Type 却是 form-urlencoded
//   - 登录态看响应体 op == "web_redirect"，不看 HTTP 200
//   - activities 是按日期分组的嵌套数组，必须先摊平
//   - score_d 是 13 位毫秒，create_time 是 10 位秒
//   - 提交状态只能从课堂 todo_list 换算；拿不到时必须是 unknown
package yuketang

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"catcher/internal/domain/entity"
	"catcher/internal/infra/sender"
	"catcher/pkg/logger"
)

const (
	// DefaultBaseURL 是雨课堂网页端域名，①②③④⑤ 全在这里
	DefaultBaseURL = "https://changjiang.yuketang.cn"

	// defaultPollTimeout 必须大于服务端约 60 秒的挂起时间
	defaultPollTimeout = 70 * time.Second

	// qrLifetime 是二维码 token 的声明有效期
	qrLifetime = 5 * time.Minute
)

// Options 构造雨课堂适配器。Sender 由 cmd/app 注入，并按平台独立限速
type Options struct {
	BaseURL     string
	Sender      *sender.Sender
	Logger      logger.Logger
	Now         func() time.Time
	PollTimeout time.Duration
}

// Client 同时实现 domain.QRLogin、domain.Fetcher 与 domain.SessionRenewer
type Client struct {
	baseURL     string
	http        *sender.Sender
	logger      logger.Logger
	now         func() time.Time
	pollTimeout time.Duration
}

func New(opts Options) *Client {
	c := &Client{
		baseURL:     strings.TrimRight(opts.BaseURL, "/"),
		http:        opts.Sender,
		logger:      opts.Logger,
		now:         opts.Now,
		pollTimeout: opts.PollTimeout,
	}
	if c.baseURL == "" {
		c.baseURL = DefaultBaseURL
	}
	if c.logger == nil {
		c.logger = logger.Discard()
	}
	if c.now == nil {
		c.now = time.Now
	}
	if c.pollTimeout <= 0 {
		c.pollTimeout = defaultPollTimeout
	}
	return c
}

// Platform 返回平台标识
func (c *Client) Platform() entity.Platform { return entity.PlatformYuketang }

// get 发送带上 sessionid 的 GET；错误统一收敛为领域错误
func (c *Client) get(ctx context.Context, path, sessionID string) (*sender.Response, error) {
	return c.getWithHeaders(ctx, path, sessionID, nil)
}

// getWithHeaders 在默认 Accept 上叠加平台要求的额外请求头
//
// /mooc-api/ 与 /c27/ 需要一整套七个头，缺一条就返回业务失败；
// 其余接口只用默认 Accept
func (c *Client) getWithHeaders(ctx context.Context, path, sessionID string,
	header map[string]string) (*sender.Response, error) {
	reqHeader := make(map[string]string, len(header)+2)
	reqHeader["Accept"] = "application/json"
	for k, v := range header {
		reqHeader[k] = v
	}
	if sessionID != "" {
		reqHeader["Cookie"] = "sessionid=" + sessionID
	}

	resp, err := c.http.Do(ctx, &sender.Request{
		Method: http.MethodGet,
		URL:    c.baseURL + path,
		Header: reqHeader,
	})
	if err != nil {
		return resp, classifySendError(err)
	}
	return resp, nil
}

// postJSON 发送 JSON body；contentType 单独给出，因为 ③ 需要反直觉的组合
func (c *Client) postJSON(ctx context.Context, path, contentType string, body []byte, timeout time.Duration) (*sender.Response, error) {
	resp, err := c.http.Do(ctx, &sender.Request{
		Method: http.MethodPost,
		URL:    c.baseURL + path,
		Header: map[string]string{
			"Accept":       "application/json",
			"Content-Type": contentType,
		},
		Body:    body,
		Timeout: timeout,
	})
	if err != nil {
		return resp, classifySendError(err)
	}
	return resp, nil
}

// classifySendError 把 sender 错误映射到领域错误，同时保留原始错误
func classifySendError(err error) error {
	var canceled *sender.CanceledError
	if errors.As(err, &canceled) {
		return fmt.Errorf("%w: %v", entity.ErrCanceled, err)
	}
	var timeout *sender.TimeoutError
	if errors.As(err, &timeout) {
		return fmt.Errorf("%w: %v", entity.ErrNetwork, err)
	}
	var transport *sender.TransportError
	if errors.As(err, &transport) {
		return fmt.Errorf("%w: %v", entity.ErrNetwork, err)
	}
	var status *sender.StatusError
	if errors.As(err, &status) {
		if status.Code >= 500 {
			return fmt.Errorf("%w: %v", entity.ErrNetwork, err)
		}
		return fmt.Errorf("%w: %v", entity.ErrPlatformChanged, err)
	}
	var tooLarge *sender.TooLargeError
	if errors.As(err, &tooLarge) {
		return fmt.Errorf("%w: %v", entity.ErrPlatformChanged, err)
	}
	return fmt.Errorf("%w: %v", entity.ErrNetwork, err)
}

// sessionCookie 从原始 Set-Cookie 行里取 sessionid，不依赖 net/http/cookiejar
func sessionCookie(setCookies []string) string {
	for _, line := range setCookies {
		name, value, ok := strings.Cut(line, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "sessionid") {
			continue
		}
		if i := strings.IndexByte(value, ';'); i >= 0 {
			value = value[:i]
		}
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
