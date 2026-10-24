package sender

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"catcher/internal/infra/sender/model"
	"catcher/pkg/logger"
)

// Request / Response 是 sender 对外暴露的报文类型。
// 具体字段定义在 model 子包，保持报文与传输逻辑分离。
type Request = model.Request
type Response = model.Response

const (
	// defaultTimeout 是单次请求的默认超时。
	defaultTimeout = 30 * time.Second

	// defaultMaxBodyBytes 是响应体的大小上限。
	defaultMaxBodyBytes = 8 << 20 // 8MB
)

// Sender 是向平台发请求的发送器，可并发使用。
//
// 除限速状态外没有任何可变状态：不持有 cookie、不缓存会话。
//
// 限速按实例生效，所以每个平台适配器应各持一个实例
// （学习通 250ms 与雨课堂 300ms 互不干扰）；多个实例可以共享同一个
// Config.Transport 以复用连接池。
type Sender struct {
	transport      http.RoundTripper
	client         *http.Client
	defaultTimeout time.Duration
	maxBody        int64
	logger         logger.Logger

	// mu 保护下面的限速状态。
	//
	// 它同时承担两件事：保证并发安全，以及把并发调用摊平成符合
	// MinInterval 的节奏 —— 两者是同一个机制，不需要分开设计。
	mu          sync.Mutex
	minInterval time.Duration
	lastSent    time.Time
}

// Config 用于配置 Sender。零值可用，字段为 0 / nil 时将在 New 中使用默认值。
type Config struct {
	Transport      http.RoundTripper
	DefaultTimeout time.Duration
	MaxBodyBytes   int64
	MinInterval    time.Duration
	// Logger 可为 nil；nil 时不输出日志。
	Logger logger.Logger
}

// New 构造一个 Sender。Config 的零值可用，各项留 0 / nil 时取默认值。
func New(cfg Config) *Sender {
	s := &Sender{
		transport:      cfg.Transport,
		defaultTimeout: cfg.DefaultTimeout,
		maxBody:        cfg.MaxBodyBytes,
		minInterval:    cfg.MinInterval,
		logger:         cfg.Logger,
	}

	if s.transport == nil {
		s.transport = http.DefaultTransport
	}
	if s.defaultTimeout <= 0 {
		s.defaultTimeout = defaultTimeout
	}
	if s.maxBody <= 0 {
		s.maxBody = defaultMaxBodyBytes
	}
	if s.logger == nil {
		s.logger = logger.Discard()
	}

	s.client = &http.Client{
		Transport: s.transport,
		// 恒不跟随重定向：学习通需要逐跳读取 Set-Cookie，
		// 自动跟随会丢掉中间跳的响应头。
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return s
}

// Do 发送一次请求，并把原始响应交回调用方。
//
// 行为约定：
//   - 非 2xx 返回 *StatusError，同时 Response 非 nil；
//   - 超时 / 取消 / 传输失败 / 响应体超限各有独立错误类型；
//   - 不自动重试、不跟随重定向、不解析 body、不处理 cookie。
func (s *Sender) Do(ctx context.Context, r *Request) (*Response, error) {
	if r == nil {
		return nil, fmt.Errorf("请求不能为空")
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = s.defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := s.wait(ctx); err != nil {
		return nil, err
	}

	started := time.Now()
	method := r.Method
	if method == "" {
		method = http.MethodGet
	}

	var body io.Reader
	if len(r.Body) > 0 {
		body = bytes.NewReader(r.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.URL, body)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	for k, v := range r.Header {
		if strings.EqualFold(k, "Host") {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		classified := classifyError(ctx, err, "请求")
		s.logRequest(method, r.URL, 0, 0, nil, classified, time.Since(started))
		return nil, classified
	}
	defer func() { _ = resp.Body.Close() }()

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, s.maxBody+1))
	if readErr != nil {
		classified := classifyError(ctx, readErr, "读取响应")
		s.logRequest(method, r.URL, resp.StatusCode, int64(len(raw)), resp.Header, classified, time.Since(started))
		return nil, classified
	}
	if int64(len(raw)) > s.maxBody {
		err := &TooLargeError{Limit: s.maxBody, Got: int64(len(raw))}
		s.logRequest(method, r.URL, resp.StatusCode, int64(len(raw)), resp.Header, err, time.Since(started))
		return nil, err
	}

	out := &Response{
		Status:     resp.StatusCode,
		Header:     resp.Header,
		Raw:        raw,
		SetCookies: append([]string(nil), resp.Header.Values("Set-Cookie")...),
		ReceivedAt: time.Now().UTC(),
	}
	if loc, err := resp.Location(); err == nil {
		out.Location = loc.String()
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		statusErr := &StatusError{Code: resp.StatusCode}
		s.logRequest(method, r.URL, resp.StatusCode, int64(len(raw)), resp.Header, statusErr, time.Since(started))
		return out, statusErr
	}

	s.logRequest(method, r.URL, resp.StatusCode, int64(len(raw)), resp.Header, nil, time.Since(started))
	return out, nil
}

// wait 在实例级最小间隔内等待。并发调用会被这把锁自然摊平。
func (s *Sender) wait(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.minInterval <= 0 {
		s.lastSent = time.Now()
		return nil
	}

	if wait := s.minInterval - time.Since(s.lastSent); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return classifyError(ctx, ctx.Err(), "限速等待")
		case <-timer.C:
		}
	}
	s.lastSent = time.Now()
	return nil
}

func (s *Sender) logRequest(method, rawURL string, status int, bodyBytes int64, header http.Header, err error, elapsed time.Duration) {
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	s.logger.Debug("http",
		"method", method,
		"url", logger.RedactURL(rawURL),
		"status", status,
		"duration_ms", elapsed.Milliseconds(),
		"body_bytes", bodyBytes,
		"resp_headers", logger.RedactHeaders(header),
		"error", errText,
	)
}

// classifyError 把 net/http 与 context 的错误收敛成调用方可分辨的类型。
func classifyError(ctx context.Context, err error, op string) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.Canceled) {
			return &CanceledError{}
		}
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return &TimeoutError{Op: op}
		}
	}
	if errors.Is(err, context.Canceled) {
		return &CanceledError{}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &TimeoutError{Op: op}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &TimeoutError{Op: op}
	}
	return &TransportError{Err: err}
}
