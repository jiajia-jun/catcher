package pigai

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
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
	expiresAt time.Time
	// jar 只保存 www.pigai.org 的 cookie；微信域名的响应写进临时袋，
	// 避免把 open.weixin.qq.com 的 cookie 一起塞进持久化会话
	jar *cookieJar
	// wxJar 是微信域名专用的临时 cookie 袋，不落库
	wxJar *cookieJar
	// last 是上一次轮询的 errcode，404 / 403 之后的请求要带上它
	last string
}

// BeginQR 取一张微信扫码登录的二维码。
//
// 二维码是服务端按 uuid 生成的 JPEG，不是前端算出来的：
// 先打开扫码页拿 uuid，再用同一个 uuid 取图。图片本身转成 data URL 交给前端，
// 前端不必再请求微信域名（那会被域名策略拦下）
func (c *Client) BeginQR(ctx context.Context) (*entity.QRChallenge, error) {
	jar := newCookieJar(nil)

	// 落地页只用来给 cookie 袋播种，打不开不影响后面取码
	if _, err := c.get(ctx, c.baseURL+"/d3/weixin/wxlogin.html", jar); err != nil {
		c.logger.Debug("pigai qr", "event", "open_login_page_failed", "error", err.Error())
	}

	wxJar := newCookieJar(nil)
	connectURL := c.weixinBaseURL + "/connect/qrconnect?" + url.Values{
		"appid":         {weixinAppID},
		"scope":         {"snsapi_login"},
		"redirect_uri":  {weixinRedirect},
		"state":         {weixinState},
		"login_type":    {"jssdk"},
		"self_redirect": {"default"},
		"styletype":     {""},
		"sizetype":      {""},
		"bgcolor":       {""},
		"rst":           {""},
		"ts":            {strconv.FormatInt(c.now().UnixMilli(), 10)},
	}.Encode()
	resp, err := c.getClassified(ctx, connectURL, wxJar)
	if err != nil {
		return nil, fmt.Errorf("打开微信扫码页失败: %w", err)
	}

	uuid := extractUUID(string(resp.Raw))
	if uuid == "" {
		return nil, fmt.Errorf("%w: 扫码页里找不到 uuid", entity.ErrPlatformChanged)
	}

	image, err := c.getClassified(ctx, c.weixinBaseURL+"/connect/qrcode/"+url.PathEscape(uuid), wxJar)
	if err != nil {
		return nil, fmt.Errorf("获取微信二维码失败: %w", err)
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
		expiresAt: expiresAt,
		jar:       jar,
		wxJar:     wxJar,
	}
	c.qrMu.Unlock()

	return &entity.QRChallenge{
		ID:        uuid,
		Content:   "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(image.Raw),
		ExpiresAt: expiresAt,
	}, nil
}

// PollQR 长轮询一次扫码状态。微信侧会阻塞到状态变化或约 15 秒超时。
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

	resp, err := c.doRaw(ctx, pollURL, qs.wxJar, pollTimeout)
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
	c.logger.Debug("pigai qr", "event", "poll", "errcode", errcode)

	switch errcode {
	case wxErrcodeWaiting:
		c.setLast(qs.uuid, "")
		return entity.QRWaiting, nil, nil
	case wxErrcodeScanned:
		c.setLast(qs.uuid, strconv.Itoa(wxErrcodeScanned))
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
		return c.finish(ctx, qs, wxCode)
	default:
		c.dropQR(qs.uuid)
		return entity.QRFailed, nil, fmt.Errorf("%w: 未知扫码状态 wx_errcode=%d", entity.ErrPlatformChanged, errcode)
	}
}

// finish 用授权码走完批改网的换号链路并验证登录态
func (c *Client) finish(ctx context.Context, qs *qrSession, wxCode string) (entity.QRState, *entity.Session, error) {
	c.dropQR(qs.uuid)

	loginURL := c.baseURL + "/d3/weixin/login.php?" + url.Values{
		"code":  {wxCode},
		"state": {weixinState},
	}.Encode()
	if err := c.followLoginChain(ctx, loginURL, qs.jar); err != nil {
		return entity.QRFailed, nil, err
	}

	// _JUKU_USER 是登录成功的实体凭据，换号链路没有它等于没登录
	if qs.jar.get("_JUKU_USER") == "" {
		return entity.QRFailed, nil, fmt.Errorf("%w: 换号后没有拿到登录凭据", entity.ErrAuthFailed)
	}
	if err := c.verifyLogin(ctx, qs.jar); err != nil {
		return entity.QRFailed, nil, err
	}

	session, err := c.newSession(qs.jar)
	if err != nil {
		return entity.QRFailed, nil, err
	}
	return entity.QRSucceeded, session, nil
}

