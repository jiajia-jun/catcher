package yuketang

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"catcher/internal/domain/entity"
	"catcher/internal/infra/sender"
)

const (
	preInfoPath  = "/api/v3/user/login/app-web-pre-info"
	pollPath     = "/api/v3/user/login/app-web-login"
	webLoginPath = "/pc/web_login"

	// contentFormURLEncoded 是 ③ 必须使用的 Content-Type：body 是 JSON，声明却是 form
	contentFormURLEncoded = "application/x-www-form-urlencoded; charset=UTF-8"
	// tokenExpiredCode 表示二维码 token 作废
	tokenExpiredCode = 50400
)

// BeginQR 取二维码。qrContent 是待渲染的 URL，token 5 分钟有效
func (c *Client) BeginQR(ctx context.Context) (*entity.QRChallenge, error) {
	resp, err := c.get(ctx, preInfoPath, "")
	if err != nil {
		return nil, err
	}

	var out preInfoResp
	if err := json.Unmarshal(resp.Raw, &out); err != nil {
		return nil, fmt.Errorf("%w: 二维码响应不是 JSON: %v", entity.ErrPlatformChanged, err)
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("%w: 取二维码返回 code=%d msg=%s", entity.ErrPlatformChanged, out.Code, out.Msg)
	}
	if out.Data.Token == "" || out.Data.QRContent == "" {
		return nil, fmt.Errorf("%w: 二维码响应缺少 token 或 qrContent", entity.ErrPlatformChanged)
	}

	return &entity.QRChallenge{
		// token 同时是轮询凭据，放在 ID 里交给 PollQR
		ID:        out.Data.Token,
		Content:   out.Data.QRContent,
		ExpiresAt: c.now().Add(qrLifetime),
	}, nil
}

// PollQR 轮询扫码结果；拿到 UserID/Auth 后立即走 ③ 换取 sessionid
//
// 504 与超时都返回 waiting：未扫码时服务端是挂住而不是拒绝
func (c *Client) PollQR(ctx context.Context, challenge *entity.QRChallenge) (entity.QRState, *entity.Session, error) {
	if challenge == nil || challenge.ID == "" {
		return entity.QRFailed, nil, fmt.Errorf("%w: 缺少二维码 token", entity.ErrAuthFailed)
	}

	body, err := json.Marshal(map[string]string{"token": challenge.ID})
	if err != nil {
		return entity.QRFailed, nil, err
	}

	resp, err := c.http.Do(ctx, &sender.Request{
		Method: "POST",
		URL:    c.baseURL + pollPath,
		Header: map[string]string{
			"Accept":       "application/json",
			"Content-Type": "application/json",
		},
		Body:    body,
		Timeout: c.pollTimeout,
	})
	if err != nil {
		return c.classifyPollError(resp, err)
	}

	var out pollResp
	if err := json.Unmarshal(resp.Raw, &out); err != nil {
		return entity.QRFailed, nil, fmt.Errorf("%w: 轮询响应不是 JSON: %v", entity.ErrPlatformChanged, err)
	}
	if out.Code != 0 {
		return entity.QRFailed, nil, fmt.Errorf("%w: 轮询返回 code=%d msg=%s", entity.ErrAuthFailed, out.Code, out.Msg)
	}
	if out.Data.UserID.String() == "" || out.Data.Auth == "" {
		return entity.QRFailed, nil, fmt.Errorf("%w: 扫码响应缺少 UserID 或 Auth", entity.ErrPlatformChanged)
	}

	session, err := c.exchange(ctx, out.Data.UserID, out.Data.Auth)
	if err != nil {
		return entity.QRFailed, nil, err
	}
	return entity.QRSucceeded, session, nil
}

