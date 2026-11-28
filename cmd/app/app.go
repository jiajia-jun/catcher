// Package app 负责装配：数据目录 → 配置 → logger → sqlite → repository
// → service → handle → wails.Run，或交给命令行适配层
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
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"catcher/internal/cli"
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

// Entry 是首参决定的入口形态
type Entry int

const (
	EntryGUI Entry = iota
	EntryCLI
	EntryUsage
)

// Dispatch 按首参选择入口：已知子命令走命令行，无参数或 --data-dir 走界面
func Dispatch(args []string) Entry {
	if len(args) == 0 {
		return EntryGUI
	}
	switch args[0] {
	case "refresh", "list", "status", "mark", "version", "help", "--help", "-h", "--version":
		return EntryCLI
	case "--data-dir":
		return EntryGUI
	}
	if strings.HasPrefix(args[0], "--data-dir=") {
		return EntryGUI
	}
	return EntryUsage
}

// Runtime 是一次装配的产物，桌面版与命令行共用
type Runtime struct {
	Config   *config.App
	Logger   logger.Logger
	Service  *service.Service
	DataDir  string
	FirstRun bool
	DevMode  bool
	Close    func()
}

// Open 完成数据目录 → 配置 → logger → sqlite → 迁移 → 仓储 → service
// args 交给 config.ResolveDataDir 解析 --data-dir
func Open(args []string, logFile string) (*Runtime, error) {
	opts := config.DefaultDataDirOptions()
	opts.Args = args
	return openWith(opts, logFile, opts.DevMode)
}

func openWith(opts config.DataDirOptions, logFile string, console bool) (*Runtime, error) {
	ctx := context.Background()

	dataDir, err := config.ResolveDataDir(opts)
	if err != nil {
		return nil, err
	}

	// 配置文件是否存在决定「首次安装」：老版本升级时字段为空，但配置已存在
	cfgPath := filepath.Join(dataDir, config.FileName+"."+config.FileExt)
	firstRun := false
	if _, statErr := os.Stat(cfgPath); errors.Is(statErr, fs.ErrNotExist) {
		firstRun = true
	}

	cfg, err := config.LoadOrCreate(dataDir)
	if err != nil {
		return nil, err
	}

	lg, logCloser, err := logger.New(logger.Options{
		Dir:        filepath.Join(dataDir, "logs"),
		FileName:   logFile,
		Level:      cfg.Log.Level,
		MaxSizeMB:  cfg.Log.MaxSizeMB,
		MaxBackups: cfg.Log.MaxBackups,
		MaxAgeDays: cfg.Log.MaxAgeDays,
		Console:    console,
	})
	if err != nil {
		return nil, err
	}

	db, err := sqlite.Open(ctx, filepath.Join(dataDir, "catcher.db"))
	if err != nil {
		_ = logCloser.Close()
		return nil, err
	}

	closeAll := func() {
		_ = db.Close()
		_ = logCloser.Close()
	}

	if err := db.Migrate(ctx); err != nil {
		closeAll()
		return nil, err
	}

	repo := sqlite.NewRepo(db, sqlite.WithLogger(lg))
	if err := repo.EnsurePlatforms(ctx, time.Now()); err != nil {
		closeAll()
		return nil, err
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
		Locker:      newPlatformLocker(dataDir),
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
		DevMode:  opts.DevMode,
		Logger:   lg,
		Now:      time.Now,
	})

	var once sync.Once
	closeRuntime := func() {
		once.Do(func() {
			lg.Info("应用退出")
			svc.CancelAll()
			closeAll()
		})
	}

	return &Runtime{
		Config:   cfg,
		Logger:   lg,
		Service:  svc,
		DataDir:  dataDir,
		FirstRun: firstRun,
		DevMode:  opts.DevMode,
		Close:    closeRuntime,
	}, nil
}

// RunCLI 装配数据、执行子命令并返回退出码
func RunCLI(args []string) int {
	attachParentConsole()
	dirArgs, err := extractDataDirArgs(args)
	if err != nil {
		return cli.UsageError(os.Stderr, err.Error())
	}

	opts := config.DefaultDataDirOptions()
	opts.Args = dirArgs
	rt, err := openWith(opts, "catcher-cli.log", opts.DevMode || hasVerbose(args))
	if err != nil {
		return cli.ReportFatal(os.Stdout, err)
	}
	defer rt.Close()

	rt.Logger.Info("命令行启动", "version", Version, "data_dir", rt.DataDir)
	code := cli.Execute(args, cli.Deps{
		Service: rt.Service,
		Logger:  rt.Logger,
		DataDir: rt.DataDir,
		Version: Version,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Now:     time.Now,
	})
	rt.Logger.Info("命令行结束", "exit_code", code)
	return code
}

// UsageError 输出用法错误并以 2 退出
func UsageError(args []string) int {
	attachParentConsole()
	if len(args) == 0 {
		return cli.UsageError(os.Stderr, "missing command")
	}
	return cli.UsageError(os.Stderr, fmt.Sprintf("unrecognized argument %q", args[0]))
}

// extractDataDirArgs 只保留 --data-dir，供 Open 解析；其余参数留给子命令
func extractDataDirArgs(args []string) ([]string, error) {
	out := make([]string, 0, 2)
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--data-dir":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--data-dir requires a path")
			}
			out = append(out, "--data-dir", args[i+1])
			i++
		case strings.HasPrefix(a, "--data-dir="):
			out = append(out, a)
		}
	}
	return out, nil
}

func hasVerbose(args []string) bool {
	for _, a := range args {
		switch a {
		case "--verbose", "-verbose", "--verbose=true", "-verbose=true":
			return true
		}
	}
	return false
}

// Run 启动 Wails 应用，阻塞到窗口关闭
func Run(assets fs.FS) error {
	rt, err := Open(os.Args[1:], "catcher.log")
	if err != nil {
		return err
	}
	defer rt.Close()

	h := handle.New(rt.Service)

	rt.Logger.Info("应用启动",
		"version", Version,
		"data_dir", rt.DataDir,
		"log_level", rt.Config.Log.Level,
	)

	wailsErr := wails.Run(&options.App{
		Title:     "Catcher",
		Width:     1180,
		Height:    780,
		MinWidth:  960,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: windowBackground(rt.Config.UI.Theme),
		Windows: &windows.Options{
			// 原生标题栏跟随 ui.theme，否则深色页面会顶一条白色横栏
			Theme: windowTheme(rt.Config.UI.Theme),
		},
		OnStartup: h.Startup,
		OnShutdown: func(context.Context) {
			rt.Close()
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
