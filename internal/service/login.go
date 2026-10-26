package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"catcher/internal/domain"
	"catcher/internal/domain/entity"
)

const (
	// maxAutoRefresh 是二维码过期后自动换码的次数上限
	maxAutoRefresh = 3
)

type loginJob struct {
	id       string
	platform entity.Platform
	cancel   context.CancelFunc
	refresh  chan struct{}
}

// StartYuketangLogin 启动雨课堂扫码登录任务
func (s *Service) StartYuketangLogin(ctx context.Context) (*RefreshStart, error) {
	_ = ctx
	return s.startQRLogin(entity.PlatformYuketang)
}

// StartChaoxingLogin 启动学习通扫码登录任务
func (s *Service) StartChaoxingLogin(ctx context.Context) (*RefreshStart, error) {
	_ = ctx
	return s.startQRLogin(entity.PlatformChaoxing)
}

// startQRLogin 同时只保留一个扫码任务，重复打开弹窗时以新任务为准
func (s *Service) startQRLogin(platform entity.Platform) (*RefreshStart, error) {
	qr := s.qrLogins[platform]
	if qr == nil {
		return nil, fmt.Errorf("%s扫码登录尚未接入", platform.DisplayName())
	}

	s.cancelLoginJobs()

	jobCtx, cancel := context.WithCancel(context.Background())
	job := &loginJob{
		id:       newJobID("login"),
		platform: platform,
		cancel:   cancel,
		refresh:  make(chan struct{}, 1),
	}

	s.jobsMu.Lock()
	s.loginJobs[job.id] = job
	s.jobsMu.Unlock()

	go s.runQRLogin(jobCtx, job, qr)
	return &RefreshStart{JobID: job.id, Platforms: []entity.Platform{platform}}, nil
}

// CancelYuketangLogin 停止雨课堂扫码轮询
func (s *Service) CancelYuketangLogin(jobID string) error {
	return s.CancelQRLogin(jobID)
}

// CancelChaoxingLogin 停止学习通扫码轮询
func (s *Service) CancelChaoxingLogin(jobID string) error {
	return s.CancelQRLogin(jobID)
}

// CancelQRLogin 停止扫码轮询
func (s *Service) CancelQRLogin(jobID string) error {
	s.jobsMu.Lock()
	job, ok := s.loginJobs[jobID]
	s.jobsMu.Unlock()
	if !ok {
		return fmt.Errorf("扫码任务不存在或已结束")
	}
	job.cancel()
	return nil
}

// RefreshYuketangLogin 在自动换码次数用尽后，由用户手动换一张雨课堂二维码
func (s *Service) RefreshYuketangLogin(jobID string) error {
	return s.RefreshQRLogin(jobID)
}

// RefreshChaoxingLogin 在自动换码次数用尽后，由用户手动换一张学习通二维码
func (s *Service) RefreshChaoxingLogin(jobID string) error {
	return s.RefreshQRLogin(jobID)
}

// RefreshQRLogin 通知扫码任务重新取码
func (s *Service) RefreshQRLogin(jobID string) error {
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

// pollResult 是一次轮询的结果
type pollResult struct {
	state   entity.QRState
	session *entity.Session
	err     error
}

// pollInterruptible 在轮询期间监听「换一张」。
//
// 雨课堂的轮询是长轮询、学习通内部还要睡 3 秒，直接调用 PollQR 会让按钮看起来失效：
// 用户点换码后必须立刻中断旧轮询，否则信号只能积压在 channel 里，
// 直到二维码自己过期才被处理
func (s *Service) pollInterruptible(ctx context.Context, job *loginJob, qr domain.QRLogin,
	challenge *entity.QRChallenge) (pollResult, bool) {
	// 上一轮点过换码、信号还压在缓冲里
	select {
	case <-job.refresh:
		return pollResult{}, true
	default:
	}

	pollCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan pollResult, 1)
	go func() {
		state, session, err := qr.PollQR(pollCtx, challenge)
		done <- pollResult{state: state, session: session, err: err}
	}()

	select {
	case res := <-done:
		return res, false
	case <-job.refresh:
		// 只取消在途请求，不等它返回：适配器即使忽略 ctx 也不会卡住任务
		cancel()
		return pollResult{}, true
	case <-ctx.Done():
		cancel()
		return pollResult{err: fmt.Errorf("%w: %v", entity.ErrCanceled, ctx.Err())}, false
	}
}

