package fiforal

import (
	"strings"
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

func TestJarDomainAndPath(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	jar := newCookieJar(nil, func() time.Time { return now })
	jar.add("https://www.fifedu.com/iplat/fifLogin/index.html", []string{
		"prod-token=abc; Domain=.fifedu.com; Path=/; Secure",
		"JSESSIONID=sess; Path=/cas-server",
	})

	if got := jar.header("https://moral.fifedu.com/kyxl-app/stu/task/teaTaskList"); !strings.Contains(got, "prod-token=abc") {
		t.Fatalf("域 cookie 没有发给子域: %q", got)
	} else if strings.Contains(got, "JSESSIONID") {
		t.Fatalf("host-only cookie 被发给了错误主机: %q", got)
	}

	got := jar.header("https://www.fifedu.com/cas-server/login")
	if !strings.Contains(got, "JSESSIONID=sess") || !strings.Contains(got, "prod-token=abc") {
		t.Fatalf("同主机同路径没有带上 cookie: %q", got)
	}

	if got := jar.header("https://www.fifedu.com/other"); strings.Contains(got, "JSESSIONID") {
		t.Fatalf("path 不匹配的 cookie 被发送: %q", got)
	}
	if got := jar.header("http://www.fifedu.com/other"); strings.Contains(got, "prod-token") {
		t.Fatalf("Secure cookie 被发给了 http: %q", got)
	}
}

func TestJarHostOnlyExactHost(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	jar := newCookieJar(nil, func() time.Time { return now })
	jar.add("https://pass.fifedu.com/iplat-pass/x", []string{"hostonly=1"})

	if got := jar.header("https://pass.fifedu.com/iplat-pass/y"); got != "hostonly=1" {
		t.Fatalf("host-only cookie 没有发给原主机: %q", got)
	}
	if got := jar.header("https://oral.fifedu.com/iplat-pass/y"); got != "" {
		t.Fatalf("host-only cookie 被发给了子域/兄弟域: %q", got)
	}
}

func TestJarSameNameDifferentPath(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	jar := newCookieJar(nil, func() time.Time { return now })
	jar.add("https://www.fifedu.com/iplat/x", []string{
		"SESSION=root; Path=/",
		"SESSION=testcenter; Path=/testcenter",
	})

	if got := jar.header("https://www.fifedu.com/testcenter/login"); !strings.Contains(got, "SESSION=testcenter") {
		t.Fatalf("同名 cookie 没有按 path 选择: %q", got)
	}
	if got := jar.header("https://www.fifedu.com/other"); !strings.Contains(got, "SESSION=root") || strings.Contains(got, "testcenter") {
		t.Fatalf("path 选择错误: %q", got)
	}
	if got := len(jar.toEntities()); got != 2 {
		t.Fatalf("同名不同路径的 cookie 应都保留，得到 %d 条", got)
	}
}

func TestJarDelete(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	jar := newCookieJar([]entity.FiforalCookie{{
		Name: "prod-token", Value: "old", Domain: "fifedu.com", Path: "/",
	}}, func() time.Time { return now })

	jar.add("https://www.fifedu.com/x", []string{"prod-token=; Domain=.fifedu.com; Path=/; Max-Age=0"})
	if got := jar.header("https://moral.fifedu.com/x"); got != "" {
		t.Fatalf("Max-Age=0 没有删除 cookie: %q", got)
	}
	if !jar.changed {
		t.Fatal("删除 cookie 应标记 changed")
	}
}

func TestJarExpiredCookieIgnored(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	jar := newCookieJar(nil, func() time.Time { return now })
	jar.add("https://www.fifedu.com/x", []string{"a=1; Expires=Wed, 01 Jan 2020 00:00:00 GMT"})
	if got := jar.header("https://www.fifedu.com/x"); got != "" {
		t.Fatalf("已过期的 cookie 不应发送: %q", got)
	}
}

func TestJarRejectsForeignKey(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	jar := newCookieJar(nil, func() time.Time { return now })
	jar.add("https://www.fifedu.com/x", []string{"evil=1; Domain=.evil.com; Path=/"})
	if got := jar.toEntities(); len(got) != 0 {
		t.Fatalf("别的域写入的 cookie 不应被接收: %#v", got)
	}
}

func TestJarToEntitiesOnlyFifedu(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	jar := newCookieJar(nil, func() time.Time { return now })
	jar.add("https://open.weixin.qq.com/connect/qrconnect", []string{"wx=1"})
	jar.add("https://www.fifedu.com/x", []string{"prod-token=1; Domain=.fifedu.com; Path=/"})

	got := jar.toEntities()
	if len(got) != 1 || got[0].Name != "prod-token" {
		t.Fatalf("只应导出 fifedu 域 cookie: %#v", got)
	}
}

func TestDefaultPath(t *testing.T) {
	cases := map[string]string{
		"":                  "/",
		"/":                 "/",
		"/iplat/x.html":     "/iplat",
		"/testcenter/login": "/testcenter",
		"relative":          "/",
	}
	for in, want := range cases {
		if got := defaultPath(in); got != want {
			t.Errorf("defaultPath(%q) = %q, want %q", in, got, want)
		}
	}
}
