// Package cli 是命令行交付适配层：参数解析、调用 service、输出 JSON 与退出码
//
// 与 handle 平级，只依赖 service 与 dto
package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"catcher/internal/domain/entity"
)

// Execute 分发子命令并返回退出码
func Execute(args []string, deps Deps) int {
	deps = deps.withDefaults()
	if len(args) == 0 {
		return UsageError(deps.Stderr, "missing command")
	}

	command, rest := args[0], args[1:]
	switch command {
	case "refresh":
		return runRefresh(deps, rest)
	case "list":
		return runList(deps, rest)
	case "status":
		return runStatus(deps, rest)
	case "mark":
		return runMark(deps, rest)
	case "version", "--version":
		return runVersion(deps, rest)
	case "help", "--help", "-h":
		return printUsage(deps.Stdout, generalUsage)
	default:
		return UsageError(deps.Stderr, fmt.Sprintf("unknown command %q", command))
	}
}

// newCommandFlagSet 关闭默认输出，解析失败由调用方统一按用法错误处理
func newCommandFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// repeatedString 收集逗号分隔且可重复给出的参数
type repeatedString []string

func (r *repeatedString) String() string { return strings.Join(*r, ",") }

func (r *repeatedString) Set(value string) error {
	*r = append(*r, value)
	return nil
}

// values 按出现顺序展开、去空白并按首次出现去重
func (r repeatedString) values() []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(r))
	for _, raw := range r {
		for _, part := range strings.Split(raw, ",") {
			value := strings.TrimSpace(part)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func parsePlatforms(values []string) ([]entity.Platform, *ErrorBody) {
	out := make([]entity.Platform, 0, len(values))
	for _, raw := range values {
		p, err := entity.ParsePlatform(raw)
		if err != nil {
			return nil, &ErrorBody{Code: "INVALID_ARGUMENT", Message: "平台参数无效: " + err.Error()}
		}
		out = append(out, p)
	}
	return out, nil
}

func parseStatuses(values []string) ([]entity.SubmissionStatus, *ErrorBody) {
	out := make([]entity.SubmissionStatus, 0, len(values))
	for _, raw := range values {
		status := entity.SubmissionStatus(raw)
		if !status.Valid() {
			return nil, &ErrorBody{Code: "INVALID_ARGUMENT", Message: "状态参数无效: " + raw}
		}
		out = append(out, status)
	}
	return out, nil
}

// writeInvalidArgument 输出参数错误信封并退出 1
func writeInvalidArgument(w io.Writer, message string) int {
	return writeFailure(w, "INVALID_ARGUMENT", message)
}
