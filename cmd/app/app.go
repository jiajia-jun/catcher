// Package app 负责装配：数据目录 → 配置 → logger → sqlite → repository
// → service → handle → wails.Run。
//
// 它不是 main 包：Wails v2 要求入口位于仓库根目录，以便 //go:embed 能
// 引用 frontend/dist；根 main.go 只做 embed 并调用本包。
package app

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"catcher/internal/handle"
	"catcher/internal/infra/sqlite"
	"catcher/internal/service"
	"catcher/pkg/config"
	"catcher/pkg/logger"
)

// Version 是构建版本。M0 先写死，发布前改为 ldflags 注入。
const Version = "0.1.0-m0"

// Run 启动 Wails 应用，阻塞到窗口关闭。
func Run(assets fs.FS) error {
	ctx := context.Background()
	dataDirOpts := config.DefaultDataDirOptions()

	dataDir, err := config.ResolveDataDir(dataDirOpts)
	if err != nil {
		return err
	}

	cfg, err := config.LoadOrCreate(dataDir)
	if err != nil {
		return err
	}

	lg, logCloser, err := logger.New(logger.Options{
		Dir:        filepath.Join(dataDir, "logs"),
		Level:      cfg.Log.Level,
		MaxSizeMB:  cfg.Log.MaxSizeMB,
		MaxBackups: cfg.Log.MaxBackups,
		MaxAgeDays: cfg.Log.MaxAgeDays,
		Console:    dataDirOpts.DevMode,
	})
	if err != nil {
		return err
	}

	db, err := sqlite.Open(ctx, filepath.Join(dataDir, "catcher.db"))
	if err != nil {
		_ = logCloser.Close()
		return err
	}

	closeAll := func() {
		_ = db.Close()
		_ = logCloser.Close()
	}

	if err := db.Migrate(ctx); err != nil {
		closeAll()
		return err
	}

	repo := sqlite.NewRepo(db)
	if err := repo.EnsurePlatforms(ctx, time.Now()); err != nil {
		closeAll()
		return err
	}

	svc := service.New(repo, repo, cfg, dataDir, Version)
	h := handle.New(svc)

	lg.Info("应用启动",
		"version", Version,
		"data_dir", dataDir,
		"log_level", cfg.Log.Level,
	)

	var once sync.Once
	shutdown := func() {
		once.Do(func() {
			lg.Info("应用退出")
			closeAll()
		})
	}
	defer shutdown()

	wailsErr := wails.Run(&options.App{
		Title:     "Catcher",
		Width:     1180,
		Height:    780,
		MinWidth:  960,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 248, G: 250, B: 252, A: 1},
		OnStartup:        h.Startup,
		OnShutdown: func(context.Context) {
			shutdown()
		},
		Bind: []interface{}{h},
	})
	if wailsErr != nil {
		return fmt.Errorf("Wails 运行失败: %w", wailsErr)
	}
	return nil
}
