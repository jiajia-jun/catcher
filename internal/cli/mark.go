package cli

import (
	"context"
	"errors"
	"strconv"

	"catcher/internal/domain/entity"
	"catcher/internal/dto"
)

type markData struct {
	Item dto.AssignmentView `json:"item"`
}

func runMark(deps Deps, args []string) int {
	if len(args) == 0 {
		return writeInvalidArgument(deps.Stdout, "缺少作业 id")
	}
	if args[0] == "--help" || args[0] == "-h" {
		return printUsage(deps.Stdout, markUsage)
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		return writeInvalidArgument(deps.Stdout, "作业 id 无效: "+args[0])
	}

	fs := newCommandFlagSet("mark")
	status := fs.String("status", "", "submitted、unsubmitted 或 clear")
	fs.String("data-dir", "", "数据目录")
	help := fs.Bool("help", false, "输出用法")
	if err := fs.Parse(args[1:]); err != nil {
		return UsageError(deps.Stderr, err.Error())
	}
	if fs.NArg() > 0 {
		return UsageError(deps.Stderr, "unexpected argument: "+fs.Arg(0))
	}
	if *help {
		return printUsage(deps.Stdout, markUsage)
	}

	var target *entity.SubmissionStatus
	switch *status {
	case "submitted", "unsubmitted":
		value := entity.SubmissionStatus(*status)
		target = &value
	case "clear":
		target = nil
	case "":
		return writeInvalidArgument(deps.Stdout, "缺少 --status")
	default:
		return writeInvalidArgument(deps.Stdout, "状态只能是 submitted、unsubmitted 或 clear")
	}

	item, err := deps.Service.SetAssignmentStatus(context.Background(), id, target)
	if err != nil {
		if errors.Is(err, entity.ErrNotFound) {
			return writeFailure(deps.Stdout, "NOT_FOUND", "作业不存在或已下架")
		}
		return writeFailure(deps.Stdout, "STORAGE", "更新作业状态失败: "+err.Error())
	}
	return writeData(deps.Stdout, markData{Item: dto.AssignmentViewOf(item, deps.Now())})
}
