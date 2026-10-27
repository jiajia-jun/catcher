package domain

import "catcher/internal/domain/entity"

type Emails interface {
	// GetInfo 返回当前配置的副本；未配置时返回空值
	GetInfo() *entity.EmailSender
	SetInfo(info *entity.EmailSender) error
	SendEmail(desc *entity.EmailContent) error
}
