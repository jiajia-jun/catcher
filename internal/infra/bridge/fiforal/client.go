// Package fiforal 实现 FiF 口语的微信扫码登录与任务抓取
package fiforal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"catcher/internal/domain/entity"
	"catcher/internal/infra/sender"
	"catcher/pkg/logger"
)

const (
	// DefaultOralURL 是口语学生入口，也是登录链的 service
	DefaultOralURL = "https://oral.fifedu.com/kyxl-web/student/index.do"
	// DefaultLoginPageURL 是播种 cookie 的统一登录页
	DefaultLoginPageURL = "https://www.fifedu.com/iplat/fifLogin/index.html"
	// DefaultPassBaseURL 是统一登录与微信回调所在的主机
	DefaultPassBaseURL = "https://pass.fifedu.com"
	// DefaultWeixinBaseURL 是微信开放平台的扫码域名
	DefaultWeixinBaseURL = "https://open.weixin.qq.com"
	// DefaultLongPollBaseURL 是微信扫码状态的长轮询域名
	DefaultLongPollBaseURL = "https://long.open.weixin.qq.com"
	// DefaultTaskBaseURL 是任务接口所在的主机
	DefaultTaskBaseURL = "https://moral.fifedu.com"
	// DefaultH5BaseURL 是学生 H5 的入口，详情路由挂在它的 fragment 上
	DefaultH5BaseURL = "https://static.fifedu.com/static/fiforal/kyxl-web-static/student-h5/index.html"
	// DefaultSSOService 是 CAS 登录成功后的落地入口
	DefaultSSOService = "https://www.fifedu.com/iplat/ssoservice"
	// DefaultCASServer 是 CAS 服务
	DefaultCASServer = "https://cycore.fifedu.com/cas-server"

	// taskListPath 是任务列表接口
	taskListPath = "/kyxl-app/stu/task/teaTaskList"
	// defaultSource 是 H5 的 source，fragment 缺失时使用
	defaultSource = "10003"

	// userAgent 是请求使用的桌面浏览器 UA 标识
	// FiF 是否拦截 Go 默认 UA 尚未验证，按其他平台的惯例统一带上
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"

	// qrDefaultTTL 是二维码的展示有效期，页面按 5 分钟倒计时
	qrDefaultTTL = 5 * time.Minute
	// pollTimeout 长于微信长轮询约 15 秒的阻塞
	pollTimeout = 35 * time.Second
	// maxLoginHops 是取码链路手工跟随 302 的上限
	maxLoginHops = 10
	// maxLandingHops 是落地链的上限，首次约 9 跳，留足余量
	maxLandingHops = 20
	// tokenRefreshMargin 是提前换 token 的余量
	tokenRefreshMargin = 5 * time.Minute
)

// Options 是 FiF 口语适配器的构造参数，Sender 由 cmd/app 注入并单独限速
type Options struct {
	OralURL         string
	LoginPageURL    string
	PassBaseURL     string
	WeixinBaseURL   string
	LongPollBaseURL string
	TaskBaseURL     string
	H5BaseURL       string
	SSOService      string
	CASServer       string
	Sender          *sender.Sender
	Logger          logger.Logger
	Now             func() time.Time
}

// Client 同时实现 domain.QRLogin 与 domain.Fetcher
// H5 token 只放内存，进程重启后用已存 cookie 重新换取
type Client struct {
	oralURL         string
	loginPageURL    string
	passBaseURL     string
	weixinBaseURL   string
	longPollBaseURL string
	taskBaseURL     string
	h5BaseURL       string
	ssoService      string
	casServer       string

	http   *sender.Sender
	logger logger.Logger
	now    func() time.Time

	tokenMu sync.Mutex
	token   *h5Token

	qrMu       sync.Mutex
	qrSessions map[string]*qrSession
}

