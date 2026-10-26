package chaoxing

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"catcher/internal/domain/entity"
	"catcher/internal/infra/sender"
)

const (
	qrLoginPath  = "/cloudscanlogin"
	qrImagePath  = "/createqr"
	qrStatusPath = "/getauthstatus"

	// qrPollInterval 与页面一致：setInterval(loopStatus, 3000)
	qrPollInterval = 3 * time.Second
	// qrPollLimit 是页面自己的上限：50 次后显示「二维码已失效」
	qrPollLimit = 50

	// qrDefaultRefer 是页面里 pcrefer 缺失时的兜底落地页
	qrDefaultRefer = "https://v1.chaoxing.com/backSchool/toLogin?source=num8"
)

// qrSession 是一张二维码在服务端的上下文。
// uuid / enc 由扫码页的隐藏 input 下发，cookie 袋必须与取图、轮询共用
type qrSession struct {
	uuid      string
	enc       string
	pcrefer   string
	jar       *cookieJar
	expiresAt time.Time
	polls     int
}

// qrStatusResp 是 /getauthstatus 的响应。
// type 是字符串：3 等待、4 已扫待确认、2 失效、6 取消、1 参数校验失败
type qrStatusResp struct {
	Status bool   `json:"status"`
	Type   string `json:"type"`
	Mes    string `json:"mes"`
}

// BeginQR 打开扫码页并取回二维码图片。
//
// 学习通的二维码不是前端算出来的，而是服务端按 uuid 生成的 PNG：
// 先用页面给的 JSESSIONID 打开 /cloudscanlogin 拿 uuid / enc，
// 再用同一个 cookie 袋请求 /createqr 取图
func (c *Client) BeginQR(ctx context.Context) (*entity.QRChallenge, error) {
	jar := newCookieJar(nil)

	pageURL := c.qrBaseURL + qrLoginPath + "?" + url.Values{
		"mobiletip": {"电脑端登录确认"},
		"time":      {strconv.FormatInt(c.now().UnixMilli(), 10)},
		"pcrefer":   {qrDefaultRefer},
	}.Encode()
	resp, err := c.get(ctx, pageURL, jar)
	if err != nil {
		return nil, fmt.Errorf("打开学习通扫码页失败: %w", err)
	}

	html := string(resp.Raw)
	uuid := hiddenInputValue(html, "uuid")
	enc := hiddenInputValue(html, "enc")
	if uuid == "" || enc == "" {
		return nil, fmt.Errorf("%w: 扫码页缺少 uuid 或 enc", entity.ErrPlatformChanged)
	}
	pcrefer := hiddenInputValue(html, "pcrefer")
	if pcrefer == "" {
		pcrefer = qrDefaultRefer
	}

	imageURL := c.qrBaseURL + qrImagePath + "?" + url.Values{
		"uuid":     {uuid},
		"xxtrefer": {""},
		"type":     {"1"},
		"clientid": {""},
		"fid":      {""},
	}.Encode()
	image, err := c.get(ctx, imageURL, jar)
	if err != nil {
		return nil, fmt.Errorf("获取学习通二维码失败: %w", err)
	}
	mime := sniffImageMime(image.Raw)
	if mime == "" {
		return nil, fmt.Errorf("%w: /createqr 返回的不是图片", entity.ErrPlatformChanged)
	}

	id, err := newQRID()
	if err != nil {
		return nil, err
	}
	expiresAt := c.now().Add(qrPollInterval * qrPollLimit)

	c.qrMu.Lock()
	c.pruneQRLocked(c.now())
	c.qrSessions[id] = &qrSession{
		uuid:      uuid,
		enc:       enc,
		pcrefer:   pcrefer,
		jar:       jar,
		expiresAt: expiresAt,
	}
	c.qrMu.Unlock()

	return &entity.QRChallenge{
		ID:        id,
		Content:   "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(image.Raw),
		ExpiresAt: expiresAt,
	}, nil
}

