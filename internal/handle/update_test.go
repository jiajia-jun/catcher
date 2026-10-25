package handle

import (
	"fmt"
	"testing"

	"catcher/internal/domain/entity"
)

// 四类失败要在手动检查时有可区分的文案
func TestUpdateAPIErrorMessages(t *testing.T) {
	cases := []struct {
		err     error
		code    string
		message string
	}{
		{
			err:     fmt.Errorf("%w: dial tcp: timeout", entity.ErrNetwork),
			code:    "NETWORK",
			message: "检查更新失败：网络不可达",
		},
		{
			err:     fmt.Errorf("%w: HTTP 404", entity.ErrReleaseUnavailable),
			code:    "RELEASE_UNAVAILABLE",
			message: "检查更新失败：发布仓库暂不可访问",
		},
		{
			err:     fmt.Errorf("%w: HTTP 403", entity.ErrReleaseRateLimited),
			code:    "RELEASE_RATE_LIMITED",
			message: "检查更新失败：GitHub 请求过于频繁，请稍后再试",
		},
		{
			err:     fmt.Errorf("%w: tag_name 缺失", entity.ErrReleaseInvalid),
			code:    "RELEASE_INVALID",
			message: "检查更新失败：发布信息格式不正确",
		},
	}
	for _, c := range cases {
		got := updateAPIError(c.err)
		if got.Code != c.code {
			t.Errorf("err=%v code=%q, want %q", c.err, got.Code, c.code)
		}
		if got.Message != c.message {
			t.Errorf("err=%v message=%q, want %q", c.err, got.Message, c.message)
		}
	}
}
