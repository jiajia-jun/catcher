package logger

import (
	"net/http"
	"net/url"
	"strings"
)

// 精确匹配的敏感键（小写）。
var sensitiveKeys = map[string]struct{}{
	"cookie":              {},
	"set-cookie":          {},
	"authorization":       {},
	"proxy-authorization": {},
	"password":            {},
	"passwd":              {},
	"pwd":                 {},
	"secret":              {},
	"token":               {},
	"access_token":        {},
	"refresh_token":       {},
	"session":             {},
	"sessionid":           {},
	"session_id":          {},
	"auth":                {},
	"qrcode":              {},
	"qr_content":          {},
}

var sensitiveSuffixes = []string{
	"_password", "_passwd", "_secret", "_token", "_session", "_cookie",
}

// IsSensitiveKey 判断字段名是否属于凭据。大小写不敏感。
func IsSensitiveKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if _, ok := sensitiveKeys[k]; ok {
		return true
	}
	for _, suffix := range sensitiveSuffixes {
		if strings.HasSuffix(k, suffix) {
			return true
		}
	}
	return false
}

// Redact 在字段名敏感时返回 ***，否则原样返回。
func Redact(key, value string) string {
	if IsSensitiveKey(key) {
		return "***"
	}
	return value
}

// RedactHeader 对单个 header 的值打码。
func RedactHeader(name string, values []string) []string {
	if !IsSensitiveKey(name) {
		return values
	}
	out := make([]string, len(values))
	for i := range out {
		out[i] = "***"
	}
	return out
}

// RedactHeaders 复制一份 header 并对凭据打码，避免修改原响应。
func RedactHeaders(h http.Header) map[string][]string {
	if h == nil {
		return nil
	}
	out := make(map[string][]string, len(h))
	for k, values := range h {
		out[k] = RedactHeader(k, values)
	}
	return out
}

// RedactURL 对 URL 查询串中的敏感参数打码。
// 路径与普通参数保持不变，方便排查。
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	changed := false
	for k := range q {
		if IsSensitiveKey(k) {
			q.Set(k, "***")
			changed = true
		}
	}
	if !changed {
		return raw
	}
	u.RawQuery = q.Encode()
	return u.String()
}