// classifyPollError 区分「继续等」和「真的失败」
// 400 只有在业务 code == 50400 时才算二维码作废
func (c *Client) classifyPollError(resp *sender.Response, err error) (entity.QRState, *entity.Session, error) {
	var status *sender.StatusError
	if errors.As(err, &status) {
		switch status.Code {
		case 504:
			return entity.QRWaiting, nil, nil
		case 400:
			var body errorResp
			if resp != nil && json.Unmarshal(resp.Raw, &body) == nil && body.Code == tokenExpiredCode {
				return entity.QRExpired, nil, nil
			}
			return entity.QRFailed, nil, fmt.Errorf("%w: 轮询 HTTP 400 且业务 code 不是 %d", entity.ErrAuthFailed, tokenExpiredCode)
		default:
			return entity.QRFailed, nil, fmt.Errorf("%w: 轮询 HTTP %d", entity.ErrAuthFailed, status.Code)
		}
	}
	var timeout *sender.TimeoutError
	if errors.As(err, &timeout) {
		return entity.QRWaiting, nil, nil
	}
	var canceled *sender.CanceledError
	if errors.As(err, &canceled) {
		return entity.QRFailed, nil, fmt.Errorf("%w", entity.ErrCanceled)
	}
	return entity.QRFailed, nil, classifySendError(err)
}

// Renew 用已保存的 Auth 重放 ③，无需重新扫码
func (c *Client) Renew(ctx context.Context, session entity.Session) (*entity.Session, error) {
	secret, err := decodeSecret(session.Secret)
	if err != nil {
		return nil, err
	}
	if secret.Auth == "" || secret.UserID == "" {
		return nil, fmt.Errorf("%w: 缺少 Auth，无法免扫码续期", entity.ErrAuthRequired)
	}
	return c.exchange(ctx, json.Number(secret.UserID), secret.Auth)
}

// exchange 执行 ③：body 是 JSON，Content-Type 是 form-urlencoded
// 成败只看有没有下发 sessionid，不硬解成功正文
func (c *Client) exchange(ctx context.Context, userID json.Number, auth string) (*entity.Session, error) {
	body, err := webLoginBody(userID, auth)
	if err != nil {
		return nil, err
	}

	resp, err := c.postJSON(ctx, webLoginPath, contentFormURLEncoded, body, 0)
	if err != nil {
		var status *sender.StatusError
		if errors.As(err, &status) {
			return nil, fmt.Errorf("%w: 换取会话 HTTP %d", entity.ErrAuthFailed, status.Code)
		}
		return nil, err
	}

	sid := sessionCookie(resp.SetCookies)
	if sid == "" {
		// 失败响应是登录页 HTML，不解析、不把它当异常
		return nil, fmt.Errorf("%w: 换取会话未下发 sessionid", entity.ErrAuthFailed)
	}

	secret, err := json.Marshal(entity.YuketangSecret{
		SessionID: sid,
		Auth:      auth,
		UserID:    userID.String(),
	})
	if err != nil {
		return nil, err
	}
	return &entity.Session{
		Platform:  entity.PlatformYuketang,
		Secret:    secret,
		UpdatedAt: c.now().UTC(),
	}, nil
}

// webLoginBody 按原始字面量构造 UserID：它是数字，不能变成字符串
func webLoginBody(userID json.Number, auth string) ([]byte, error) {
	raw := userID.String()
	if raw == "" {
		return nil, fmt.Errorf("%w: 缺少 UserID", entity.ErrAuthFailed)
	}
	uid := json.RawMessage(raw)
	if _, err := strconv.ParseInt(raw, 10, 64); err != nil {
		quoted, _ := json.Marshal(raw)
		uid = quoted
	}
	return json.Marshal(struct {
		UserID json.RawMessage `json:"UserID"`
		Auth   string          `json:"Auth"`
	}{UserID: uid, Auth: auth})
}

func decodeSecret(raw json.RawMessage) (*entity.YuketangSecret, error) {
	var secret entity.YuketangSecret
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: 会话为空", entity.ErrAuthRequired)
	}
	if err := json.Unmarshal(raw, &secret); err != nil {
		return nil, fmt.Errorf("%w: 解析雨课堂会话失败: %v", entity.ErrAuthRequired, err)
	}
	if secret.SessionID == "" {
		return nil, fmt.Errorf("%w: 会话缺少 sessionid", entity.ErrAuthRequired)
	}
	return &secret, nil
}
