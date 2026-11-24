package logger

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// 精确匹配的敏感键（小写）
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

// 只在 URL 查询串里算凭据的键
// code 作为 header 或字段名可能误伤课程编号一类的普通值，
// 出现在查询串里的 code 通常是 OAuth 授权码
// username 只出现在登录跳转的查询串里，按凭据处理
var sensitiveQueryKeys = map[string]struct{}{
	"code":     {},
	"username": {},
}

// IsSensitiveKey 判断字段名是否属于凭据，大小写不敏感
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

// IsSensitiveQueryKey 判断查询串参数名是否属于凭据，比 IsSensitiveKey 多认 code
func IsSensitiveQueryKey(key string) bool {
	if IsSensitiveKey(key) {
		return true
	}
	k := strings.ToLower(strings.TrimSpace(key))
	_, ok := sensitiveQueryKeys[k]
	return ok
}

// Redact 在字段名敏感时返回 ***，否则原样返回
func Redact(key, value string) string {
	if IsSensitiveKey(key) {
		return "***"
	}
	return value
}

// RedactHeader 对单个 header 的值打码
// Location 的 fragment 里可能带 H5 token，保留路径、丢弃 fragment
func RedactHeader(name string, values []string) []string {
	if !IsSensitiveKey(name) {
		if !strings.EqualFold(strings.TrimSpace(name), "Location") {
			return values
		}
		out := make([]string, len(values))
		for i := range out {
			out[i] = stripFragment(values[i])
		}
		return out
	}
	out := make([]string, len(values))
	for i := range out {
		out[i] = "***"
	}
	return out
}

// RedactHeaders 复制一份 header 并对凭据打码，避免修改原响应
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

// urlInTextRe 匹配文本里出现的绝对 URL，遇到空白或引号即截止
// net/http 的错误串形如 `Get "https://...": EOF`
var urlInTextRe = regexp.MustCompile(`https?://[^\s"'<>]+`)

// RedactText 把一段文本里出现的 URL 逐个按查询串打码
// 错误串本身会回显完整 URL，其中的 OAuth code 需要一同打码
func RedactText(text string) string {
	if !strings.Contains(text, "://") {
		return text
	}
	return urlInTextRe.ReplaceAllStringFunc(text, RedactURL)
}

// RedactURL 对 URL 查询串中的敏感参数打码，并剥掉 fragment
// 路径与普通参数保持不变，方便排查，fragment 里可能带 H5 token
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	changed := false
	q := u.Query()
	for k := range q {
		if IsSensitiveQueryKey(k) {
			q.Set(k, "***")
			changed = true
		}
	}
	if changed {
		u.RawQuery = q.Encode()
	}
	if u.Fragment != "" {
		u.Fragment = ""
		changed = true
	}
	if !changed {
		return raw
	}
	return u.String()
}

// stripFragment 去掉 URL 的 fragment，解析失败时按第一个 # 截断
func stripFragment(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		if i := strings.IndexByte(raw, '#'); i >= 0 {
			return raw[:i]
		}
		return raw
	}
	if u.Fragment == "" {
		return raw
	}
	u.Fragment = ""
	return u.String()
}
