package entity

import "errors"

// ErrNotFound 表示记录不存在。仓储实现必须返回它，方便上层用 errors.Is 判断。
var ErrNotFound = errors.New("记录不存在")

// 以下哨兵错误对应设计文档 §8.3 的错误分类，适配器用 fmt.Errorf("%w: ...") 包装细节
var (
	// ErrAuthRequired 表示尚未登录，不能发起抓取
	ErrAuthRequired = errors.New("该平台尚未登录")
	// ErrAuthExpired 表示会话失效，需要续期或重新登录
	ErrAuthExpired = errors.New("登录已失效")
	// ErrAuthFailed 表示登录流程本身失败（扫码确认、换取会话等）
	ErrAuthFailed = errors.New("登录失败")
	// ErrPlatformChanged 表示响应结构无法按文档解析
	ErrPlatformChanged = errors.New("平台返回格式变化")
	// ErrBusiness 表示平台明确拒绝（权限不足、参数被拒等），不是会话失效
	ErrBusiness = errors.New("平台拒绝请求")
	// ErrNetwork 表示连接、DNS、TLS 或超时
	ErrNetwork = errors.New("网络请求失败或超时")
	// ErrCanceled 表示用户取消
	ErrCanceled = errors.New("已取消")

	// ErrReleaseUnavailable 表示发布仓库不可访问：不存在、未公开或没有发布
	ErrReleaseUnavailable = errors.New("发布仓库不可访问")
	// ErrReleaseRateLimited 表示更新查询被 GitHub 限流
	ErrReleaseRateLimited = errors.New("更新查询被限流")
	// ErrReleaseInvalid 表示发布信息无法解析
	ErrReleaseInvalid = errors.New("发布信息格式不正确")
)
