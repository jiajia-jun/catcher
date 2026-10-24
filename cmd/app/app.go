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
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"catcher/internal/domain"
	"catcher/internal/domain/entity"
	"catcher/internal/handle"
	"catcher/internal/infra/bridge/chaoxing"
	"catcher/internal/infra/bridge/tronclass"
	"catcher/internal/infra/bridge/yuketang"
	"catcher/internal/infra/secretstore"
	"catcher/internal/infra/sender"
	"catcher/internal/infra/sqlite"
	"catcher/internal/service"
	"catcher/pkg/config"
	"catcher/pkg/logger"
)

// Version 是构建版本，预发布期间写死，后续改为 ldflags 注入
const Version = "0.4.0-dev"

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

	// 每个平台一个 Sender（限速互不干扰），共享 Transport 复用连接池
	transport := http.DefaultTransport
	cipher := secretstore.New()
	ykSender := sender.New(sender.Config{
		Transport:   transport,
		MinInterval: 300 * time.Millisecond,
		Logger:      lg,
	})
	yk := yuketang.New(yuketang.Options{
		Sender: ykSender,
		Logger: lg,
		Now:    time.Now,
	})
	cxSender := sender.New(sender.Config{
		Transport:   transport,
		MinInterval: 250 * time.Millisecond,
		Logger:      lg,
	})
	cx := chaoxing.New(chaoxing.Options{
		Sender: cxSender,
		Logger: lg,
		Now:    time.Now,
	})
	tronSender := sender.New(sender.Config{
		Transport:   transport,
		MinInterval: 250 * time.Millisecond,
		Logger:      lg,
	})
	tron := tronclass.New(tronclass.Options{
		Sender: tronSender,
		Logger: lg,
		Now:    time.Now,
	})

	svc := service.New(service.Deps{
		Accounts:    repo,
		Sessions:    sqlite.NewSessionRepo(db, cipher),
		Credentials: sqlite.NewCredentialRepo(db, cipher),
		Assignments: repo,
		Runs:        sqlite.NewFetchRunRepo(db),
		Fetchers: map[entity.Platform]domain.Fetcher{
			entity.PlatformChaoxing:  cx,
			entity.PlatformYuketang:  yk,
			entity.PlatformTronclass: tron,
		},
		Renewers: map[entity.Platform]domain.SessionRenewer{
			entity.PlatformYuketang: yk,
		},
		Authenticators: map[entity.Platform]domain.PasswordAuthenticator{
			entity.PlatformChaoxing: cx,
		},
		QRLogin:  yk,
		Importer: tron,
		Config:   cfg,
		DataDir:  dataDir,
		Version:  Version,
		Logger:   lg,
		Now:      time.Now,
	})
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
			svc.CancelAll()
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
