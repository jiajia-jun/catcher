// Package updater 通过 GitHub Releases API 查询最新正式发布。
//
// 它只发一次 GET，不重试、不下载任何内容、不保存任何状态；
// 更新动作由用户点击后在系统浏览器里完成。失败按 entity 的哨兵错误分类：
// ErrNetwork（网络/超时/5xx）、ErrReleaseUnavailable（404）、
// ErrReleaseRateLimited（403/429）、ErrReleaseInvalid（响应或 tag 无法解析）
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"catcher/internal/domain/entity"
	"catcher/pkg/logger"
	"catcher/pkg/version"
)

const (
	// DefaultBaseURL 是 GitHub API 的根地址
	DefaultBaseURL = "https://api.github.com"
	// DefaultOwner 与 DefaultRepo 指向本项目的发布仓库
	DefaultOwner = "jiajia-jun"
	DefaultRepo  = "catcher"
	// DefaultTimeout 是单次查询的总超时。这只是可选检查，宁可早失败也不拖住界面
	DefaultTimeout = 5 * time.Second
	// maxResponseBytes 限制响应体大小，防止异常响应拖爆内存
	maxResponseBytes = 1 << 20
)

// Options 配置 Checker。Owner、Repo、Current 由装配方提供
type Options struct {
	BaseURL string
	Owner   string
	Repo    string
	Current string
	// Client 为空时使用 DefaultTimeout 的新客户端
	Client *http.Client
	Logger logger.Logger
}

// Checker 实现 domain.ReleaseChecker
type Checker struct {
	baseURL string
	owner   string
	repo    string
	current string
	client  *http.Client
	logger  logger.Logger
}

// New 创建 Checker；缺省项填充默认值
func New(opts Options) *Checker {
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	owner := strings.TrimSpace(opts.Owner)
	if owner == "" {
		owner = DefaultOwner
	}
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		repo = DefaultRepo
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	lg := opts.Logger
	if lg == nil {
		lg = logger.Discard()
	}
	return &Checker{
		baseURL: baseURL,
		owner:   owner,
		repo:    repo,
		current: strings.TrimSpace(opts.Current),
		client:  client,
		logger:  lg,
	}
}

type releasePayload struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
}

// LatestRelease 返回仓库最新一次正式发布。
// GitHub 的 /releases/latest 本身会跳过草稿与预发布，客户端不再过滤
func (c *Checker) LatestRelease(ctx context.Context) (entity.ReleaseInfo, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/releases/latest", c.baseURL, c.owner, c.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return entity.ReleaseInfo{}, fmt.Errorf("%w: 构造请求失败: %v", entity.ErrReleaseInvalid, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.userAgent())

	started := time.Now()
	resp, err := c.client.Do(req)
	elapsed := time.Since(started)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return entity.ReleaseInfo{}, fmt.Errorf("%w: %v", entity.ErrCanceled, err)
		}
		c.logger.Debug("更新检查请求失败", "elapsed_ms", elapsed.Milliseconds(), "error", err)
		return entity.ReleaseInfo{}, fmt.Errorf("%w: %v", entity.ErrNetwork, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		c.logger.Debug("更新检查返回非 200", "status", resp.StatusCode, "elapsed_ms", elapsed.Milliseconds())
		return entity.ReleaseInfo{}, statusError(resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		c.logger.Debug("读取更新响应失败", "elapsed_ms", elapsed.Milliseconds(), "error", err)
		return entity.ReleaseInfo{}, fmt.Errorf("%w: 读取响应失败: %v", entity.ErrNetwork, err)
	}

	var payload releasePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		c.logger.Warn("更新响应不是合法 JSON", "elapsed_ms", elapsed.Milliseconds())
		return entity.ReleaseInfo{}, fmt.Errorf("%w: 响应不是合法 JSON", entity.ErrReleaseInvalid)
	}
	tag, err := version.Parse(payload.TagName)
	if err != nil {
		c.logger.Warn("发布 tag 不是合法版本号", "tag", payload.TagName)
		return entity.ReleaseInfo{}, fmt.Errorf("%w: %v", entity.ErrReleaseInvalid, err)
	}

	releaseURL := releasePageURL(payload.HTMLURL, c.owner, c.repo)
	c.logger.Debug("更新检查完成",
		"status", resp.StatusCode,
		"elapsed_ms", elapsed.Milliseconds(),
		"latest", version.Display(tag),
	)
	return entity.ReleaseInfo{
		Version:     version.Display(tag),
		Tag:         payload.TagName,
		Name:        strings.TrimSpace(payload.Name),
		URL:         releaseURL,
		PublishedAt: payload.PublishedAt,
	}, nil
}

func (c *Checker) userAgent() string {
	if c.current == "" {
		return "Catcher"
	}
	return "Catcher/" + c.current
}

// statusError 把 HTTP 状态码映射到哨兵错误；5xx 视为服务不可达
func statusError(status int) error {
	switch status {
	case http.StatusNotFound, http.StatusGone, http.StatusUnauthorized:
		return fmt.Errorf("%w: HTTP %d", entity.ErrReleaseUnavailable, status)
	case http.StatusForbidden, http.StatusTooManyRequests:
		return fmt.Errorf("%w: HTTP %d", entity.ErrReleaseRateLimited, status)
	default:
		return fmt.Errorf("%w: HTTP %d", entity.ErrNetwork, status)
	}
}

// releasePageURL 只接受 http(s) 链接，异常或缺失时回退到仓库的 releases 页
func releasePageURL(raw, owner, repo string) string {
	if u, err := url.Parse(strings.TrimSpace(raw)); err == nil {
		if u.Scheme == "https" || u.Scheme == "http" {
			return u.String()
		}
	}
	return fmt.Sprintf("https://github.com/%s/%s/releases", owner, repo)
}
