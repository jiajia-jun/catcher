// Package chaoxing 实现学习通的密码登录、会话续期与作业抓取
//
// 接口依据 docs/接口文档/学习通.md。几条刻意保留的陷阱：
//   - 登录在 passport2.xuexitong.com，数据在 mooc1-api.chaoxing.com，域名分裂
//   - sender 不跟随重定向，登录必须手工跟随 ≤10 跳并逐跳累积 Set-Cookie
//   - cookie 表不按 host 过滤、不硬编码名字；JSESSIONID 是 host-only
//   - 登录态只看课程列表的 result，不看 HTTP 状态码；失效时可能是 200 + 登录页 HTML
//   - 状态与剩余时间只从 aria-label 取，三个定界符从右往左找
//   - redPoint 是未读标记，不是未交
//   - 课程 id 必须按原始字面量取，不能过 float64
//   - 标题里的日期不是截止时间；剩余时间要当场换算成绝对时刻
package chaoxing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"catcher/internal/domain/entity"
	"catcher/internal/infra/sender"
	"catcher/pkg/logger"
)

const (
	// DefaultBaseURL 是课程与作业数据接口的域名
	DefaultBaseURL = "https://mooc1-api.chaoxing.com"
	// DefaultLoginBaseURL 是密码登录接口的域名
	DefaultLoginBaseURL = "https://passport2.xuexitong.com"
	// DefaultQRBaseURL 是扫码登录接口的域名。它和密码登录不是同一个站点
	DefaultQRBaseURL = "https://passport2.chaoxing.com"

	// maxLoginRedirects 是手工跟随 302 的上限
	maxLoginRedirects = 10
	// maxDetailRedirects 是详情页跟随跳转的上限（Android 接口 → H5 页面）
	maxDetailRedirects = 5

	// userAgent 伪装成桌面浏览器。
	// 学习通会拦截 Go 默认的 "Go-http-client/1.1"：登录直接 302 到
	// passport2-api.chaoxing.com/views/error/passport404.html，表现为「登录返回页面而不是 JSON」
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
)

// Options 构造学习通适配器。Sender 由 cmd/app 注入，单独限速 250ms
type Options struct {
	BaseURL      string
	LoginBaseURL string
	QRBaseURL    string
	Sender       *sender.Sender
	Logger       logger.Logger
	Now          func() time.Time
	// QRPollInterval 是扫码轮询间隔，默认 3 秒；测试里调小以免拖慢用例
	QRPollInterval time.Duration
}

// Client 同时实现 domain.PasswordAuthenticator、domain.QRLogin 与 domain.Fetcher
type Client struct {
	baseURL        string
	loginBaseURL   string
	qrBaseURL      string
	qrPollInterval time.Duration
	http           *sender.Sender
	logger         logger.Logger
	now            func() time.Time

	// qrSessions 保存每张二维码的 uuid / enc 与同一个 cookie 袋。
	// 轮询必须复用 BeginQR 时拿到的 JSESSIONID，丢了服务端会返回 type=2
	qrMu       sync.Mutex
	qrSessions map[string]*qrSession
}

