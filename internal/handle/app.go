// Package handle 是 Wails 绑定层：参数校验、错误翻译、DTO 转换
//
// 它不直接访问数据库或 HTTP；所有用例都经过 service
package handle

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"catcher/internal/service"
)

// App 是 Wails 绑定的接收者。前端通过 window.go.handle.App 调用这些方法
type App struct {
	svc *service.Service
	ctx context.Context
}

func New(svc *service.Service) *App {
	return &App{svc: svc}
}

// Startup 由 Wails 在窗口启动后调用
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	// 事件通过 Wails 推给前端；关闭窗口后 runtime 不再发送
	a.svc.SetEmitter(func(name string, payload any) {
		runtime.EventsEmit(ctx, name, payload)
	})
}

func (a *App) context() context.Context {
	if a.ctx == nil {
		return context.Background()
	}
	return a.ctx
}
