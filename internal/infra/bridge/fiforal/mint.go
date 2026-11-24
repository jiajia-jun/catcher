package fiforal

import (
	"bytes"
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

// callbackPage 是回调页里的一次性凭据与落地入口
type callbackPage struct {
	Username   string
	Password   string
	NextPage   string // 已做 URL 解码
	SSOService string
	CASServer  string
}

var (
	callbackUsernameRe = regexp.MustCompile(`var\s+username\s*=\s*["']([^"']*)["']`)
	callbackPasswordRe = regexp.MustCompile(`var\s+password\s*=\s*["']([^"']*)["']`)
	callbackNextPageRe = regexp.MustCompile(`var\s+nextPage\s*=\s*["']([^"']*)["']`)
	eduSSORe           = regexp.MustCompile(`new\s+EduSSO\(\s*["']([^"']+)["']\s*,\s*["']([^"']+)["']\s*\)`)
	callbackFailedRe   = regexp.MustCompile(`"status"\s*:\s*0`)
	scriptRedirectRe   = regexp.MustCompile(`(?:top\.|window\.)?location\.(?:href\s*=\s*|replace\(\s*)['"]([^'"]+)['"]`)
)

// fetchCallbackPage 用微信授权码换回调页里的账号密码
// 回调页返回 200 HTML，正文决定成败
func (c *Client) fetchCallbackPage(ctx context.Context, jar *cookieJar, code, state string) (*callbackPage, error) {
	callbackURL := c.passBaseURL + "/iplat-pass/weixin/callback?" + url.Values{
		"service": {c.oralURL},
		"code":    {code},
		"state":   {state},
	}.Encode()
	resp, err := c.get(ctx, callbackURL, jar)
	if err != nil {
		return nil, safeRequestError("读取 FiF 回调页", err)
	}
	page, parseErr := parseCallbackPage(resp.Raw)
	if parseErr == nil {
		return page, nil
	}
	if callbackFailedRe.Match(resp.Raw) {
		return nil, fmt.Errorf("%w: 微信授权码已失效或已被使用，请重新扫码", entity.ErrAuthFailed)
	}
	return nil, parseErr
}

func parseCallbackPage(raw []byte) (*callbackPage, error) {
	page := &callbackPage{
		SSOService: DefaultSSOService,
		CASServer:  DefaultCASServer,
	}
	if m := callbackUsernameRe.FindSubmatch(raw); len(m) == 2 {
		page.Username = string(m[1])
	}
	if m := callbackPasswordRe.FindSubmatch(raw); len(m) == 2 {
		page.Password = string(m[1])
	}
	if m := callbackNextPageRe.FindSubmatch(raw); len(m) == 2 {
		next, err := url.PathUnescape(string(m[1]))
		if err != nil {
			return nil, fmt.Errorf("%w: 回调页 nextPage 无法解码", entity.ErrPlatformChanged)
		}
		page.NextPage = next
	}
	if m := eduSSORe.FindSubmatch(raw); len(m) == 3 {
		page.SSOService = strings.TrimSpace(string(m[1]))
		page.CASServer = strings.TrimRight(strings.TrimSpace(string(m[2])), "/")
	}
	if page.Username == "" || page.Password == "" || page.NextPage == "" {
		return nil, fmt.Errorf("%w: 回调页缺少账号、密码或落地地址", entity.ErrPlatformChanged)
	}
	return page, nil
}

// casLogin 复现页面里的两步 CAS，返回的 token 就是新的 prod-token
// 明文密码只在这里短暂使用，日志与错误串中只保留打码内容
func (c *Client) casLogin(ctx context.Context, jar *cookieJar, page *callbackPage) (string, error) {
	ms := strconv.FormatInt(c.now().UnixMilli(), 10)

	ltURL := page.CASServer + "/login?" + url.Values{
		"service":  {page.SSOService},
		"get-lt":   {"true"},
		"n":        {ms},
		"callback": {"jsonpcallback"},
		"_":        {ms},
	}.Encode()
	resp, err := c.get(ctx, ltURL, jar)
	if err != nil {
		return "", safeRequestError("获取 FiF 登录票据", err)
	}
	var ltResp struct {
		LT        string `json:"lt"`
		Execution string `json:"execution"`
	}
	if err := parseJSONP(resp.Raw, &ltResp); err != nil {
		return "", fmt.Errorf("%w: CAS 取票响应无法解析", entity.ErrPlatformChanged)
	}
	if strings.TrimSpace(ltResp.LT) == "" {
		return "", fmt.Errorf("%w: CAS 没有返回 lt", entity.ErrPlatformChanged)
	}

	password := base64.StdEncoding.EncodeToString([]byte(page.Password))
	loginURL := page.CASServer + "/login?" + url.Values{
		"service":    {page.SSOService},
		"callback":   {"logincallback"},
		"username":   {page.Username},
		"password":   {password},
		"isajax":     {"true"},
		"isframe":    {"true"},
		"_eventId":   {"submit"},
		"serviceURL": {""},
		"lt":         {ltResp.LT},
		"execution":  {ltResp.Execution},
		"type":       {"pwd"},
		"_":          {ms},
	}.Encode()
	resp, err = c.get(ctx, loginURL, jar)
	if err != nil {
		return "", safeRequestError("FiF CAS 登录", err)
	}
	var loginResp struct {
		Login  string `json:"login"`
		Token  string `json:"token"`
		Msg    string `json:"msg"`
		Ticket string `json:"ticket"`
	}
	if err := parseJSONP(resp.Raw, &loginResp); err != nil {
		return "", fmt.Errorf("%w: CAS 登录响应无法解析", entity.ErrPlatformChanged)
	}
	token := strings.TrimSpace(loginResp.Token)
	if loginResp.Login != "success" || token == "" {
		return "", fmt.Errorf("%w: CAS 登录未成功", entity.ErrAuthFailed)
	}
	return token, nil
}

// parseJSONP 按「第一个 { 到最后一个 }」解析 JSONP 体，回调函数名不参与解析
func parseJSONP(raw []byte, out any) error {
	start := bytes.IndexByte(raw, '{')
	end := bytes.LastIndexByte(raw, '}')
	if start < 0 || end < start {
		return fmt.Errorf("响应里没有 JSON 对象")
	}
	if err := json.Unmarshal(raw[start:end+1], out); err != nil {
		return fmt.Errorf("JSON 无法解析: %w", err)
	}
	return nil
}

// landingToToken 逐跳跟随落地链，从 302 的 Location fragment 里取 H5 token
// sender 将重定向作为状态码返回，这里手工走完跳转链，包括 /cas-server/ 那一跳
func (c *Client) landingToToken(ctx context.Context, jar *cookieJar, startURL string) (*h5Token, error) {
	started := time.Now()
	hops := 0
	// 登录与续期共用这条链，跳数是解释登录耗时的主要线索
	defer func() {
		c.logger.Debug("fiforal login", "event", "landing",
			"hops", hops, "duration_ms", time.Since(started).Milliseconds())
	}()

	current := startURL
	for hop := 0; hop < maxLandingHops; hop++ {
		hops = hop + 1
		resp, err := c.doRaw(ctx, http.MethodGet, current, jar, 0)
		if err != nil {
			var status *sender.StatusError
			if !errors.As(err, &status) || resp == nil {
				// 落地链的 URL 里可能有 ticket，错误串只保留脱敏内容
				return nil, safeRequestError("FiF 落地", err)
			}
			if status.Code < 300 || status.Code >= 400 {
				return nil, classifySendError(err)
			}
			next := strings.TrimSpace(resp.Location)
			if next == "" {
				return nil, fmt.Errorf("%w: 落地跳转缺少 Location", entity.ErrPlatformChanged)
			}
			resolved, resolveErr := c.resolveLandingURL(current, next)
			if resolveErr != nil {
				return nil, resolveErr
			}
			token, tokenErr := tokenFromURL(resolved)
			if tokenErr != nil {
				return nil, tokenErr
			}
			if token != nil {
				return token, nil
			}
			current = resolved
			continue
		}

		// 200：只有 pass 的过渡页才跟脚本跳转，业务页面的 location.href 会自循环
		if isTransitionPage(current) {
			if next, ok := extractScriptRedirect(resp.Raw); ok {
				resolved, resolveErr := c.resolveLandingURL(current, next)
				if resolveErr != nil {
					return nil, resolveErr
				}
				token, tokenErr := tokenFromURL(resolved)
				if tokenErr != nil {
					return nil, tokenErr
				}
				if token != nil {
					return token, nil
				}
				current = resolved
				continue
			}
		} else if next, ok := extractScriptRedirect(resp.Raw); ok {
			// 业务页面的脚本跳转只用于提取 token，页面本身保持不动
			if resolved, resolveErr := c.resolveLandingURL(current, next); resolveErr == nil {
				token, tokenErr := tokenFromURL(resolved)
				if tokenErr != nil {
					return nil, tokenErr
				}
				if token != nil {
					return token, nil
				}
			}
		}
		return nil, landingStopError(current)
	}
	return nil, fmt.Errorf("%w: 落地跳转超过 %d 跳", entity.ErrPlatformChanged, maxLandingHops)
}

// resolveLandingURL 把 Location 或脚本跳转补全为绝对地址，并确认仍在 fifedu 域内
func (c *Client) resolveLandingURL(current, next string) (string, error) {
	base, err := url.Parse(current)
	if err != nil {
		return "", fmt.Errorf("%w: 落地跳转地址无效", entity.ErrPlatformChanged)
	}
	ref, err := url.Parse(strings.TrimSpace(next))
	if err != nil {
		return "", fmt.Errorf("%w: 落地跳转地址无效", entity.ErrPlatformChanged)
	}
	resolved := base.ResolveReference(ref)
	if resolved.Scheme != "https" && resolved.Scheme != "http" {
		return "", fmt.Errorf("%w: 落地跳转协议异常: %s", entity.ErrPlatformChanged, resolved.Scheme)
	}
	if !isFifeduHost(resolved.Hostname()) {
		return "", fmt.Errorf("%w: 落地被跳转到 %s", entity.ErrPlatformChanged, resolved.Hostname())
	}
	return resolved.String(), nil
}

func isTransitionPage(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "pass.fifedu.com" && strings.HasPrefix(u.Path, "/iplat-pass/")
}

// landingStopError 判断 200 停在哪种页面，停在登录页时按会话失效处理
func landingStopError(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: 落地链停在无效地址", entity.ErrPlatformChanged)
	}
	host := strings.ToLower(u.Hostname())
	path := u.Path
	if (host == "cycore.fifedu.com" && strings.HasPrefix(path, "/cas-server/login")) ||
		strings.Contains(path, "/fifLogin/") {
		return fmt.Errorf("%w: 登录链落回登录页，cookie 已失效", entity.ErrAuthExpired)
	}
	return fmt.Errorf("%w: 落地链停在 %s%s", entity.ErrPlatformChanged, host, path)
}

// extractScriptRedirect 从 200 页面里取 JS 跳转目标
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
