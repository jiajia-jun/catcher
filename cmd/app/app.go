// Package app 负责装配：数据目录 → 配置 → logger → sqlite → repository
// → service → handle → wails.Run
//
// 本包由根 main.go 调用
// Wails v2 要求入口位于仓库根目录，根 main.go 在那里完成 frontend/dist 的 embed
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"catcher/internal/domain"
	"catcher/internal/domain/entity"
	"catcher/internal/handle"
	"catcher/internal/infra/bridge/chaoxing"
	"catcher/internal/infra/bridge/fiforal"
	"catcher/internal/infra/bridge/pigai"
	"catcher/internal/infra/bridge/tronclass"
	"catcher/internal/infra/bridge/yuketang"
	"catcher/internal/infra/secretstore"
	"catcher/internal/infra/sender"
	"catcher/internal/infra/sqlite"
	"catcher/internal/infra/updater"
	"catcher/internal/service"
	"catcher/pkg/config"
	"catcher/pkg/logger"
)

// Version 是构建版本，预发布期间写死，后续改为 ldflags 注入
const Version = "1.5.1"

// Run 启动 Wails 应用，阻塞到窗口关闭
func Run(assets fs.FS) error {
	ctx := context.Background()
	dataDirOpts := config.DefaultDataDirOptions()

	dataDir, err := config.ResolveDataDir(dataDirOpts)
	if err != nil {
		return err
	}

	// 配置文件是否存在决定「首次安装」：老版本升级时字段为空，但配置已存在
	cfgPath := filepath.Join(dataDir, config.FileName+"."+config.FileExt)
	firstRun := false
	if _, statErr := os.Stat(cfgPath); errors.Is(statErr, fs.ErrNotExist) {
		firstRun = true
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

	repo := sqlite.NewRepo(db, sqlite.WithLogger(lg))
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
	pigSender := sender.New(sender.Config{
		Transport:   transport,
		MinInterval: 250 * time.Millisecond,
		Logger:      lg,
	})
	pig := pigai.New(pigai.Options{
		Sender: pigSender,
		Logger: lg,
		Now:    time.Now,
	})
	// FiF 登录链是十几跳串行 302，每跳本身只有几十到一百毫秒，
	// 250ms 的间隔会把确认后的等待拉长约两秒；抓取请求量也不大，100ms 足够
	fifSender := sender.New(sender.Config{
		Transport:   transport,
		MinInterval: 100 * time.Millisecond,
		Logger:      lg,
	})
	fif := fiforal.New(fiforal.Options{
		Sender: fifSender,
		Logger: lg,
		Now:    time.Now,
	})
	tron := tronclass.New(tronclass.Options{
		Sender: tronSender,
		Logger: lg,
		Now:    time.Now,
	})

	// 更新检查只读取 GitHub Releases 的版本信息
	releases := updater.New(updater.Options{
		BaseURL: updater.DefaultBaseURL,
		Owner:   updater.DefaultOwner,
		Repo:    updater.DefaultRepo,
		Current: Version,
		Logger:  lg,
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
			entity.PlatformPigai:     pig,
			entity.PlatformFiforal:   fif,
		},
		Renewers: map[entity.Platform]domain.SessionRenewer{
			entity.PlatformYuketang: yk,
		},
		Authenticators: map[entity.Platform]domain.PasswordAuthenticator{
			entity.PlatformChaoxing: cx,
		},
		QRLogins: map[entity.Platform]domain.QRLogin{
			entity.PlatformYuketang: yk,
			entity.PlatformChaoxing: cx,
			entity.PlatformPigai:    pig,
			entity.PlatformFiforal:  fif,
		},
		Importer: tron,
		Releases: releases,
		Config:   cfg,
		DataDir:  dataDir,
		Version:  Version,
		FirstRun: firstRun,
		DevMode:  dataDirOpts.DevMode,
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
		BackgroundColour: windowBackground(cfg.UI.Theme),
		Windows: &windows.Options{
			// 原生标题栏跟随 ui.theme，否则深色页面会顶一条白色横栏
			Theme: windowTheme(cfg.UI.Theme),
		},
		OnStartup: h.Startup,
		OnShutdown: func(context.Context) {
			shutdown()
		},
		Bind: []interface{}{h},
	})
	if wailsErr != nil {
		return fmt.Errorf("启动 Wails 窗口失败: %w", wailsErr)
	}
	return nil
}

// windowTheme 把 ui.theme 映射为原生窗口主题，auto 交给系统
func windowTheme(mode string) windows.Theme {
	switch mode {
	case "dark":
		return windows.Dark
	case "light":
		return windows.Light
	default:
		return windows.SystemDefault
	}
}

// windowBackground 是 WebView2 首帧绘制前的底色，深色主题下避免启动闪白
func windowBackground(mode string) *options.RGBA {
	if mode == "dark" {
		return &options.RGBA{R: 13, G: 17, B: 23, A: 1}
	}
	return &options.RGBA{R: 248, G: 250, B: 252, A: 1}
}