// PollQR 查询一次扫码状态。第一次立即查，之后每次先等 3 秒
func (c *Client) PollQR(ctx context.Context, challenge *entity.QRChallenge) (entity.QRState, *entity.Session, error) {
	if challenge == nil || challenge.ID == "" {
		return entity.QRFailed, nil, fmt.Errorf("%w: 缺少二维码 id", entity.ErrAuthFailed)
	}

	c.qrMu.Lock()
	qs := c.qrSessions[challenge.ID]
	if qs != nil {
		if !c.now().Before(qs.expiresAt) || qs.polls >= qrPollLimit {
			delete(c.qrSessions, challenge.ID)
			qs = nil
		}
	}
	first := false
	if qs != nil {
		first = qs.polls == 0
		qs.polls++
	}
	c.qrMu.Unlock()
	if qs == nil {
		return entity.QRExpired, nil, nil
	}

	if !first {
		if err := waitContext(ctx, c.qrPollInterval); err != nil {
			return entity.QRFailed, nil, fmt.Errorf("%w: %v", entity.ErrCanceled, err)
		}
	}

	form := url.Values{"enc": {qs.enc}, "uuid": {qs.uuid}}
	resp, err := c.doForm(ctx, c.qrBaseURL+qrStatusPath, form, qs.jar)
	if err != nil {
		// 超时与 5xx 都是「继续等」，页面自己也是这么做的
		if isTransientPollError(err) {
			return entity.QRWaiting, nil, nil
		}
		return entity.QRFailed, nil, classifySendError(err)
	}

	var out qrStatusResp
	if err := json.Unmarshal(resp.Raw, &out); err != nil {
		c.dropQR(challenge.ID)
		return entity.QRFailed, nil, fmt.Errorf("%w: 扫码状态不是 JSON: %v", entity.ErrPlatformChanged, err)
	}
	if out.Status {
		return c.completeQR(ctx, challenge.ID, qs)
	}

	switch out.Type {
	case "3":
		return entity.QRWaiting, nil, nil
	case "4":
		return entity.QRScanned, nil, nil
	case "2":
		c.dropQR(challenge.ID)
		return entity.QRExpired, nil, nil
	case "6":
		// 用户在手机上取消了登录，不是链路故障，给出明确提示
		c.dropQR(challenge.ID)
		return entity.QRFailed, nil, fmt.Errorf("%w: 已在手机上取消登录", entity.ErrAuthFailed)
	case "1":
		c.dropQR(challenge.ID)
		msg := strings.TrimSpace(out.Mes)
		if msg == "" {
			msg = "验证失败"
		}
		return entity.QRFailed, nil, fmt.Errorf("%w: %s", entity.ErrAuthFailed, msg)
	default:
		c.dropQR(challenge.ID)
		return entity.QRFailed, nil, fmt.Errorf("%w: 未知扫码状态 type=%q", entity.ErrPlatformChanged, out.Type)
	}
}

// completeQR 在确认登录后跟随 pcrefer 收全 cookie，并用课程列表验证登录态。
// 未确认成功响应本身是否下发 cookie，所以 pcrefer 这一跳必须走
func (c *Client) completeQR(ctx context.Context, id string, qs *qrSession) (entity.QRState, *entity.Session, error) {
	c.dropQR(id)

	if err := c.followToRefer(ctx, qs.pcrefer, qs.jar); err != nil {
		return entity.QRFailed, nil, err
	}
	if _, err := c.fetchCourseList(ctx, qs.jar); err != nil {
		return entity.QRFailed, nil, err
	}
	if len(qs.jar.order) == 0 {
		return entity.QRFailed, nil, fmt.Errorf("%w: 扫码成功但没有拿到 cookie", entity.ErrAuthFailed)
	}

	session, err := c.newSession(qs.jar)
	if err != nil {
		return entity.QRFailed, nil, err
	}
	return entity.QRSucceeded, session, nil
}

