// Package pigai 实现批改网的微信扫码登录与作业抓取
//
// 几条刻意保留的陷阱：
//   - 接口一律返回 HTTP 200，成败只看 JSON 信封的 error 字段；
//     error=302 表示未登录，不能当成「今天没作业」
//   - 所有请求必须带桌面浏览器 User-Agent 和 _display=json
//   - sender 不跟随重定向，扫码换号必须逐跳读取 Location 并累积 Set-Cookie
//   - _JUKU_USER 是 URL 编码的 JSON，字段语义未证实，整体当不透明凭据回放
//   - 详情响应里带老师的密码摘要、邮箱、IP 与 CSRF token，只解析白名单字段
//   - 截止时间是 10 位 Unix 秒；"0" 表示未设置，不是 1970 年
package pigai

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
	// DefaultBaseURL 是批改网的站点域名
	DefaultBaseURL = "https://www.pigai.org"
	// DefaultWeixinBaseURL 是微信开放平台的扫码域名
	DefaultWeixinBaseURL = "https://open.weixin.qq.com"
	// DefaultLongPollURL 是微信扫码状态的长轮询域名
	DefaultLongPollURL = "https://long.open.weixin.qq.com"

	// weixinAppID 是批改网在微信开放平台的网站应用 appid
	weixinAppID = "wxbf58f347fef4fe8a"
	// weixinRedirect 是扫码确认后的回调地址
	weixinRedirect = "https://www.pigai.org/d3/weixin/login.php"
	// weixinState 是回调时校验的 state，批改网固定用域名
	weixinState = "www.pigai.org"

	// userAgent 伪装成桌面浏览器。批改网会拦截 Go 默认的 "Go-http-client/1.1"
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"

	// qrDefaultTTL 是二维码的展示有效期，页面按 5 分钟提示
	qrDefaultTTL = 5 * time.Minute
	// pollTimeout 要长于微信长轮询的 15 秒阻塞，否则每次都被自己打断
	pollTimeout = 35 * time.Second
	// maxLoginHops 是扫码换号时手工跟随 302 的上限
	maxLoginHops = 10
)

// Options 构造批改网适配器。Sender 由 cmd/app 注入，单独限速 250ms
type Options struct {
	BaseURL         string
	WeixinBaseURL   string
	LongPollBaseURL string
	Sender          *sender.Sender
	Logger          logger.Logger
	Now             func() time.Time
}

// Client 同时实现 domain.QRLogin 与 domain.Fetcher
type Client struct {
	baseURL         string
	weixinBaseURL   string
	longPollBaseURL string
	// loginHosts 是换号跳转允许落在的主机：站点本身与去掉 www. 的主域，
	// 两者的子域也算。跨出这个集合说明链路异常，不能把 cookie 送出去
	loginHosts []string
	http       *sender.Sender
	logger     logger.Logger
	now        func() time.Time

	// qrSessions 保存每张二维码的站点 cookie 袋与上一次 errcode。
	// 长轮询的 last 参数要用上一次的状态码，站点 cookie 要留到换号链路
	qrMu       sync.Mutex
	qrSessions map[string]*qrSession
}

