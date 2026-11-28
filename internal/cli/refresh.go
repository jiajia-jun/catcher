package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"time"

	"catcher/internal/domain/entity"
	"catcher/internal/service"
)

type refreshCounts struct {
	CoursesTotal  int `json:"coursesTotal"`
	CoursesOK     int `json:"coursesOk"`
	ItemsSeen     int `json:"itemsSeen"`
	ItemsUpserted int `json:"itemsUpserted"`
	WarningCount  int `json:"warningCount"`
}

type refreshIssue struct {
	Scope   string `json:"scope"`
	Ref     string `json:"ref,omitempty"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
	RawPath string `json:"rawPath,omitempty"`
}

type refreshPlatform struct {
	Platform    string         `json:"platform"`
	DisplayName string         `json:"displayName"`
	Status      string         `json:"status"`
	SkipReason  string         `json:"skipReason,omitempty"`
	RunID       int64          `json:"runId,omitempty"`
	Counts      *refreshCounts `json:"counts,omitempty"`
	Issues      []refreshIssue `json:"issues"`
	Error       *ErrorBody     `json:"error,omitempty"`
}

type refreshData struct {
	JobID      string            `json:"jobId"`
	StartedAt  string            `json:"startedAt"`
	FinishedAt string            `json:"finishedAt"`
	Platforms  []refreshPlatform `json:"platforms"`
}

var refreshSeq atomic.Int64

func newRefreshJobID(at time.Time) string {
	return fmt.Sprintf("refresh-%d-%d", at.UnixMilli(), refreshSeq.Add(1))
}

func runRefresh(deps Deps, args []string) int {
	fs := newCommandFlagSet("refresh")
	var platformFlag repeatedString
	fs.Var(&platformFlag, "platform", "平台，逗号分隔，可重复")
	timeout := fs.Duration("timeout", 10*time.Minute, "总时限")
	fs.Bool("verbose", false, "日志同时写到 stderr")
	fs.String("data-dir", "", "数据目录")
	help := fs.Bool("help", false, "输出用法")
	if err := fs.Parse(args); err != nil {
		return UsageError(deps.Stderr, err.Error())
	}
	if fs.NArg() > 0 {
		return UsageError(deps.Stderr, "unexpected argument: "+fs.Arg(0))
	}
	if *help {
		return printUsage(deps.Stdout, refreshUsage)
	}
	if *timeout < 0 {
		return writeInvalidArgument(deps.Stdout, "--timeout 不能为负数")
	}

	platforms := deps.Service.EnabledPlatforms()
	if values := platformFlag.values(); len(values) > 0 {
		parsed, apiErr := parsePlatforms(values)
		if apiErr != nil {
			return writeFailure(deps.Stdout, apiErr.Code, apiErr.Message)
		}
		platforms = parsed
	}

	startedAt := deps.Now()
	baseCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	runCtx := baseCtx
	if *timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(baseCtx, *timeout)
		defer cancel()
	}

	results := deps.Service.RefreshSync(runCtx, platforms)
	finishedAt := deps.Now()

	data := refreshData{
		JobID:      newRefreshJobID(startedAt),
		StartedAt:  formatLocal(startedAt),
		FinishedAt: formatLocal(finishedAt),
		Platforms:  make([]refreshPlatform, 0, len(results)),
	}

	var firstErr *service.ErrorEvent
	hasAuth := false
	hasFailure := false
	for _, result := range results {
		view := refreshPlatform{
			Platform:    string(result.Platform),
			DisplayName: result.Platform.DisplayName(),
			Status:      string(result.Status),
			SkipReason:  result.SkipReason,
			Issues:      make([]refreshIssue, 0, len(result.Issues)),
		}
		if result.Run.ID > 0 {
			view.RunID = result.Run.ID
			view.Counts = &refreshCounts{
				CoursesTotal:  result.Run.CoursesTotal,
				CoursesOK:     result.Run.CoursesOK,
				ItemsSeen:     result.Run.ItemsSeen,
				ItemsUpserted: result.Run.ItemsUpserted,
				WarningCount:  result.Run.WarningCount,
			}
		}
		for _, issue := range result.Issues {
			view.Issues = append(view.Issues, refreshIssue{
				Scope:   string(issue.Scope),
				Ref:     issue.Ref,
				Kind:    string(issue.Kind),
				Message: redact(issue.Message),
				RawPath: issue.RawPath,
			})
		}
		if result.Error != nil {
			view.Error = &ErrorBody{Code: result.Error.Code, Message: redact(result.Error.Message)}
			if firstErr == nil {
				firstErr = result.Error
			}
			if exitForCode(result.Error.Code) == ExitAuth {
				hasAuth = true
			}
		}
		switch result.Status {
		case entity.RunFailed, entity.RunCanceled, entity.RunBusy:
			hasFailure = true
		}
		data.Platforms = append(data.Platforms, view)
	}

	exit := ExitOK
	var overall *ErrorBody
	switch {
	case hasAuth:
		exit = ExitAuth
		overall = errorBodyOf(firstErr)
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		exit = ExitFailure
		overall = &ErrorBody{Code: "TIMEOUT", Message: "刷新超时"}
	case errors.Is(runCtx.Err(), context.Canceled):
		exit = ExitFailure
		overall = &ErrorBody{Code: "CANCELED", Message: "已取消"}
	case hasFailure || firstErr != nil:
		exit = ExitFailure
		overall = errorBodyOf(firstErr)
	}

	env := Envelope{OK: exit == ExitOK, Data: data}
	if overall != nil {
		env.Error = overall
	}
	if code := writeJSON(deps.Stdout, env); code != ExitOK && exit == ExitOK {
		return code
	}
	return exit
}

func errorBodyOf(ev *service.ErrorEvent) *ErrorBody {
	if ev == nil {
		return &ErrorBody{Code: "INTERNAL", Message: "刷新失败"}
	}
	return &ErrorBody{Code: ev.Code, Message: redact(ev.Message)}
}
