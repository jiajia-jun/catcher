package cli

import (
	"context"

	"catcher/internal/domain/entity"
	"catcher/internal/dto"
	"catcher/internal/service"
)

type lastRunView struct {
	ID         int64  `json:"id"`
	Status     string `json:"status"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt,omitempty"`
}

type statusPlatform struct {
	Platform     string         `json:"platform"`
	DisplayName  string         `json:"displayName"`
	LoginState   string         `json:"loginState"`
	Experimental bool           `json:"experimental"`
	Counts       dto.CountsView `json:"counts"`
	LastRun      *lastRunView   `json:"lastRun,omitempty"`
}

type statusData struct {
	Version     string           `json:"version"`
	DataDir     string           `json:"dataDir"`
	GeneratedAt string           `json:"generatedAt"`
	Platforms   []statusPlatform `json:"platforms"`
}

func runStatus(deps Deps, args []string) int {
	fs := newCommandFlagSet("status")
	fs.String("data-dir", "", "数据目录")
	help := fs.Bool("help", false, "输出用法")
	if err := fs.Parse(args); err != nil {
		return UsageError(deps.Stderr, err.Error())
	}
	if fs.NArg() > 0 {
		return UsageError(deps.Stderr, "unexpected argument: "+fs.Arg(0))
	}
	if *help {
		return printUsage(deps.Stdout, statusUsage)
	}

	bootstrap, err := deps.Service.Bootstrap(context.Background(), true)
	if err != nil {
		return writeFailure(deps.Stdout, "STORAGE", "读取应用状态失败: "+err.Error())
	}

	accounts := make(map[entity.Platform]entity.Account, len(bootstrap.Accounts))
	for _, account := range bootstrap.Accounts {
		accounts[account.Platform] = account
	}

	data := statusData{
		Version:     nonEmpty(deps.Service.Version(), deps.Version, bootstrap.Version),
		DataDir:     nonEmpty(deps.Service.DataDir(), deps.DataDir, bootstrap.DataDir),
		GeneratedAt: formatLocal(deps.Now()),
		Platforms:   make([]statusPlatform, 0, len(entity.AllPlatforms())),
	}
	for _, p := range entity.AllPlatforms() {
		view := statusPlatform{
			Platform:     string(p),
			DisplayName:  p.DisplayName(),
			LoginState:   string(entity.LoginDisconnected),
			Experimental: p.Experimental(),
			Counts:       countsView(bootstrap.Counts[p]),
		}
		if account, ok := accounts[p]; ok {
			view.LoginState = string(account.LoginState)
		}
		if run, ok := bootstrap.LastRuns[p]; ok {
			last := &lastRunView{
				ID:        run.ID,
				Status:    string(run.Status),
				StartedAt: formatLocal(run.StartedAt),
			}
			if run.FinishedAt != nil {
				last.FinishedAt = formatLocal(*run.FinishedAt)
			}
			view.LastRun = last
		}
		data.Platforms = append(data.Platforms, view)
	}
	return writeData(deps.Stdout, data)
}

func countsView(c service.Counts) dto.CountsView {
	return dto.CountsView{
		OpenUnsubmitted:    c.OpenUnsubmitted,
		OverdueUnsubmitted: c.OverdueUnsubmitted,
		Unknown:            c.Unknown,
		Submitted:          c.Submitted,
	}
}

func nonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
