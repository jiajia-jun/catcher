package fiforal

import (
	"context"
	"encoding/base64"
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

// 微信扫码的状态码。408 未扫码、404 已扫待确认、405 已确认、
// 402 二维码失效、403 用户取消、500 服务端异常
const (
	wxErrcodeWaiting   = 408
	wxErrcodeScanned   = 404
	wxErrcodeConfirmed = 405
	wxErrcodeExpired   = 402
	wxErrcodeCanceled  = 403
	wxErrcodeServer    = 500
)

// qrSession 是一张二维码在服务端的上下文
type qrSession struct {
	uuid      string
	state     string
	expiresAt time.Time
	// jar 只接收 fifedu 域的 cookie，换到登录态后整体落库
	jar *cookieJar
	// wxJar 是微信域名的临时 cookie 袋，不落库
	wxJar *cookieJar
	// last 是上一次轮询的 errcode，404 之后的请求要带上它
	last string
	// scannedAt 是首次收到 404 的时刻，用来量「已扫码」到「已确认」的间隔
	scannedAt time.Time
}

// BeginQR 取一张微信扫码登录的二维码。
// 取码走 getWebLoginUrl → 微信扫码页 → 二维码图片，图片转 data URL 交给前端
func (c *Client) BeginQR(ctx context.Context) (*entity.QRChallenge, error) {
	started := time.Now()
	jar := newCookieJar(nil, c.now)

	// 统一登录页只用来播种 cookie，打不开不影响后面取码
	seedURL := c.loginPageURL + "?service=" + url.QueryEscape(c.oralURL)
	if _, err := c.get(ctx, seedURL, jar); err != nil {
		c.logger.Debug("fiforal qr", "event", "open_login_page_failed", "error", err.Error())
	}

	loginURL, err := c.fetchWeixinURL(ctx, jar)
	if err != nil {
		return nil, err
	}
	state := queryValue(loginURL, "state")
	if state == "" {
		state = "iplat"
	}

	wxJar := newCookieJar(nil, c.now)
	resp, err := c.followWeixin(ctx, loginURL, wxJar)
	if err != nil {
		return nil, err
	}
	uuid := extractUUID(string(resp.Raw))
	if uuid == "" {
		return nil, fmt.Errorf("%w: 微信扫码页里找不到 uuid", entity.ErrPlatformChanged)
	}

	image, err := c.get(ctx, c.weixinBaseURL+"/connect/qrcode/"+url.PathEscape(uuid), wxJar)
	if err != nil {
		return nil, safeRequestError("获取微信二维码", err)
	}
	mime := sniffImageMime(image.Raw)
	if mime == "" {
		return nil, fmt.Errorf("%w: 二维码接口返回的不是图片", entity.ErrPlatformChanged)
	}

	expiresAt := c.now().Add(qrDefaultTTL)
	c.qrMu.Lock()
	c.pruneQRLocked(c.now())
	c.qrSessions[uuid] = &qrSession{
		uuid:      uuid,
		state:     state,
		expiresAt: expiresAt,
		jar:       jar,
		wxJar:     wxJar,
	}
	c.qrMu.Unlock()

	c.logger.Debug("fiforal login", "event", "qr_ready",
		"duration_ms", time.Since(started).Milliseconds())

	return &entity.QRChallenge{
		ID:        uuid,
		Content:   "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(image.Raw),
		ExpiresAt: expiresAt,
	}, nil
}

// fetchWeixinURL 调 getWebLoginUrl 取微信扫码地址。status 是数字 1
func (c *Client) fetchWeixinURL(ctx context.Context, jar *cookieJar) (string, error) {
	apiURL := c.passBaseURL + "/iplat-pass/web/getWebLoginUrl?" + url.Values{
		"type":    {"2"},
		"service": {c.oralURL},
	}.Encode()
	resp, err := c.get(ctx, apiURL, jar)
	if err != nil {
		return "", safeRequestError("获取 FiF 登录地址", err)
	}
	var env loginURLEnvelope
	if err := json.Unmarshal(resp.Raw, &env); err != nil {
		return "", fmt.Errorf("%w: getWebLoginUrl 响应不是 JSON", entity.ErrPlatformChanged)
	}
	if env.Status != 1 {
		return "", fmt.Errorf("%w: %s (status=%d)", entity.ErrBusiness, env.Msg, env.Status)
	}
	weixinURL := strings.TrimSpace(env.Data.WeixinURL)
	if weixinURL == "" {
		return "", fmt.Errorf("%w: getWebLoginUrl 没有返回 weixinUrl", entity.ErrPlatformChanged)
	}
	return weixinURL, nil
}

// followWeixin 打开微信扫码页并手工跟随 302，拿到含 uuid 的 HTML
func (c *Client) followWeixin(ctx context.Context, rawURL string, jar *cookieJar) (*sender.Response, error) {
	current := rawURL
	for hop := 0; hop < maxLoginHops; hop++ {
		resp, err := c.doRaw(ctx, http.MethodGet, current, jar, 0)
		if err == nil {
			if resp == nil {
				return nil, fmt.Errorf("%w: 微信扫码页没有响应", entity.ErrPlatformChanged)
			}
			return resp, nil
		}
		var status *sender.StatusError
		if !errors.As(err, &status) || resp == nil {
			return nil, safeRequestError("打开微信扫码页", err)
		}
		if status.Code < 300 || status.Code >= 400 {
			return nil, safeRequestError("打开微信扫码页", err)
		}
		next := strings.TrimSpace(resp.Location)
		if next == "" {
			return nil, fmt.Errorf("%w: 微信扫码页跳转缺少 Location", entity.ErrPlatformChanged)
		}
		base, parseErr := url.Parse(current)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: 微信扫码页地址无效", entity.ErrPlatformChanged)
		}
		ref, parseErr := url.Parse(next)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: 微信扫码页跳转地址无效", entity.ErrPlatformChanged)
		}
		resolved := base.ResolveReference(ref)
		if !isWeixinHost(resolved.Hostname()) {
			return nil, fmt.Errorf("%w: 微信扫码页被跳转到 %s", entity.ErrPlatformChanged, resolved.Hostname())
		}
		current = resolved.String()
	}
	return nil, fmt.Errorf("%w: 微信扫码页跳转超过 %d 跳", entity.ErrPlatformChanged, maxLoginHops)
}