func New(opts Options) *Client {
	c := &Client{
		baseURL:        strings.TrimRight(opts.BaseURL, "/"),
		loginBaseURL:   strings.TrimRight(opts.LoginBaseURL, "/"),
		qrBaseURL:      strings.TrimRight(opts.QRBaseURL, "/"),
		qrPollInterval: opts.QRPollInterval,
		http:           opts.Sender,
		logger:         opts.Logger,
		now:            opts.Now,
		qrSessions:     make(map[string]*qrSession),
	}
	if c.baseURL == "" {
		c.baseURL = DefaultBaseURL
	}
	if c.loginBaseURL == "" {
		c.loginBaseURL = DefaultLoginBaseURL
	}
	if c.qrBaseURL == "" {
		c.qrBaseURL = DefaultQRBaseURL
	}
	if c.qrPollInterval <= 0 {
		c.qrPollInterval = qrPollInterval
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
func (c *Client) Platform() entity.Platform { return entity.PlatformChaoxing }

// cookieJar 是平台内全局 cookie 袋。
//
// 刻意不做 host / path 过滤：登录链会跨 passport2 与 mooc1-api 两个域名，
// cookiejar 会按 host-only 规则丢掉 JSESSIONID 这类凭据
type cookieJar struct {
	order   []string
	values  map[string]string
	changed bool
}

func newCookieJar(cookies []entity.Cookie) *cookieJar {
	jar := &cookieJar{values: make(map[string]string, len(cookies))}
	for _, cookie := range cookies {
		if cookie.Name == "" {
			continue
		}
		jar.set(cookie.Name, cookie.Value)
	}
	jar.changed = false
	return jar
}

// add 累积原始 Set-Cookie 行。只取第一段 name=value，不解析 Domain/Path/过期
func (j *cookieJar) add(lines []string) {
	for _, line := range lines {
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if i := strings.IndexByte(value, ';'); i >= 0 {
			value = value[:i]
		}
		j.set(name, strings.TrimSpace(value))
	}
}

func (j *cookieJar) set(name, value string) {
	if old, ok := j.values[name]; ok && old == value {
		return
	}
	if _, ok := j.values[name]; !ok {
		j.order = append(j.order, name)
	}
	j.values[name] = value
	j.changed = true
}

func (j *cookieJar) header() string {
	if len(j.order) == 0 {
		return ""
	}
	parts := make([]string, 0, len(j.order))
	for _, name := range j.order {
		parts = append(parts, name+"="+j.values[name])
	}
	return strings.Join(parts, "; ")
}

func (j *cookieJar) toEntities() []entity.Cookie {
	out := make([]entity.Cookie, 0, len(j.order))
	for _, name := range j.order {
		out = append(out, entity.Cookie{Name: name, Value: j.values[name]})
	}
	return out
}

// decodeSecret 读取会话里的 cookie 袋
func decodeSecret(raw json.RawMessage) (*entity.ChaoxingSecret, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: 会话为空", entity.ErrAuthRequired)
	}
	var secret entity.ChaoxingSecret
	if err := json.Unmarshal(raw, &secret); err != nil {
		return nil, fmt.Errorf("%w: 解析学习通会话失败: %v", entity.ErrAuthRequired, err)
	}
	if len(secret.Cookies) == 0 {
		return nil, fmt.Errorf("%w: 会话缺少 cookie", entity.ErrAuthRequired)
	}
	return &secret, nil
}

func (c *Client) newSession(jar *cookieJar) (*entity.Session, error) {
	raw, err := json.Marshal(entity.ChaoxingSecret{
		Cookies:   jar.toEntities(),
		UpdatedAt: c.now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	return &entity.Session{
		Platform:  entity.PlatformChaoxing,
		Secret:    raw,
		UpdatedAt: c.now().UTC(),
	}, nil
}

// persistCookies 在 cookie 发生变化时立即交给上层加密保存
func (c *Client) persistCookies(jar *cookieJar, opts entity.FetchOptions) error {
	if !jar.changed || opts.OnSessionUpdated == nil {
		return nil
	}
	session, err := c.newSession(jar)
	if err != nil {
		return err
	}
	if err := opts.OnSessionUpdated(*session); err != nil {
		return err
	}
	jar.changed = false
	return nil
}

// get 发送带全量 cookie 的 GET。学习通数据接口都走这里
func (c *Client) get(ctx context.Context, rawURL string, jar *cookieJar) (*sender.Response, error) {
	return c.do(ctx, http.MethodGet, rawURL, nil, jar)
}

func (c *Client) do(ctx context.Context, method, rawURL string, body []byte, jar *cookieJar) (*sender.Response, error) {
	resp, err := c.doRaw(ctx, method, rawURL, body, jar)
	if err != nil {
		return resp, classifySendError(err)
	}
	return resp, nil
}

// doRaw 与 do 相同，但把 sender 的原始错误原样返回。
// 需要区分 302 跳转与真正失败的地方用它：classifySendError 会把
// sender.StatusError 折成领域错误，之后 errors.As 就找不到状态码了
func (c *Client) doRaw(ctx context.Context, method, rawURL string, body []byte, jar *cookieJar) (*sender.Response, error) {
	header := map[string]string{
		"Accept":     "text/html,application/json;q=0.9,*/*;q=0.8",
		"User-Agent": userAgent,
	}
	if cookie := jar.header(); cookie != "" {
		header["Cookie"] = cookie
	}
	resp, err := c.http.Do(ctx, &sender.Request{
		Method: method,
		URL:    rawURL,
		Header: header,
		Body:   body,
	})
	if resp != nil {
		jar.add(resp.SetCookies)
	}
	return resp, err
}

// getFollowing 用 GET 跟随重定向，专供详情页。
//
// 作业列表给的详情链接是 Android 接口路径 mtaskmsgspecial，服务器会 302 到
// H5 页面 /mooc-ans/work/phone/task-work —— 这是正常跳转，不是登录失效。
// 只有跳到 passport 登录页才算会话失效
func (c *Client) getFollowing(ctx context.Context, rawURL string, jar *cookieJar) (*sender.Response, error) {
	for hop := 0; hop <= maxDetailRedirects; hop++ {
		resp, err := c.get(ctx, rawURL, jar)
		if resp != nil && resp.Status >= 300 && resp.Status < 400 {
			location := strings.TrimSpace(resp.Location)
			if location == "" {
				return nil, fmt.Errorf("%w: 详情页重定向缺少 Location（HTTP %d）", entity.ErrPlatformChanged, resp.Status)
			}
			if isLoginURL(location) {
				return nil, fmt.Errorf("%w: 详情页被重定向到登录页", entity.ErrAuthExpired)
			}
			rawURL = location
			continue
		}
		if err != nil {
			return resp, err
		}
		return resp, nil
	}
	return nil, fmt.Errorf("%w: 详情页重定向超过 %d 跳", entity.ErrPlatformChanged, maxDetailRedirects)
}

// isLoginURL 判断重定向目标是不是登录页
func isLoginURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(u.Host), "passport") ||
		strings.Contains(strings.ToLower(u.Path), "login")
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
		// sender 不跟随重定向；数据接口被 3xx 带去登录页说明会话失效
		case status.Code >= 300 && status.Code < 400:
			return fmt.Errorf("%w: 请求被重定向（HTTP %d）", entity.ErrAuthExpired, status.Code)
		case status.Code == 401 || status.Code == 403:
			return fmt.Errorf("%w: HTTP %d", entity.ErrAuthExpired, status.Code)
		case status.Code >= 500:
			return fmt.Errorf("%w: %v", entity.ErrNetwork, err)
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
	case errors.Is(err, entity.ErrPlatformChanged):
		return entity.IssueParse
	default:
		return entity.IssueInternal
	}
}
