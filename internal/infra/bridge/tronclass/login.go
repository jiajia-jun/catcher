package tronclass

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"catcher/internal/domain/entity"
)

const todosPath = "/api/todos"

// ImportSession 验证浏览器里的 session 并打包成会话
//
// 验证接口是 /api/todos：200 + JSON 才算登录有效。
// 只有 200 响应里的轮换值可以覆盖输入；401/302 的匿名 session 一律丢弃
func (c *Client) ImportSession(ctx context.Context, raw string) (*entity.Session, error) {
	value := normalizeSessionValue(raw)
	if value == "" {
		return nil, fmt.Errorf("%w: 会话不能为空", entity.ErrAuthRequired)
	}
	if len(value) > maxSessionBytes {
		return nil, fmt.Errorf("%w: 会话长度超过 %d 字节", entity.ErrAuthRequired, maxSessionBytes)
	}

	resp, err := c.do(ctx, http.MethodGet, todosPath, nil, value)
	if err != nil {
		return nil, err
	}
	if !isJSONResponse(resp) {
		return nil, fmt.Errorf("%w: 验证会话时返回的不是 JSON", entity.ErrPlatformChanged)
	}

	latest := value
	if rotated := sessionFromSetCookies(resp.SetCookies); rotated != "" {
		latest = rotated
	}
	return c.newSession(latest, parseUID(latest))
}

// normalizeSessionValue 去掉首尾空白与可选的 session= 前缀
func normalizeSessionValue(raw string) string {
	value := strings.TrimSpace(raw)
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "session=") {
		value = strings.TrimSpace(value[len("session="):])
	}
	return value
}

// parseUID 从 session 第二段解出 uid；失败只影响显示名，不阻断导入
func parseUID(session string) string {
	parts := strings.Split(session, ".")
	if len(parts) < 2 {
		return ""
	}
	segment := strings.TrimSpace(parts[1])
	decoded, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(segment)
	}
	if err != nil {
		return ""
	}
	uid := strings.TrimSpace(string(decoded))
	if uid == "" || len(uid) > 64 {
		return ""
	}
	for _, r := range uid {
		if !unicode.IsPrint(r) {
			return ""
		}
	}
	return uid
}