// PollQR 长轮询一次扫码状态。
//
// 超时与 5xx 都当成「继续等」：长轮询没有消息时本来就以超时结束
func (c *Client) PollQR(ctx context.Context, challenge *entity.QRChallenge) (entity.QRState, *entity.Session, error) {
	if challenge == nil || challenge.ID == "" {
		return entity.QRFailed, nil, fmt.Errorf("%w: 缺少二维码 id", entity.ErrAuthFailed)
	}
	qs, last := c.lookupQR(challenge.ID)
	if qs == nil {
		return entity.QRExpired, nil, nil
	}

	query := url.Values{"uuid": {qs.uuid}}
	if last != "" {
		query.Set("last", last)
	}
	pollURL := c.longPollBaseURL + "/connect/l/qrconnect?" + query.Encode()

	resp, err := c.doRaw(ctx, http.MethodGet, pollURL, qs.wxJar, pollTimeout)
	if err != nil {
		if isTransientPollError(err) {
			return entity.QRWaiting, nil, nil
		}
		var canceled *sender.CanceledError
		if errors.As(err, &canceled) {
			return entity.QRFailed, nil, fmt.Errorf("%w: %v", entity.ErrCanceled, err)
		}
		return entity.QRFailed, nil, classifySendError(err)
	}

	errcode, wxCode, ok := parseQRResponse(resp.Raw)
	if !ok {
		c.dropQR(qs.uuid)
		return entity.QRFailed, nil, fmt.Errorf("%w: 扫码状态响应无法解析", entity.ErrPlatformChanged)
	}
	c.logger.Debug("fiforal qr", "event", "poll", "errcode", errcode)

	switch errcode {
	case wxErrcodeWaiting:
		c.setLast(qs.uuid, "")
		return entity.QRWaiting, nil, nil
	case wxErrcodeScanned:
		c.markScanned(qs.uuid)
		return entity.QRScanned, nil, nil
	case wxErrcodeExpired, wxErrcodeCanceled, wxErrcodeServer:
		// 二维码失效、用户在手机上取消、微信侧异常，都让前端自动换一张新码
		c.dropQR(qs.uuid)
		return entity.QRExpired, nil, nil
	case wxErrcodeConfirmed:
		if strings.TrimSpace(wxCode) == "" {
			c.dropQR(qs.uuid)
			return entity.QRFailed, nil, fmt.Errorf("%w: 已确认但未拿到授权码", entity.ErrAuthFailed)
		}
		if scanned := c.scannedTime(qs.uuid); !scanned.IsZero() {
			c.logger.Debug("fiforal login", "event", "confirmed",
				"since_scan_ms", c.now().Sub(scanned).Milliseconds())
		}
		return c.finish(ctx, qs, wxCode)
	default:
		c.dropQR(qs.uuid)
		return entity.QRFailed, nil, fmt.Errorf("%w: 未知扫码状态 wx_errcode=%d", entity.ErrPlatformChanged, errcode)
	}
}

