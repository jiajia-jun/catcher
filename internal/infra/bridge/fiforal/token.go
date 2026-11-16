package fiforal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"catcher/internal/domain/entity"
)

// h5Token 是任务接口要用的 120 分钟 JWT。只放内存，不落库
type h5Token struct {
	Value     string
	UserID    string
	Source    string
	ExpiresAt time.Time
}

// ensureToken 返回可用的 H5 token；快过期或进程刚启动时重新换一次
func (c *Client) ensureToken(ctx context.Context, jar *cookieJar) (*h5Token, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if c.token != nil && c.now().Before(c.token.ExpiresAt.Add(-tokenRefreshMargin)) {
		return c.token, nil
	}
	token, err := c.landingToToken(ctx, jar, c.oralURL)
	if err != nil {
		return nil, err
	}
	c.token = token
	return token, nil
}

// remintToken 强制用已存 cookie 重走 oral 落地链，忽略内存缓存
func (c *Client) remintToken(ctx context.Context, jar *cookieJar) (*h5Token, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	c.token = nil
	token, err := c.landingToToken(ctx, jar, c.oralURL)
	if err != nil {
		return nil, err
	}
	c.token = token
	return token, nil
}

func (c *Client) cacheToken(token *h5Token) {
	if token == nil {
		return
	}
	c.tokenMu.Lock()
	c.token = token
	c.tokenMu.Unlock()
}

// tokenFromURL 从落地跳转的 fragment 里取 token 与 source。
// 没有 token 返回 (nil, nil)，格式不对返回错误
func tokenFromURL(raw string) (*h5Token, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, nil
	}
	fragment := u.Fragment
	if fragment == "" {
		return nil, nil
	}
	if i := strings.IndexByte(fragment, '?'); i >= 0 {
		fragment = fragment[i+1:]
	}
	values, err := url.ParseQuery(fragment)
	if err != nil {
		return nil, nil
	}
	value := strings.TrimSpace(values.Get("token"))
	if value == "" {
		return nil, nil
	}
	return parseH5Token(value, values.Get("source"))
}

// parseH5Token 解析 3 段式 JWT 的载荷。长度不固定，不能硬编码
func parseH5Token(value, source string) (*h5Token, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 || parts[1] == "" {
		return nil, fmt.Errorf("%w: 落地 token 不是 3 段式 JWT", entity.ErrPlatformChanged)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// 少数实现会带 padding
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, fmt.Errorf("%w: 落地 token 载荷无法解码", entity.ErrPlatformChanged)
		}
	}
	var claims struct {
		LoginName string `json:"login_name"`
		UserID    string `json:"user_id"`
		UserKey   string `json:"user_key"`
		Iat       int64  `json:"iat"`
		Exp       int64  `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("%w: 落地 token 载荷不是 JSON", entity.ErrPlatformChanged)
	}
	userID := strings.TrimSpace(claims.UserID)
	if userID == "" {
		return nil, fmt.Errorf("%w: 落地 token 缺少 user_id", entity.ErrPlatformChanged)
	}
	if claims.Exp <= 0 {
		return nil, fmt.Errorf("%w: 落地 token 缺少过期时间", entity.ErrPlatformChanged)
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = defaultSource
	}
	return &h5Token{
		Value:     value,
		UserID:    userID,
		Source:    source,
		ExpiresAt: time.Unix(claims.Exp, 0),
	}, nil
}
