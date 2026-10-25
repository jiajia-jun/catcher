// Package version 规范化并比较 semver 版本号。
//
// 平台的发布 tag 习惯带 v 前缀（v1.2.3），程序内常量不带（1.2.3），
// 这里统一补上 v 前缀再交给 golang.org/x/mod/semver 比较
package version

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/mod/semver"
)

// semverRe 要求完整的 主.次.修订 三段。x/mod/semver 会接受 v1.0 这类简写，
// 发布 tag 一律写成 vX.Y.Z，这里不接受简写以免版本比较出现意外
var semverRe = regexp.MustCompile(
	`^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// Parse 把 1.2.3、v1.2.3、V1.2.3 统一成 semver 认识的 v1.2.3
func Parse(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("版本号为空")
	}
	if strings.HasPrefix(s, "V") {
		s = "v" + s[1:]
	} else if !strings.HasPrefix(s, "v") {
		s = "v" + s
	}
	if !semverRe.MatchString(s) || !semver.IsValid(s) {
		return "", fmt.Errorf("版本号 %q 不是合法的 semver", raw)
	}
	return s, nil
}

// Display 返回去掉 v 前缀的展示形式；无法解析时原样返回
func Display(raw string) string {
	s, err := Parse(raw)
	if err != nil {
		return strings.TrimSpace(raw)
	}
	return strings.TrimPrefix(s, "v")
}

// IsNewer 判断 latest 是否严格新于 current
func IsNewer(current, latest string) (bool, error) {
	c, err := Parse(current)
	if err != nil {
		return false, fmt.Errorf("当前版本无效: %w", err)
	}
	l, err := Parse(latest)
	if err != nil {
		return false, fmt.Errorf("最新版本无效: %w", err)
	}
	return semver.Compare(l, c) > 0, nil
}