// finish 用授权码走完「回调页 → 两步 CAS → 落地 → 验证」并保存会话
func (c *Client) finish(ctx context.Context, qs *qrSession, wxCode string) (entity.QRState, *entity.Session, error) {
	c.dropQR(qs.uuid)
	started := time.Now()

	page, err := c.fetchCallbackPage(ctx, qs.jar, wxCode, qs.state)
	if err != nil {
		return entity.QRFailed, nil, err
	}
	callbackDone := time.Now()
	c.logger.Debug("fiforal login", "event", "callback_page",
		"duration_ms", callbackDone.Sub(started).Milliseconds())

	prodToken, err := c.casLogin(ctx, qs.jar, page)
	if err != nil {
		return entity.QRFailed, nil, err
	}
	casDone := time.Now()
	c.logger.Debug("fiforal login", "event", "cas",
		"duration_ms", casDone.Sub(callbackDone).Milliseconds())

	// logincallback 的 token 就是新的 prod-token
	qs.jar.setCookie(entity.FiforalCookie{
		Name:     "prod-token",
		Value:    prodToken,
		Domain:   "fifedu.com",
		Path:     "/",
		HostOnly: false,
		Secure:   true,
	})

	startURL := c.ssoService + "?nextPage=" + url.QueryEscape(page.NextPage)
	token, err := c.landingToToken(ctx, qs.jar, startURL)
	if err != nil {
		return entity.QRFailed, nil, err
	}
	landedDone := time.Now()

	if err := c.verifyToken(ctx, qs.jar, token); err != nil {
		return entity.QRFailed, nil, err
	}
	verifiedDone := time.Now()
	c.cacheToken(token)

	session, err := c.newSession(qs.jar, token.UserID)
	if err != nil {
		return entity.QRFailed, nil, err
	}
	c.logger.Debug("fiforal login", "event", "completed",
		"total_ms", verifiedDone.Sub(started).Milliseconds(),
		"callback_ms", callbackDone.Sub(started).Milliseconds(),
		"cas_ms", casDone.Sub(callbackDone).Milliseconds(),
		"landing_ms", landedDone.Sub(casDone).Milliseconds(),
		"verify_ms", verifiedDone.Sub(landedDone).Milliseconds())
	return entity.QRSucceeded, session, nil
}