func New(opts Options) *Client {
	c := &Client{
		oralURL:         strings.TrimRight(opts.OralURL, "/"),
		loginPageURL:    strings.TrimRight(opts.LoginPageURL, "/"),
		passBaseURL:     strings.TrimRight(opts.PassBaseURL, "/"),
		weixinBaseURL:   strings.TrimRight(opts.WeixinBaseURL, "/"),
		longPollBaseURL: strings.TrimRight(opts.LongPollBaseURL, "/"),
		taskBaseURL:     strings.TrimRight(opts.TaskBaseURL, "/"),
		h5BaseURL:       strings.TrimRight(opts.H5BaseURL, "/"),
		ssoService:      strings.TrimRight(opts.SSOService, "/"),
		casServer:       strings.TrimRight(opts.CASServer, "/"),
		http:            opts.Sender,
		logger:          opts.Logger,
		now:             opts.Now,
		qrSessions:      make(map[string]*qrSession),
	}
	// 入口 URL 自带路径，构造时只去掉末尾斜杠
	if opts.OralURL == "" {
		c.oralURL = DefaultOralURL
	}
	if opts.LoginPageURL == "" {
		c.loginPageURL = DefaultLoginPageURL
	}
	if opts.PassBaseURL == "" {
		c.passBaseURL = DefaultPassBaseURL
	}
	if opts.WeixinBaseURL == "" {
		c.weixinBaseURL = DefaultWeixinBaseURL
	}
	if opts.LongPollBaseURL == "" {
		c.longPollBaseURL = DefaultLongPollBaseURL
	}
	if opts.TaskBaseURL == "" {
		c.taskBaseURL = DefaultTaskBaseURL
	}
	if opts.H5BaseURL == "" {
		c.h5BaseURL = DefaultH5BaseURL
	}
	if opts.SSOService == "" {
		c.ssoService = DefaultSSOService
	}
	if opts.CASServer == "" {
		c.casServer = DefaultCASServer
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
func (c *Client) Platform() entity.Platform { return entity.PlatformFiforal }

func (c *Client) get(ctx context.Context, rawURL string, jar *cookieJar) (*sender.Response, error) {
	return c.do(ctx, "GET", rawURL, jar, nil, 0, nil)
}

// doRaw 把 sender 的原始错误原样返回，供需要按状态码区分 302 的落地链使用
func (c *Client) doRaw(ctx context.Context, method, rawURL string, jar *cookieJar, timeout time.Duration) (*sender.Response, error) {
	return c.do(ctx, method, rawURL, jar, nil, timeout, nil)
}

func (c *Client) do(ctx context.Context, method, rawURL string, jar *cookieJar,
	body []byte, timeout time.Duration, headers map[string]string) (*sender.Response, error) {
	// fragment 里可能是 H5 token，请求发出前剥离
	target := stripFragment(rawURL)
	header := map[string]string{
		"Accept":     "application/json, text/plain, */*",
		"User-Agent": userAgent,
	}
	if len(body) > 0 {
		header["Content-Type"] = "application/x-www-form-urlencoded"
	}
	for k, v := range headers {
		header[k] = v
	}
	if cookie := jar.header(target); cookie != "" {
		header["Cookie"] = cookie
	}

	resp, err := c.http.Do(ctx, &sender.Request{
		Method:  method,
		URL:     target,
		Header:  header,
		Body:    body,
		Timeout: timeout,
	})
	if resp != nil {
		jar.add(target, resp.SetCookies)
	}
	return resp, err
}

// safeRequestError 把带凭据的登录链请求失败收敛成不含 URL 的错误
// CAS 的查询串里有 username 与 Base64 密码，transport 错误串会原样回显整个 URL
func safeRequestError(action string, err error) error {
	var canceled *sender.CanceledError
	if errors.As(err, &canceled) {
		return fmt.Errorf("%w: %v", entity.ErrCanceled, canceled)
	}
	var timeout *sender.TimeoutError
	var transport *sender.TransportError
	if errors.As(err, &timeout) || errors.As(err, &transport) {
		return fmt.Errorf("%s：暂时无法连接 FiF，请稍后重试（%w）", action, entity.ErrNetwork)
	}
	var status *sender.StatusError
	if errors.As(err, &status) {
		switch {
		case status.Code == 401 || status.Code == 403:
			return fmt.Errorf("%w: %s 被拒绝（HTTP %d）", entity.ErrAuthExpired, action, status.Code)
		case status.Code >= 500:
			return fmt.Errorf("%w: %s 服务端异常（HTTP %d）", entity.ErrNetwork, action, status.Code)
		default:
			return fmt.Errorf("%w: %s 返回 HTTP %d", entity.ErrPlatformChanged, action, status.Code)
		}
	}
	var tooLarge *sender.TooLargeError
	if errors.As(err, &tooLarge) {
		return fmt.Errorf("%w: %s 响应体超限", entity.ErrPlatformChanged, action)
	}
	return fmt.Errorf("%w: %s 请求失败", entity.ErrPlatformChanged, action)
}

// classifySendError 把数据接口的 sender 错误映射到领域错误
func classifySendError(err error) error {
	var canceled *sender.CanceledError
	if errors.As(err, &canceled) {
		return fmt.Errorf("%w: %v", entity.ErrCanceled, err)
	}
	var timeout *sender.TimeoutError
	var transport *sender.TransportError
	if errors.As(err, &timeout) || errors.As(err, &transport) {
		return fmt.Errorf("%w: %v", entity.ErrNetwork, err)
	}
	var status *sender.StatusError
	if errors.As(err, &status) {
		switch {
		// sender 将重定向作为状态码返回，数据接口被 3xx 带走时按会话失效处理
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
	case errors.Is(err, entity.ErrAuthExpired), errors.Is(err, entity.ErrAuthRequired):
		return entity.IssueAuth
	case errors.Is(err, entity.ErrCanceled):
		return entity.IssueCanceled
	case errors.Is(err, entity.ErrPlatformChanged):
		return entity.IssueParse
	case errors.Is(err, entity.ErrBusiness):
		return entity.IssueBusiness
	default:
		return entity.IssueInternal
	}
}

// decodeSecret 读取会话里的 cookie 表
func decodeSecret(raw json.RawMessage) (*entity.FiforalSecret, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: 会话为空", entity.ErrAuthRequired)
	}
	var secret entity.FiforalSecret
	if err := json.Unmarshal(raw, &secret); err != nil {
		return nil, fmt.Errorf("%w: 解析 FiF 口语会话失败: %v", entity.ErrAuthRequired, err)
	}
	if len(secret.Cookies) == 0 {
		return nil, fmt.Errorf("%w: 会话缺少 cookie", entity.ErrAuthRequired)
	}
	return &secret, nil
}

func (c *Client) newSession(jar *cookieJar, userID string) (*entity.Session, error) {
	raw, err := json.Marshal(entity.FiforalSecret{
		Cookies:   jar.toEntities(),
		UserID:    userID,
		UpdatedAt: c.now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	return &entity.Session{
		Platform:  entity.PlatformFiforal,
		Secret:    raw,
		UpdatedAt: c.now().UTC(),
	}, nil
}

// persistCookies 在 cookie 发生变化时立即交给上层加密保存
func (c *Client) persistCookies(jar *cookieJar, userID string, opts entity.FetchOptions) error {
	if !jar.changed || opts.OnSessionUpdated == nil {
		return nil
	}
	session, err := c.newSession(jar, userID)
	if err != nil {
		return err
	}
	if err := opts.OnSessionUpdated(*session); err != nil {
		return err
	}
	jar.changed = false
	return nil
}

// isFifeduHost 判断主机是否属于 fifedu.com
func isFifeduHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "fifedu.com" || strings.HasSuffix(host, ".fifedu.com")
}

func isWeixinHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "weixin.qq.com" || strings.HasSuffix(host, ".weixin.qq.com")
}

// stripFragment 去掉 URL 的 fragment，解析失败时按第一个 # 截断
func stripFragment(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		if i := strings.IndexByte(raw, '#'); i >= 0 {
			return raw[:i]
		}
		return raw
	}
	if u.Fragment == "" {
		return raw
	}
	u.Fragment = ""
	return u.String()
}

// sniffImageMime 按文件头判断图片类型
// 二维码接口的 Content-Type 与实际内容可能不一致
func sniffImageMime(raw []byte) string {
	switch {
	case bytes.HasPrefix(raw, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(raw, []byte("\xff\xd8\xff")):
		return "image/jpeg"
	case bytes.HasPrefix(raw, []byte("GIF8")):
		return "image/gif"
	default:
		return ""
	}
}

// shanghai 是平台时间字段的时区
var shanghai = time.FixedZone("CST", 8*60*60)
