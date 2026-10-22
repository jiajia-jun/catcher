package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"catcher/internal/domain/entity"
)

const (
	// maxAutoRefresh 是二维码过期后自动换码的次数上限
	maxAutoRefresh = 3
)

type loginJob struct {
	id      string
	cancel  context.CancelFunc
	refresh chan struct{}
}

// StartYuketangLogin 启动扫码登录任务，二维码通过事件推给前端
func (s *Service) StartYuketangLogin(ctx context.Context) (*RefreshStart, error) {
	_ = ctx
	if s.qrLogin == nil {
		return nil, fmt.Errorf("雨课堂扫码登录尚未接入")
	}

	// 同时只保留一个扫码任务，重复打开弹窗时以新任务为准
	s.cancelLoginJobs()

	jobCtx, cancel := context.WithCancel(context.Background())
	job := &loginJob{
		id:      newJobID("login"),
		cancel:  cancel,
		refresh: make(chan struct{}, 1),
	}

	s.jobsMu.Lock()
	s.loginJobs[job.id] = job
	s.jobsMu.Unlock()

	go s.runYuketangLogin(jobCtx, job)
	return &RefreshStart{JobID: job.id, Platforms: []entity.Platform{entity.PlatformYuketang}}, nil
}

// CancelYuketangLogin 停止轮询
func (s *Service) CancelYuketangLogin(jobID string) error {
	s.jobsMu.Lock()
	job, ok := s.loginJobs[jobID]
	s.jobsMu.Unlock()
	if !ok {
		return fmt.Errorf("扫码任务不存在或已结束")
	}
	job.cancel()
	return nil
}

// RefreshYuketangLogin 在自动换码次数用尽后，由用户手动换一张码
func (s *Service) RefreshYuketangLogin(jobID string) error {
	s.jobsMu.Lock()
	job, ok := s.loginJobs[jobID]
	s.jobsMu.Unlock()
	if !ok {
		return fmt.Errorf("扫码任务不存在或已结束")
	}
	select {
	case job.refresh <- struct{}{}:
	default:
	}
	return nil
}

func (s *Service) cancelLoginJobs() {
	s.jobsMu.Lock()
	jobs := make([]*loginJob, 0, len(s.loginJobs))
	for _, job := range s.loginJobs {
		jobs = append(jobs, job)
	}
	s.jobsMu.Unlock()
	for _, job := range jobs {
		job.cancel()
	}
}

func (s *Service) runYuketangLogin(ctx context.Context, job *loginJob) {
	defer func() {
		s.jobsMu.Lock()
		delete(s.loginJobs, job.id)
		s.jobsMu.Unlock()
		job.cancel()
	}()

	autoRefreshes := 0
	for {
		if ctx.Err() != nil {
			s.emitQRState(job.id, entity.QRFailed, "已取消")
			return
		}

		challenge, err := s.qrLogin.BeginQR(ctx)
		if err != nil {
			s.emitQRState(job.id, entity.QRFailed, err.Error())
			return
		}
		s.emitEvent("auth.yuketang.qr", map[string]any{
			"jobId":     job.id,
			"qrContent": challenge.Content,
			"expiresAt": challenge.ExpiresAt.UTC().Format(time.RFC3339),
		})
		s.emitQRState(job.id, entity.QRWaiting, "等待扫码")

		needNewCode := false
		for !needNewCode {
			state, session, err := s.qrLogin.PollQR(ctx, challenge)
			if err != nil {
				if errors.Is(err, entity.ErrCanceled) {
					s.emitQRState(job.id, entity.QRFailed, "已取消")
				} else {
					s.emitQRState(job.id, entity.QRFailed, err.Error())
				}
				return
			}

			switch state {
			case entity.QRWaiting:
				continue
			case entity.QRScanned:
				s.emitQRState(job.id, entity.QRScanned, "已扫码，正在确认")
			case entity.QRSucceeded:
				if session == nil {
					s.emitQRState(job.id, entity.QRFailed, "登录成功但没有拿到会话")
					return
				}
				if err := s.finishYuketangLogin(ctx, *session); err != nil {
					s.emitQRState(job.id, entity.QRFailed, err.Error())
					return
				}
				s.emitQRState(job.id, entity.QRSucceeded, "登录成功")
				return
			case entity.QRExpired:
				needNewCode = true
			case entity.QRFailed:
				s.emitQRState(job.id, entity.QRFailed, "扫码登录失败")
				return
			}
		}

		autoRefreshes++
		if autoRefreshes <= maxAutoRefresh {
			s.emitQRState(job.id, entity.QRExpired,
				fmt.Sprintf("二维码已过期，正在自动刷新（%d/%d）", autoRefreshes, maxAutoRefresh))
			continue
		}

		s.emitQRState(job.id, entity.QRExpired, "二维码已过期，请手动刷新")
		select {
		case <-ctx.Done():
			s.emitQRState(job.id, entity.QRFailed, "已取消")
			return
		case <-job.refresh:
			autoRefreshes = 0
			continue
		}
	}
}

func (s *Service) finishYuketangLogin(ctx context.Context, session entity.Session) error {
	account, err := s.accounts.GetAccount(ctx, entity.PlatformYuketang)
	if err != nil {
		return fmt.Errorf("读取雨课堂账号失败: %w", err)
	}
	if err := s.sessions.Save(ctx, account.ID, session.Secret, nil, s.now()); err != nil {
		return fmt.Errorf("保存雨课堂会话失败: %w", err)
	}

	var secret entity.YuketangSecret
	if err := json.Unmarshal(session.Secret, &secret); err == nil && secret.UserID != "" {
		if err := s.accounts.SetDisplayName(ctx, account.Platform, secret.UserID, s.now()); err != nil {
			s.logger.Warn("更新雨课堂显示名失败", "error", err)
		}
	}

	s.setLoginState(ctx, entity.PlatformYuketang, entity.LoginConnected, nil)
	return nil
}

func (s *Service) emitQRState(jobID string, state entity.QRState, message string) {
	s.emitEvent("auth.yuketang.state", map[string]any{
		"jobId":   jobID,
		"state":   string(state),
		"message": message,
	})
}