// verifyToken 落地后立刻调一次任务列表确认登录态，同时预热 token
func (c *Client) verifyToken(ctx context.Context, jar *cookieJar, token *h5Token) error {
	if _, _, err := c.callTaskList(ctx, jar, token, statusInProgress, 1); err != nil {
		if errors.Is(err, entity.ErrAuthExpired) {
			return fmt.Errorf("%w: 落地后仍未登录", entity.ErrAuthFailed)
		}
		return err
	}
	return nil
}

func (c *Client) lookupQR(uuid string) (*qrSession, string) {
	c.qrMu.Lock()
	defer c.qrMu.Unlock()
	qs := c.qrSessions[uuid]
	if qs == nil {
		return nil, ""
	}
	if !c.now().Before(qs.expiresAt) {
		delete(c.qrSessions, uuid)
		return nil, ""
	}
	return qs, qs.last
}

func (c *Client) setLast(uuid, last string) {
	c.qrMu.Lock()
	defer c.qrMu.Unlock()
	if qs := c.qrSessions[uuid]; qs != nil {
		qs.last = last
	}
}

// markScanned 记下 404 并保留首次扫码时刻，用来量到 405 的间隔
func (c *Client) markScanned(uuid string) {
	c.qrMu.Lock()
	defer c.qrMu.Unlock()
	qs := c.qrSessions[uuid]
	if qs == nil {
		return
	}
	qs.last = strconv.Itoa(wxErrcodeScanned)
	if qs.scannedAt.IsZero() {
		qs.scannedAt = c.now()
	}
}

func (c *Client) scannedTime(uuid string) time.Time {
	c.qrMu.Lock()
	defer c.qrMu.Unlock()
	if qs := c.qrSessions[uuid]; qs != nil {
		return qs.scannedAt
	}
	return time.Time{}
}

func (c *Client) dropQR(uuid string) {
	c.qrMu.Lock()
	delete(c.qrSessions, uuid)
	c.qrMu.Unlock()
}

// pruneQRLocked 清掉已经过期的码，避免反复换码时 map 只增不减
func (c *Client) pruneQRLocked(now time.Time) {
	for uuid, qs := range c.qrSessions {
		if !now.Before(qs.expiresAt) {
			delete(c.qrSessions, uuid)
		}
	}
}

// isTransientPollError 把超时与 HTTP 5xx 当成「继续等」，其余错误向上抛
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

var (
	uuidFromQRCodeRe = regexp.MustCompile(`connect/qrcode/([A-Za-z0-9_-]+)`)
	uuidFromQueryRe  = regexp.MustCompile(`[?&]uuid=([A-Za-z0-9_-]+)`)
	errcodeRe        = regexp.MustCompile(`wx_errcode\s*=\s*(\d+)`)
	wxcodeRe         = regexp.MustCompile(`wx_code\s*=\s*'([^']*)'`)
)

// queryValue 取 URL 查询串里的单个值
func queryValue(rawURL, key string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get(key)
}

// extractUUID 从扫码页 HTML 里取 uuid，两种出现形式都吃
func extractUUID(html string) string {
	if m := uuidFromQRCodeRe.FindStringSubmatch(html); len(m) == 2 {
		return m[1]
	}
	if m := uuidFromQueryRe.FindStringSubmatch(html); len(m) == 2 {
		return m[1]
	}
	return ""
}

// parseQRResponse 解析长轮询返回的 JS 片段：
// window.wx_errcode=408;window.wx_code=...
func parseQRResponse(raw []byte) (int, string, bool) {
	m := errcodeRe.FindSubmatch(raw)
	if len(m) != 2 {
		return 0, "", false
	}
	errcode, err := strconv.Atoi(string(m[1]))
	if err != nil {
		return 0, "", false
	}
	code := ""
	if m := wxcodeRe.FindSubmatch(raw); len(m) == 2 {
		code = string(m[1])
	}
	return errcode, code, true
}