func New(opts Options) *Client {
	c := &Client{
		baseURL:         strings.TrimRight(opts.BaseURL, "/"),
		weixinBaseURL:   strings.TrimRight(opts.WeixinBaseURL, "/"),
		longPollBaseURL: strings.TrimRight(opts.LongPollBaseURL, "/"),
		http:            opts.Sender,
		logger:          opts.Logger,
		now:             opts.Now,
		qrSessions:      make(map[string]*qrSession),
	}
	if c.baseURL == "" {
		c.baseURL = DefaultBaseURL
	}
	if c.weixinBaseURL == "" {
		c.weixinBaseURL = DefaultWeixinBaseURL
	}
	if c.longPollBaseURL == "" {
		c.longPollBaseURL = DefaultLongPollURL
	}
	if host := hostOf(c.baseURL); host != "" {
		c.loginHosts = []string{host}
		if root := strings.TrimPrefix(host, "www."); root != host && root != "" {
			c.loginHosts = append(c.loginHosts, root)
		}
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
func (c *Client) Platform() entity.Platform { return entity.PlatformPigai }

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// isLoginHost 判断换号跳转是否仍在允许的主机集合内
func (c *Client) isLoginHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return false
	}
	for _, allowed := range c.loginHosts {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

// cookieJar 是平台内全局 cookie 袋。
//
// 刻意不做 host / path 过滤，也不解析 _JUKU_USER：换号链路会跨
// www.pigai.org 与 open.weixin.qq.com，cookiejar 会按 host-only 规则
// 丢掉 PHPSESSID 这类凭据；_JUKU_USER 的字段语义未证实，原样回放最稳
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

func (j *cookieJar) get(name string) string { return j.values[name] }

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
func decodeSecret(raw json.RawMessage) (*entity.PigaiSecret, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: 会话为空", entity.ErrAuthRequired)
	}
	var secret entity.PigaiSecret
	if err := json.Unmarshal(raw, &secret); err != nil {
		return nil, fmt.Errorf("%w: 解析批改网会话失败: %v", entity.ErrAuthRequired, err)
	}
	if len(secret.Cookies) == 0 {
		return nil, fmt.Errorf("%w: 会话缺少 cookie", entity.ErrAuthRequired)
	}
	return &secret, nil
}

func (c *Client) newSession(jar *cookieJar) (*entity.Session, error) {
	raw, err := json.Marshal(entity.PigaiSecret{
		Cookies:   jar.toEntities(),
		UpdatedAt: c.now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	return &entity.Session{
		Platform:  entity.PlatformPigai,
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

// get 发送带全量 cookie 的 GET
func (c *Client) get(ctx context.Context, rawURL string, jar *cookieJar) (*sender.Response, error) {
	return c.do(ctx, http.MethodGet, rawURL, jar, 0)
}

// doRaw 与 get 相同，但把 sender 的原始错误原样返回。
// 需要按状态码区分 302 跳转的地方用它：classifySendError 会把
// sender.StatusError 折成领域错误，之后 errors.As 就找不到状态码了
func (c *Client) doRaw(ctx context.Context, rawURL string, jar *cookieJar, timeout time.Duration) (*sender.Response, error) {
	return c.do(ctx, http.MethodGet, rawURL, jar, timeout)
}

func (c *Client) do(ctx context.Context, method, rawURL string, jar *cookieJar, timeout time.Duration) (*sender.Response, error) {
	header := map[string]string{
		"Accept":     "application/json, text/plain, */*",
		"User-Agent": userAgent,
		"Referer":    c.baseURL + "/",
	}
	if cookie := jar.header(); cookie != "" {
		header["Cookie"] = cookie
	}
	resp, err := c.http.Do(ctx, &sender.Request{
		Method:  method,
		URL:     rawURL,
		Header:  header,
		Timeout: timeout,
	})
	if resp != nil {
		jar.add(resp.SetCookies)
	}
	return resp, err
}

func (c *Client) getClassified(ctx context.Context, rawURL string, jar *cookieJar) (*sender.Response, error) {
	resp, err := c.do(ctx, http.MethodGet, rawURL, jar, 0)
	if err != nil {
		return resp, classifySendError(err)
	}
	return resp, nil
}

// envelope 是批改网所有 JSON 接口的外层信封
type envelope struct {
	Data     json.RawMessage `json:"data"`
	Error    int             `json:"error"`
	ErrorDes string          `json:"error_des"`
}

// envelopeError 把信封里的 error 折成领域错误。
// 批改网的 HTTP 状态恒为 200，成败只能看这里
func envelopeError(env *envelope) error {
	switch env.Error {
	case 0:
		return nil
	case 302:
		return fmt.Errorf("%w: %s", entity.ErrAuthExpired, describe(env))
	default:
		return fmt.Errorf("%w: %s (error=%d)", entity.ErrBusiness, describe(env), env.Error)
	}
}

func describe(env *envelope) string {
	if msg := strings.TrimSpace(env.ErrorDes); msg != "" {
		return msg
	}
	return "平台未给出原因"
}

// fetchEnvelope 请求接口并解析信封，非 0 error 直接返回领域错误
func (c *Client) fetchEnvelope(ctx context.Context, rawURL string, jar *cookieJar) (*envelope, error) {
	resp, err := c.getClassified(ctx, rawURL, jar)
	if err != nil {
		return nil, err
	}
	var env envelope
	if err := json.Unmarshal(resp.Raw, &env); err != nil {
		return nil, fmt.Errorf("%w: 响应不是 JSON: %v", entity.ErrPlatformChanged, err)
	}
	if err := envelopeError(&env); err != nil {
		return nil, err
	}
	return &env, nil
}

// apiURL 拼接带 _display=json 的接口地址
func (c *Client) apiURL(query url.Values) string {
	q := url.Values{"_display": {"json"}}
	for k, v := range query {
		q[k] = v
	}
	return c.baseURL + "/index.php?" + q.Encode()
}

// pageURL 拼接用户点开能看的页面地址，不带 _display。
// 存进作业的详情链接必须是这样的人看的地址，抓取时再用 withJSONDisplay 换回接口
func (c *Client) pageURL(query url.Values) string {
	return c.baseURL + "/index.php?" + query.Encode()
}

// withJSONDisplay 给页面地址补上 _display=json，同一套 query 换个视图取数据
func withJSONDisplay(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	if q.Get("_display") == "json" {
		return rawURL, nil
	}
	q.Set("_display", "json")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// shanghai 是平台时间字段的时区
var shanghai = time.FixedZone("CST", 8*60*60)

// parseUnixSeconds 解析 10 位 Unix 秒。
// "" 与 "0" 表示平台没有设置时间，返回 nil；13 位毫秒或其他长度一律报错，
// 不能按秒读，否则会得到 1970 年或几万年后的截止时间
func parseUnixSeconds(raw string) (*time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" || s == "0" {
		return nil, nil
	}
	if len(s) != 10 {
		return nil, fmt.Errorf("时间字段不是 10 位 Unix 秒: %q", raw)
	}
	var sec int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return nil, fmt.Errorf("时间字段不是 10 位 Unix 秒: %q", raw)
		}
		sec = sec*10 + int64(r-'0')
	}
	t := time.Unix(sec, 0).In(shanghai)
	return &t, nil
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
		// sender 不跟随重定向；数据接口被 3xx 带走说明会话失效
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
	default:
		return entity.IssueInternal
	}
}
