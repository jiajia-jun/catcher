package fiforal

import (
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"catcher/internal/domain/entity"
)

// cookieJar 是一个最小的 domain/path cookie 袋
//
// FiF 的登录态跨 www / pass / cycore / oral / moral / static 多个主机
// prod-token 是 .fifedu.com 的域 cookie，CASTGC / JSESSIONID 属于 cycore.fifedu.com
// 平铺的 name/value 表会把 cookie 发给错误主机，这里按 RFC 6265 的 domain/path 规则存取
type cookieJar struct {
	cookies []storedCookie
	changed bool
	now     func() time.Time
}

type storedCookie struct {
	value entity.FiforalCookie
	// expires 为零值表示会话 cookie
	expires time.Time
}

func newCookieJar(cookies []entity.FiforalCookie, now func() time.Time) *cookieJar {
	j := &cookieJar{
		cookies: make([]storedCookie, 0, len(cookies)),
		now:     now,
	}
	if j.now == nil {
		j.now = time.Now
	}
	for _, cookie := range cookies {
		if cookie.Name == "" {
			continue
		}
		j.cookies = append(j.cookies, storedCookie{value: cookie})
	}
	return j
}

func (j *cookieJar) isEmpty() bool { return len(j.cookies) == 0 }

// add 解析响应的 Set-Cookie 行，requestURL 用来确定 host-only 与默认 path
func (j *cookieJar) add(requestURL string, lines []string) {
	if len(lines) == 0 {
		return
	}
	req, err := url.Parse(requestURL)
	if err != nil {
		return
	}
	host := strings.ToLower(req.Hostname())
	for _, line := range lines {
		c, parseErr := http.ParseSetCookie(line)
		if parseErr != nil {
			// 个别网关会下发不规范的属性，退回只取 name=value
			name, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if i := strings.IndexByte(value, ';'); i >= 0 {
				value = value[:i]
			}
			j.set(entity.FiforalCookie{
				Name:     name,
				Value:    strings.TrimSpace(value),
				Domain:   host,
				Path:     defaultPath(req.Path),
				HostOnly: true,
			})
			continue
		}
		if c.Name == "" {
			continue
		}

		domain := strings.ToLower(strings.TrimSpace(c.Domain))
		hostOnly := domain == ""
		if hostOnly {
			domain = host
		} else {
			domain = strings.TrimPrefix(domain, ".")
			if !domainMatch(host, domain) {
				// 其他域写入的 cookie 在此跳过
				continue
			}
		}
		if domain == "" {
			continue
		}

		path := c.Path
		if path == "" || !strings.HasPrefix(path, "/") {
			path = defaultPath(req.Path)
		}

		if c.MaxAge < 0 {
			j.delete(c.Name, domain, path)
			continue
		}
		var expires time.Time
		switch {
		case c.MaxAge > 0:
			expires = j.now().Add(time.Duration(c.MaxAge) * time.Second)
		case !c.Expires.IsZero():
			if !c.Expires.After(j.now()) {
				j.delete(c.Name, domain, path)
				continue
			}
			expires = c.Expires
		}

		j.set(entity.FiforalCookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   domain,
			Path:     path,
			HostOnly: hostOnly,
			Secure:   c.Secure,
		})
		// set 只写入值，过期时间单独回填，无过期时间时清空旧值
		for i := range j.cookies {
			got := &j.cookies[i]
			if got.value.Name == c.Name && got.value.Domain == domain && got.value.Path == path {
				got.expires = expires
				break
			}
		}
	}
}

// setCookie 用于 CAS 返回的 prod-token，它出现在 JSON 里，可能伴随 Set-Cookie
func (j *cookieJar) setCookie(cookie entity.FiforalCookie) {
	j.set(cookie)
}

func (j *cookieJar) set(cookie entity.FiforalCookie) {
	for i := range j.cookies {
		got := &j.cookies[i]
		if got.value.Name != cookie.Name || got.value.Domain != cookie.Domain || got.value.Path != cookie.Path {
			continue
		}
		if got.value == cookie {
			return
		}
		got.value = cookie
		j.changed = true
		return
	}
	j.cookies = append(j.cookies, storedCookie{value: cookie})
	j.changed = true
}

func (j *cookieJar) delete(name, domain, path string) {
	kept := j.cookies[:0]
	removed := false
	for _, cookie := range j.cookies {
		if cookie.value.Name == name && cookie.value.Domain == domain && cookie.value.Path == path {
			removed = true
			continue
		}
		kept = append(kept, cookie)
	}
	j.cookies = kept
	if removed {
		j.changed = true
	}
}

// header 按 domain / path / Secure 规则拼出 Cookie 请求头
func (j *cookieJar) header(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	secure := strings.EqualFold(u.Scheme, "https") || strings.EqualFold(u.Scheme, "wss")
	path := u.Path
	if path == "" {
		path = "/"
	}
	now := j.now()

	matched := make([]storedCookie, 0, len(j.cookies))
	for _, cookie := range j.cookies {
		c := cookie.value
		if !cookie.expires.IsZero() && !now.Before(cookie.expires) {
			continue
		}
		if c.HostOnly {
			if host != c.Domain {
				continue
			}
		} else if !domainMatch(host, c.Domain) {
			continue
		}
		if !pathMatch(path, c.Path) {
			continue
		}
		if c.Secure && !secure {
			continue
		}
		matched = append(matched, cookie)
	}
	if len(matched) == 0 {
		return ""
	}
	// RFC 6265：路径更长的排在前面
	sort.SliceStable(matched, func(i, k int) bool {
		return len(matched[i].value.Path) > len(matched[k].value.Path)
	})
	parts := make([]string, 0, len(matched))
	for _, cookie := range matched {
		parts = append(parts, cookie.value.Name+"="+cookie.value.Value)
	}
	return strings.Join(parts, "; ")
}

// toEntities 只导出 fifedu 域的 cookie
// 微信域名的临时袋保留在内存
func (j *cookieJar) toEntities() []entity.FiforalCookie {
	out := make([]entity.FiforalCookie, 0, len(j.cookies))
	for _, cookie := range j.cookies {
		if !isFifeduHost(cookie.value.Domain) {
			continue
		}
		out = append(out, cookie.value)
	}
	return out
}

func domainMatch(host, domain string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	domain = strings.ToLower(strings.TrimPrefix(domain, "."))
	if host == "" || domain == "" {
		return false
	}
	return host == domain || strings.HasSuffix(host, "."+domain)
}

func pathMatch(requestPath, cookiePath string) bool {
	if cookiePath == "" {
		return true
	}
	if requestPath == cookiePath {
		return true
	}
	if !strings.HasPrefix(requestPath, cookiePath) {
		return false
	}
	if strings.HasSuffix(cookiePath, "/") {
		return true
	}
	return len(requestPath) > len(cookiePath) && requestPath[len(cookiePath)] == '/'
}

// defaultPath 按 RFC 6265 取请求路径的目录部分
func defaultPath(requestPath string) string {
	if requestPath == "" || !strings.HasPrefix(requestPath, "/") {
		return "/"
	}
	i := strings.LastIndexByte(requestPath, '/')
	if i <= 0 {
		return "/"
	}
	return requestPath[:i]
}
