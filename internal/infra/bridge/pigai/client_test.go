package pigai

import (
	"errors"
	"strings"
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

func TestParseUnixSeconds(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string // 东八区时间，空表示 nil
		wantErr bool
	}{
		{name: "空串表示未设置", raw: ""},
		{name: "零表示未设置", raw: "0"},
		{name: "两端空白", raw: "  "},
		{name: "10 位秒", raw: "1761700000", want: "2025-10-29T09:06:40+08:00"},
		{name: "13 位毫秒必须报错", raw: "1761700000000", wantErr: true},
		{name: "9 位不是 Unix 秒", raw: "176170000", wantErr: true},
		{name: "非数字", raw: "2026-10-29", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseUnixSeconds(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际得到 %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if tc.want == "" {
				if got != nil {
					t.Fatalf("期望 nil，实际 %v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("期望 %s，实际 nil", tc.want)
			}
			if got.Format(time.RFC3339) != tc.want {
				t.Fatalf("= %s, want %s", got.Format(time.RFC3339), tc.want)
			}
		})
	}
}

func TestEnvelopeError(t *testing.T) {
	cases := []struct {
		name string
		env  envelope
		want error
	}{
		{name: "成功", env: envelope{Error: 0}},
		{name: "未登录", env: envelope{Error: 302, ErrorDes: "请先登录"}, want: entity.ErrAuthExpired},
		{name: "业务拒绝", env: envelope{Error: 9, ErrorDes: "参数错误"}, want: entity.ErrBusiness},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := envelopeError(&tc.env)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("期望成功，实际 %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("= %v, want %v", err, tc.want)
			}
			if tc.env.ErrorDes != "" && !strings.Contains(err.Error(), tc.env.ErrorDes) {
				t.Fatalf("错误信息应带平台描述: %v", err)
			}
		})
	}
}

func TestCookieJarKeepsOrderAndOverwrites(t *testing.T) {
	jar := newCookieJar([]entity.Cookie{{Name: "PHPSESSID", Value: "a"}})
	if jar.changed {
		t.Fatal("从已有 cookie 初始化不应标记变更")
	}
	jar.add([]string{"_JUKU_USER=first; path=/; HttpOnly", "other=1"})
	jar.add([]string{"_JUKU_USER=second"})
	if got := jar.header(); got != "PHPSESSID=a; _JUKU_USER=second; other=1" {
		t.Fatalf("header = %q", got)
	}
	if !jar.changed {
		t.Fatal("写入新 cookie 应标记变更")
	}
	if len(jar.toEntities()) != 3 {
		t.Fatalf("cookies = %d", len(jar.toEntities()))
	}
}

func TestIsLoginHost(t *testing.T) {
	c := New(Options{BaseURL: "https://www.pigai.org"})
	for _, host := range []string{"www.pigai.org", "pigai.org", "api.pigai.org", "WWW.PIGAI.ORG."} {
		if !c.isLoginHost(host) {
			t.Fatalf("%s 应被允许", host)
		}
	}
	for _, host := range []string{"evil.com", "pigai.org.evil.com", ""} {
		if c.isLoginHost(host) {
			t.Fatalf("%s 不应被允许", host)
		}
	}
}