func (s *Service) runQRLogin(ctx context.Context, job *loginJob, qr domain.QRLogin) {
	defer func() {
		s.jobsMu.Lock()
		delete(s.loginJobs, job.id)
		s.jobsMu.Unlock()
		job.cancel()
	}()

	autoRefreshes := 0
	for {
		if ctx.Err() != nil {
			s.emitQRState(job, entity.QRFailed, "已取消")
			return
		}

		challenge, err := qr.BeginQR(ctx)
		if err != nil {
			s.emitQRState(job, entity.QRFailed, err.Error())
			return
		}
		s.emitEvent("auth."+string(job.platform)+".qr", map[string]any{
			"jobId":     job.id,
			"qrContent": challenge.Content,
			"expiresAt": challenge.ExpiresAt.UTC().Format(time.RFC3339),
		})
		s.emitQRState(job, entity.QRWaiting, "等待扫码")

		needNewCode := false
		manual := false
		for !needNewCode {
			res, refreshed := s.pollInterruptible(ctx, job, qr, challenge)
			if refreshed {
				manual = true
				break
			}
			if res.err != nil {
				if errors.Is(res.err, entity.ErrCanceled) {
					s.emitQRState(job, entity.QRFailed, "已取消")
				} else {
					s.emitQRState(job, entity.QRFailed, res.err.Error())
				}
				return
			}

			switch res.state {
			case entity.QRWaiting:
				continue
			case entity.QRScanned:
				s.emitQRState(job, entity.QRScanned, qrScannedMessage(job.platform))
			case entity.QRSucceeded:
				if res.session == nil {
					s.emitQRState(job, entity.QRFailed, "登录成功但没有拿到会话")
					return
				}
				if err := s.finishQRLogin(ctx, job.platform, *res.session); err != nil {
					s.emitQRState(job, entity.QRFailed, err.Error())
					return
				}
				s.emitQRState(job, entity.QRSucceeded, "登录成功")
				return
			case entity.QRExpired:
				needNewCode = true
			case entity.QRFailed:
				s.emitQRState(job, entity.QRFailed, "扫码登录失败")
				return
			}
		}

		// 用户主动换码：不算过期，也不消耗自动换码次数
		if manual {
			autoRefreshes = 0
			continue
		}

		autoRefreshes++
		if autoRefreshes <= maxAutoRefresh {
			s.emitQRState(job, entity.QRExpired,
				fmt.Sprintf("二维码已过期，正在自动刷新（%d/%d）", autoRefreshes, maxAutoRefresh))
			continue
		}

		s.emitQRState(job, entity.QRExpired, "二维码已过期，请手动刷新")
		select {
		case <-ctx.Done():
			s.emitQRState(job, entity.QRFailed, "已取消")
			return
		case <-job.refresh:
			autoRefreshes = 0
			continue
		}
	}
}

func qrScannedMessage(platform entity.Platform) string {
	if platform == entity.PlatformChaoxing {
		return "已扫码，请在手机上确认"
	}
	return "已扫码，正在确认"
}

// finishQRLogin 保存会话并更新登录态。
// 学习通扫码拿不到手机号，只在显示名为空时写一个通用名字
func (s *Service) finishQRLogin(ctx context.Context, platform entity.Platform, session entity.Session) error {
	account, err := s.accounts.GetAccount(ctx, platform)
	if err != nil {
		return fmt.Errorf("读取%s账号失败: %w", platform.DisplayName(), err)
	}
	if err := s.sessions.Save(ctx, account.ID, session.Secret, nil, s.now()); err != nil {
		return fmt.Errorf("保存%s会话失败: %w", platform.DisplayName(), err)
	}

	if platform == entity.PlatformYuketang {
		var secret entity.YuketangSecret
		if err := json.Unmarshal(session.Secret, &secret); err == nil && secret.UserID != "" {
			if err := s.accounts.SetDisplayName(ctx, account.Platform, secret.UserID, s.now()); err != nil {
				s.logger.Warn("更新雨课堂显示名失败", "error", err)
			}
		}
	}
	if platform == entity.PlatformChaoxing && strings.TrimSpace(account.DisplayName) == "" {
		if err := s.accounts.SetDisplayName(ctx, account.Platform, "学习通账号", s.now()); err != nil {
			s.logger.Warn("更新学习通显示名失败", "error", err)
		}
	}

	s.setLoginState(ctx, platform, entity.LoginConnected, nil)
	return nil
}

func (s *Service) emitQRState(job *loginJob, state entity.QRState, message string) {
	s.emitEvent("auth."+string(job.platform)+".state", map[string]any{
		"jobId":   job.id,
		"state":   string(state),
		"message": message,
	})
}