// followToRefer 逐跳跟随登录后的落地页，每跳都累积 Set-Cookie。
// sender 不跟随重定向，这里必须手工走完
func (c *Client) followToRefer(ctx context.Context, rawURL string, jar *cookieJar) error {
	for hop := 0; hop < maxLoginRedirects; hop++ {
		// 用原始错误判断 3xx：do 会把 StatusError 折成 ErrAuthExpired
		resp, err := c.doRaw(ctx, http.MethodGet, rawURL, nil, jar)
		if err == nil {
			return nil
		}

		var status *sender.StatusError
		if !errors.As(err, &status) || resp == nil {
			return classifySendError(err)
		}
		if status.Code < 300 || status.Code >= 400 {
			return classifySendError(err)
		}
		next := strings.TrimSpace(resp.Location)
		if next == "" {
			return fmt.Errorf("%w: 登录跳转缺少 Location", entity.ErrPlatformChanged)
		}
		rawURL = next
	}
	return fmt.Errorf("%w: 扫码后跳转超过 %d 跳", entity.ErrPlatformChanged, maxLoginRedirects)
}

// doForm 发送扫码轮询需要的表单 POST。
// 响应声明是 text/html，正文却是 JSON，这里不做内容类型判断
func (c *Client) doForm(ctx context.Context, rawURL string, form url.Values, jar *cookieJar) (*sender.Response, error) {
	header := map[string]string{
		"Accept":           "application/json, text/plain, */*",
		"Content-Type":     "application/x-www-form-urlencoded",
		"X-Requested-With": "XMLHttpRequest",
		"Origin":           c.qrBaseURL,
		"Referer":          c.qrBaseURL + qrLoginPath,
		"User-Agent":       userAgent,
	}
	if cookie := jar.header(); cookie != "" {
		header["Cookie"] = cookie
	}
	// 保留原始错误，调用方要按状态码区分「继续等」与真失败
	resp, err := c.http.Do(ctx, &sender.Request{
		Method: http.MethodPost,
		URL:    rawURL,
		Header: header,
		Body:   []byte(form.Encode()),
	})
	if resp != nil {
		jar.add(resp.SetCookies)
	}
	return resp, err
}

// isTransientPollError 把超时与 5xx 当成「继续等」，其余错误向上抛
func isTransientPollError(err error) bool {
	var timeout *sender.TimeoutError
	if errors.As(err, &timeout) {
		return true
	}
	var status *sender.StatusError
	if errors.As(err, &status) {
		return status.Code >= 500
	}
	return false
}

func (c *Client) dropQR(id string) {
	c.qrMu.Lock()
	delete(c.qrSessions, id)
	c.qrMu.Unlock()
}

// pruneQRLocked 清掉已经过期的码，避免手动反复换码时 map 只增不减
func (c *Client) pruneQRLocked(now time.Time) {
	for id, qs := range c.qrSessions {
		if !now.Before(qs.expiresAt) {
			delete(c.qrSessions, id)
		}
	}
}

func waitContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func newQRID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("生成二维码 id 失败: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

// hiddenInputValue 读取 <input id="..." value="...">，属性顺序两种都吃
func hiddenInputValue(html, id string) string {
	quoted := regexp.QuoteMeta(id)
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?is)<input[^>]*\bid\s*=\s*["']` + quoted + `["'][^>]*\bvalue\s*=\s*["']([^"']*)["']`),
		regexp.MustCompile(`(?is)<input[^>]*\bvalue\s*=\s*["']([^"']*)["'][^>]*\bid\s*=\s*["']` + quoted + `["']`),
	}
	for _, re := range patterns {
		if m := re.FindStringSubmatch(html); len(m) == 2 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// sniffImageMime 按文件头判断图片类型。/createqr 的 Content-Type 不可信
func sniffImageMime(raw []byte) string {
	switch {
	case bytes.HasPrefix(raw, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(raw, []byte("\xff\xd8\xff")):
		return "image/jpeg"
	case bytes.HasPrefix(raw, []byte("GIF8")):
		return "image/gif"
	case len(raw) > 12 && bytes.HasPrefix(raw, []byte("RIFF")) && bytes.Equal(raw[8:12], []byte("WEBP")):
		return "image/webp"
	default:
		return ""
	}
}
