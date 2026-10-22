package chaoxing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"catcher/internal/domain/entity"
	"catcher/internal/infra/sender"
)

const loginPath = "/fanyalogin"

// LoginWithPassword 用手机号+密码登录，并累积整条 302 链的 Set-Cookie
//
// 实测登录通常单跳，但接口文档坚持多跳；两种形态都必须吃得下
func (c *Client) LoginWithPassword(ctx context.Context, creds entity.PasswordCredential) (*entity.Session, error) {
	creds.Phone = strings.TrimSpace(creds.Phone)
	if err := validateCredential(creds); err != nil {
		return nil, err
	}

	uname, err := encryptAES(creds.Phone)
	if err != nil {
		return nil, fmt.Errorf("加密手机号失败: %w", err)
	}
	password, err := encryptAES(creds.Password)
	if err != nil {
		return nil, fmt.Errorf("加密密码失败: %w", err)
	}

	form := url.Values{
		"fid":               {"-1"},
		"uname":             {uname},
		"password":          {password},
		"refer":             {"https://i.xuexitong.com"},
		"t":                 {"true"},
		"forbidotherlogin":  {"0"},
		"validate":          {""},
		"doubleFactorLogin": {"0"},
		"independentId":     {"0"},
		"independentNameId": {"0"},
	}

	jar := newCookieJar(nil)
	resp, err := c.followLogin(ctx, form.Encode(), jar)
	if err != nil {
		return nil, err
	}
	if err := checkLoginResult(resp.Raw); err != nil {
		return nil, err
	}
	if len(jar.order) == 0 {
		return nil, fmt.Errorf("%w: 登录成功但没有拿到 cookie", entity.ErrAuthFailed)
	}
	return c.newSession(jar)
}

// followLogin 逐跳跟随登录重定向，每跳都收 Set-Cookie。
//
// sender 恒不跟随重定向，这里必须手工走完；超过 10 跳或缺少 Location 直接报错
func (c *Client) followLogin(ctx context.Context, formBody string, jar *cookieJar) (*sender.Response, error) {
	rawURL := c.loginBaseURL + loginPath
	method := http.MethodPost
	body := []byte(formBody)

	for hop := 0; hop < maxLoginRedirects; hop++ {
		header := map[string]string{
			"Accept":     "application/json, text/plain, */*",
			"User-Agent": userAgent,
		}
		if method == http.MethodPost {
			header["Content-Type"] = "application/x-www-form-urlencoded"
			header["Origin"] = c.loginBaseURL
			header["Referer"] = c.loginBaseURL + "/"
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
		if err == nil {
			return resp, nil
		}

		var status *sender.StatusError
		if !errors.As(err, &status) {
			return nil, classifySendError(err)
		}
		if status.Code < 300 || status.Code >= 400 {
			// 非重定向的失败：登录接口对账号/密码错误返回 200 + status=false，
			// 其余状态码按领域错误处理
			return nil, classifySendError(err)
		}
		if hop == maxLoginRedirects-1 {
			return nil, fmt.Errorf("%w: 登录重定向超过 %d 跳", entity.ErrAuthFailed, maxLoginRedirects)
		}

		next := strings.TrimSpace(resp.Location)
		if next == "" {
			return nil, fmt.Errorf("%w: 登录重定向缺少 Location", entity.ErrAuthFailed)
		}
		rawURL = next
		// 302/303 按浏览器语义转成 GET；307/308 保留 POST
		if status.Code != http.StatusTemporaryRedirect && status.Code != http.StatusPermanentRedirect {
			method = http.MethodGet
			body = nil
		}
	}
	return nil, fmt.Errorf("%w: 登录重定向超过 %d 跳", entity.ErrAuthFailed, maxLoginRedirects)
}

// checkLoginResult 成功是 {"status": true}，失败读 msg2
func checkLoginResult(raw []byte) error {
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "<") {
		return fmt.Errorf("%w: 登录返回页面而不是 JSON", entity.ErrAuthFailed)
	}

	var out struct {
		Status bool   `json:"status"`
		Msg2   string `json:"msg2"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("%w: 登录响应不是 JSON: %v", entity.ErrAuthFailed, err)
	}
	if out.Status {
		return nil
	}
	message := strings.TrimSpace(out.Msg2)
	if message == "" {
		message = strings.TrimSpace(out.Msg)
	}
	if message == "" {
		message = "账号或密码错误"
	}
	return fmt.Errorf("%w: %s", entity.ErrAuthFailed, message)
}
