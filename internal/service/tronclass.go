package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"catcher/internal/domain/entity"
)

// ErrRefreshRunning 表示同一平台正在刷新，暂不接受导入
var ErrRefreshRunning = errors.New("该平台正在刷新")

// ImportTronclassSession 验证并保存浏览器里的 session，返回用于展示的标识
//
// 导入只做验证与保存，不自动抓取；失败时不动已保存的会话与登录态
func (s *Service) ImportTronclassSession(ctx context.Context, raw string) (string, error) {
	if s.importer == nil {
		return "", fmt.Errorf("学在重邮会话导入尚未接入")
	}

	// 刷新会轮换 session，两边同时进行会互相覆盖
	s.jobsMu.Lock()
	_, busy := s.running[entity.PlatformTronclass]
	s.jobsMu.Unlock()
	if busy {
		return "", ErrRefreshRunning
	}

	session, err := s.importer.ImportSession(ctx, raw)
	if err != nil {
		return "", err
	}
	if session == nil {
		return "", fmt.Errorf("%w: 验证成功但没有返回会话", entity.ErrAuthFailed)
	}

	account, err := s.accounts.GetAccount(ctx, entity.PlatformTronclass)
	if err != nil {
		return "", fmt.Errorf("读入学在重邮账号失败: %w", err)
	}
	if err := s.sessions.Save(ctx, account.ID, session.Secret, nil, s.now()); err != nil {
		return "", fmt.Errorf("保存学在重邮会话失败: %w", err)
	}

	name := "已导入"
	var secret entity.TronclassSecret
	if err := json.Unmarshal(session.Secret, &secret); err == nil {
		if uid := strings.TrimSpace(secret.UserID); uid != "" {
			name = uid
		}
	}
	if err := s.accounts.SetDisplayName(ctx, account.Platform, name, s.now()); err != nil {
		s.logger.Warn("更新学在重邮显示名失败", "error", err)
	}
	s.setLoginState(ctx, entity.PlatformTronclass, entity.LoginConnected, nil)
	return name, nil
}
