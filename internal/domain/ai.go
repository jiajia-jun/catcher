package domain

import (
	"context"
	"time"

	"catcher/internal/domain/entity"
)

// AIModel 只接收白名单上下文，不持有平台发送器或平台凭据。
type AIModel interface {
	Generate(context.Context, entity.AIModelRequest) (entity.AIModelResult, error)
}

type AIRepository interface {
	Settings(context.Context) (entity.AISettings, error)
	SaveSettings(context.Context, entity.AISettings) error
	Snapshot(context.Context, []int64) (entity.AISnapshot, error)
	Reserve(context.Context, string, string, int, int, time.Time) error
	Finish(context.Context, string, string, entity.AIModelResult, time.Time) error
	Usage(context.Context, string) (entity.AIUsage, error)
	ConfirmPlan(context.Context, entity.StudyPlan) (entity.StudyPlan, error)
	Plans(context.Context) ([]entity.StudyPlan, error)
	SetTaskDone(context.Context, string, string, bool) error
}
