package cli

import (
	"context"
	"strings"

	"catcher/internal/domain/entity"
	"catcher/internal/dto"
)

type listData struct {
	Total int                  `json:"total"`
	Items []dto.AssignmentView `json:"items"`
}

func runList(deps Deps, args []string) int {
	fs := newCommandFlagSet("list")
	var platformFlag repeatedString
	var statusFlag repeatedString
	fs.Var(&platformFlag, "platform", "平台，逗号分隔，可重复")
	fs.Var(&statusFlag, "status", "状态，逗号分隔，可重复")
	showSubmitted := fs.Bool("show-submitted", false, "包含已提交作业")
	allTerms := fs.Bool("all-terms", false, "不限制本学期")
	keyword := fs.String("keyword", "", "匹配标题或课程名")
	fs.String("data-dir", "", "数据目录")
	help := fs.Bool("help", false, "输出用法")
	if err := fs.Parse(args); err != nil {
		return UsageError(deps.Stderr, err.Error())
	}
	if fs.NArg() > 0 {
		return UsageError(deps.Stderr, "unexpected argument: "+fs.Arg(0))
	}
	if *help {
		return printUsage(deps.Stdout, listUsage)
	}

	filter := entity.AssignmentFilter{
		ShowSubmitted: *showSubmitted,
		Keyword:       strings.TrimSpace(*keyword),
		TermOnly:      !*allTerms,
	}

	if values := platformFlag.values(); len(values) > 0 {
		platforms, apiErr := parsePlatforms(values)
		if apiErr != nil {
			return writeFailure(deps.Stdout, apiErr.Code, apiErr.Message)
		}
		filter.Platforms = platforms
	}
	if values := statusFlag.values(); len(values) > 0 {
		statuses, apiErr := parseStatuses(values)
		if apiErr != nil {
			return writeFailure(deps.Stdout, apiErr.Code, apiErr.Message)
		}
		filter.Statuses = statuses
	}

	items, err := deps.Service.ListAssignments(context.Background(), filter)
	if err != nil {
		return writeFailure(deps.Stdout, "STORAGE", "查询作业失败: "+err.Error())
	}

	now := deps.Now()
	views := make([]dto.AssignmentView, 0, len(items))
	for _, item := range items {
		views = append(views, dto.AssignmentViewOf(item, now))
	}
	return writeData(deps.Stdout, listData{Total: len(views), Items: views})
}