// followLoginChain 逐跳跟随换号重定向，每跳都累积 Set-Cookie。
// sender 不跟随重定向，这里必须手工走完；跨出 pigai.org 一律视为异常
//
// 首跳是特例：login.php 带 state 时返回 200 + 一段 JS 跳到去掉 state 的自身，
// 真正的 302 链路从第二跳才开始。只认 3xx 会在这里停下，拿不到 _JUKU_USER
func (c *Client) followLoginChain(ctx context.Context, rawURL string, jar *cookieJar) error {
	current := rawURL
	for hop := 0; hop <= maxLoginHops; hop++ {
		resp, err := c.doRaw(ctx, current, jar, 0)
		if err == nil {
			if resp == nil {
				return nil
			}
			next, ok := extractScriptRedirect(resp.Raw)
			if !ok {
				return nil
			}
			resolved, resolveErr := c.resolveLoginURL(current, next)
			if resolveErr != nil {
				return resolveErr
			}
			current = resolved
			continue
		}

		// 用原始错误判断 3xx：classifySendError 会把状态码折成领域错误
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
		resolved, resolveErr := c.resolveLoginURL(current, next)
		if resolveErr != nil {
			return resolveErr
		}
		current = resolved
	}
	return fmt.Errorf("%w: 换号跳转超过 %d 跳", entity.ErrPlatformChanged, maxLoginHops)
}

// resolveLoginURL 把 Location / JS 跳转按当前地址补全成绝对地址，并确认仍在批改网主机上
func (c *Client) resolveLoginURL(current, next string) (string, error) {
	base, err := url.Parse(current)
	if err != nil {
		return "", fmt.Errorf("%w: 登录跳转地址无效: %v", entity.ErrPlatformChanged, err)
	}
	ref, err := url.Parse(strings.TrimSpace(next))
	if err != nil {
		return "", fmt.Errorf("%w: 登录跳转地址无效: %v", entity.ErrPlatformChanged, err)
	}
	resolved := base.ResolveReference(ref)
	if resolved.Scheme != "https" && resolved.Scheme != "http" {
		return "", fmt.Errorf("%w: 登录跳转协议异常: %s", entity.ErrPlatformChanged, resolved.Scheme)
	}
	if !c.isLoginHost(resolved.Hostname()) {
		return "", fmt.Errorf("%w: 登录被跳转到 %s", entity.ErrPlatformChanged, resolved.Hostname())
	}
	return resolved.String(), nil
}

// verifyLogin 用作业页确认登录态。信封的 error 才是判据，HTTP 状态码没有意义
func (c *Client) verifyLogin(ctx context.Context, jar *cookieJar) error {
	env, err := c.fetchEnvelope(ctx, c.apiURL(url.Values{"c": {"write"}}), jar)
	if err != nil {
		if errors.Is(err, entity.ErrAuthExpired) {
			return fmt.Errorf("%w: 换号后仍未登录", entity.ErrAuthFailed)
		}
		return err
	}

	// _u 是当前用户块。缺省不判失败（白名单外字段随时可能消失），
	// 但出现了就必须带 user_id
	var data struct {
		User *struct {
			UserID string `json:"user_id"`
		} `json:"_u"`
	}
	if err := decodeEnvelopeData(env, &data); err != nil {
		return err
	}
	if data.User != nil && strings.TrimSpace(data.User.UserID) == "" {
		return fmt.Errorf("%w: 登录校验没有返回用户 id", entity.ErrAuthFailed)
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
	scriptRedirectRe = regexp.MustCompile(`(?:top\.|window\.)?location\.(?:href\s*=\s*|replace\(\s*)['"]([^'"]+)['"]`)
)

// extractScriptRedirect 从 200 页面里取 JS 跳转目标。
// login.php 首跳就是这么跳的，响应头里没有 Location
func extractScriptRedirect(raw []byte) (string, bool) {
	m := scriptRedirectRe.FindSubmatch(raw)
	if len(m) != 2 {
		return "", false
	}
	target := strings.TrimSpace(string(m[1]))
	if target == "" {
		return "", false
	}
	return target, true
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
// window.wx_errcode=408;window.wx_code='';
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

// sniffImageMime 按文件头判断图片类型，二维码接口的 Content-Type 不可信
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
