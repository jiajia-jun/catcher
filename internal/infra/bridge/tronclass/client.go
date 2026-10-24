// Package tronclass 实现学在重邮的浏览器会话导入与作业抓取
//
// 接口依据 docs/接口文档/学在重邮.md。几条刻意保留的陷阱：
//   - 数据全在 lms.tc.cqupt.edu.cn，登录链跨三台机器；本版本只导入 session，不做 CAS 登录
//   - 只靠 session，role_token 不依赖也不保存
//   - 每个 200 都轮换 session；401/302 下发的匿名 session 绝不能覆盖已保存值
//   - 未认证时 Accept: application/json 决定失败形态（401 JSON 而不是 302）
//   - 作业列表越界页会重复最后一页，空课程 page=0，翻页只能用 pages 终止
//   - 作业项没有 course_id，课程归属只能从请求 URL 带出
//   - 提交判据只用 submitted，截止时间只认 deadline
package tronclass

import (
	"context"
	"encoding/json"
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
	// DefaultBaseURL 是学在重邮数据接口的域名
	DefaultBaseURL = "https://lms.tc.cqupt.edu.cn"

	// maxSessionBytes 是导入会话的长度上限
	maxSessionBytes = 4096

	// retryDelay 是网络类错误的退避间隔
	retryDelay = 250 * time.Millisecond

	// userAgent 伪装成桌面浏览器
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
)

// Options 构造学在重邮适配器。Sender 由 cmd/app 注入，单独限速 250ms
type Options struct {
	BaseURL string
	Sender  *sender.Sender
	Logger  logger.Logger
	Now     func() time.Time
}

// Client 实现 domain.SessionImporter 与 domain.Fetcher
type Client struct {
	baseURL string
	http    *sender.Sender
	logger  logger.Logger
	now     func() time.Time
}

func New(opts Options) *Client {
	c := &Client{
		baseURL: strings.TrimRight(opts.BaseURL, "/"),
		http:    opts.Sender,
		logger:  opts.Logger,
		now:     opts.Now,
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
	return c
}

// Platform 返回平台标识
func (c *Client) Platform() entity.Platform { return entity.PlatformTronclass }

// decodeSecret 读取会话里的 session 字符串
func decodeSecret(raw json.RawMessage) (*entity.TronclassSecret, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: 会话为空", entity.ErrAuthRequired)
	}
	var secret entity.TronclassSecret
	if err := json.Unmarshal(raw, &secret); err != nil {
		return nil, fmt.Errorf("%w: 解析学在重邮会话失败: %v", entity.ErrAuthRequired, err)
	}
	if strings.TrimSpace(secret.Session) == "" {
		return nil, fmt.Errorf("%w: 会话缺少 session", entity.ErrAuthRequired)
	}
	return &secret, nil
}

// newSession 把 session 与 uid 打包成待加密保存的会话
func (c *Client) newSession(value, uid string) (*entity.Session, error) {
	raw, err := json.Marshal(entity.TronclassSecret{
		Session: value,
		UserID:  uid,
	})
	if err != nil {
		return nil, err
	}
	return &entity.Session{
		Platform:  entity.PlatformTronclass,
		Secret:    raw,
		UpdatedAt: c.now().UTC(),
	}, nil
}

// do 发送一次请求，网络类错误退避后重试一次
//
// 只有传输失败与超时重试；状态码错误、取消、响应超限都不重试
func (c *Client) do(ctx context.Context, method, path string, body []byte, session string) (*sender.Response, error) {
	header := map[string]string{
		"Accept":     "application/json",
		"User-Agent": userAgent,
		"Cookie":     "session=" + session,
	}
	if len(body) > 0 {
		header["Content-Type"] = "application/json"
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		resp, err := c.http.Do(ctx, &sender.Request{
			Method: method,
			URL:    c.baseURL + path,
			Header: header,
			Body:   body,
		})
		if err == nil {
			return resp, nil
		}
		lastErr = classifySendError(err)
		if attempt == 1 || !errors.Is(lastErr, entity.ErrNetwork) {
			return resp, lastErr
		}
		if err := sleepCtx(ctx, retryDelay); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

// sleepCtx 在退避期间响应取消
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%w: 重试等待时超时", entity.ErrNetwork)
		}
		return fmt.Errorf("%w: 已取消", entity.ErrCanceled)
	case <-timer.C:
		return nil
	}
}

// sessionFromSetCookies 只取 200 响应里轮换后的 session 值
func sessionFromSetCookies(lines []string) string {
	for _, line := range lines {
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(name) != "session" {
			continue
		}
		if i := strings.IndexByte(value, ';'); i >= 0 {
			value = value[:i]
		}
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// isJSONResponse 判断响应头与响应体是否都是 JSON
func isJSONResponse(resp *sender.Response) bool {
	if resp == nil {
		return false
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "json") {
		return false
	}
	return json.Valid(resp.Raw)
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
		switch {
		// sender 不跟随重定向；数据接口被 3xx 带去 CAS 说明会话失效
		case status.Code >= 300 && status.Code < 400:
			return fmt.Errorf("%w: 请求被重定向（HTTP %d）", entity.ErrAuthExpired, status.Code)
		case status.Code == http.StatusUnauthorized:
			return fmt.Errorf("%w: HTTP %d", entity.ErrAuthExpired, status.Code)
		case status.Code >= 400 && status.Code < 500:
			return fmt.Errorf("%w: HTTP %d", entity.ErrBusiness, status.Code)
		case status.Code >= 500:
			return fmt.Errorf("%w: HTTP %d", entity.ErrPlatformChanged, status.Code)
		default:
			return fmt.Errorf("%w: %v", entity.ErrPlatformChanged, err)
		}
	}
	var tooLarge *sender.TooLargeError
	if errors.As(err, &tooLarge) {
		return fmt.Errorf("%w: %v", entity.ErrPlatformChanged, err)
	}
	return fmt.Errorf("%w: %v", entity.ErrNetwork, err)
}

func issueKindOf(err error) entity.IssueKind {
	switch {
	case errors.Is(err, entity.ErrNetwork):
		return entity.IssueNetwork
	case errors.Is(err, entity.ErrAuthExpired):
		return entity.IssueAuth
	case errors.Is(err, entity.ErrCanceled):
		return entity.IssueCanceled
	case errors.Is(err, entity.ErrBusiness):
		return entity.IssueBusiness
	case errors.Is(err, entity.ErrPlatformChanged):
		return entity.IssueParse
	default:
		return entity.IssueInternal
	}
}
