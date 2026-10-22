package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"catcher/internal/domain/entity"
)

// LoginChaoxing 用手机号+密码登录学习通，并保存会话。
//
// remember 为 true 时同时保存 DPAPI 加密的密码，供会话失效后自动重登一次；
// 为 false 时明确删除旧密码，避免「取消记住」后仍能自动重登
func (s *Service) LoginChaoxing(ctx context.Context, creds entity.PasswordCredential, remember bool) error {
	auth, ok := s.authenticators[entity.PlatformChaoxing]
	if !ok {
		return fmt.Errorf("学习通密码登录尚未接入")
	}
	creds.Phone = strings.TrimSpace(creds.Phone)

	session, err := auth.LoginWithPassword(ctx, creds)
	if err != nil {
		return err
	}
	if session == nil {
		return fmt.Errorf("%w: 登录成功但没有返回会话", entity.ErrAuthFailed)
	}

	account, err := s.accounts.GetAccount(ctx, entity.PlatformChaoxing)
	if err != nil {
		return fmt.Errorf("读取学习通账号失败: %w", err)
	}
	if err := s.sessions.Save(ctx, account.ID, session.Secret, nil, s.now()); err != nil {
		return fmt.Errorf("保存学习通会话失败: %w", err)
	}

	if remember {
		if s.credentials == nil {
			s.logger.Warn("未接入凭据仓储，密码不会被保存")
		} else if err := s.credentials.Save(ctx, account.ID, creds, s.now()); err != nil {
			// 登录已经成功，不能因为保存密码失败把用户挡在门外
			s.logger.Warn("保存学习通密码失败", "error", err)
		}
	} else if s.credentials != nil {
		if err := s.credentials.Delete(ctx, account.ID); err != nil {
			s.logger.Warn("清除学习通密码失败", "error", err)
		}
	}

	if err := s.accounts.SetDisplayName(ctx, account.Platform,
		entity.MaskPhone(creds.Phone), s.now()); err != nil {
		s.logger.Warn("更新学习通显示名失败", "error", err)
	}
	s.setLoginState(ctx, entity.PlatformChaoxing, entity.LoginConnected, nil)
	return nil
}

// renew 在抓取遇到 ErrAuthExpired 时续期一次。
//
// 雨课堂用保存的 Auth 重放；学习通没有可复用的凭据，只能拿 DPAPI 里的
// 手机号+密码重新登录
func (s *Service) renew(ctx context.Context, account entity.Account, session entity.Session) (*entity.Session, error) {
	if renewer, ok := s.renewers[account.Platform]; ok {
		return renewer.Renew(ctx, session)
	}

	auth, ok := s.authenticators[account.Platform]
	if !ok {
		return nil, fmt.Errorf("%w: 该平台没有可用的续期方式", entity.ErrAuthExpired)
	}
	if s.credentials == nil {
		return nil, fmt.Errorf("%w: 未保存可用于重登的凭据", entity.ErrAuthExpired)
	}
	creds, err := s.credentials.Get(ctx, account.ID)
	if err != nil {
		if errors.Is(err, entity.ErrNotFound) {
			return nil, fmt.Errorf("%w: 未保存密码，请重新登录", entity.ErrAuthExpired)
		}
		return nil, err
	}
	return auth.LoginWithPassword(ctx, *creds)
}
