package entity

import (
	"encoding/json"
	"time"
)

// Account 是某平台在本机的唯一账号。v1 单用户、每个平台一行。
type Account struct {
	ID          int64
	Platform    Platform
	DisplayName string // 只存脱敏后的标识，如 138****1234
	LoginState  LoginState
	LastLoginAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Session 是持久化登录态。Secret 是尚未加密的 JSON 明文，
// 只能在进程内短暂存在；落库前必须经过 secretstore 加密。
type Session struct {
	AccountID int64
	Platform  Platform
	Secret    json.RawMessage
	UpdatedAt time.Time
}

// ChaoxingSecret 是学习通会话秘密。cookie 不按 host 过滤、不硬编码名字。
type ChaoxingSecret struct {
	Cookies   []Cookie  `json:"cookies"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Cookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// YuketangSecret 只持久化 sessionid 与用于免扫码续期的 Auth。
type YuketangSecret struct {
	SessionID string `json:"sessionid"`
	Auth      string `json:"auth"`
	UserID    string `json:"user_id"`
}

// TronclassSecret 只持久化 session；role_token 不依赖、不保存。
type TronclassSecret struct {
	Session string `json:"session"`
	UserID  string `json:"user_id"`
}

// PigaiSecret 只持久化 cookie 表。_JUKU_USER 整体保存、不解析，
// 因为它的字段语义未证实，重建请求头比原样回放更容易出错。
type PigaiSecret struct {
	Cookies   []Cookie  `json:"cookies"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MaskPhone 把 11 位手机号变成 138****1234，用于 accounts.display_name
func MaskPhone(phone string) string {
	if len(phone) != 11 {
		return phone
	}
	return phone[:3] + "****" + phone[7:]
}
