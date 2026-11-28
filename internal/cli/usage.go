package cli

import (
	"fmt"
	"io"
)

const generalUsage = `Catcher 命令行

用法:
  Catcher.exe <命令> [参数]

命令:
  refresh   刷新平台作业并等待结束
  list      查询作业
  status    查看登录态、最近刷新与统计（不发网络请求）
  mark      手动标记作业状态
  version   输出程序版本
  help      输出本说明

参数:
  --data-dir <路径>   数据目录，默认与桌面版一致
  --help              输出命令用法

输出:
  stdout 只输出 JSON，help 除外；stderr 只放诊断信息
  退出码 0 成功、1 失败、2 用法错误、3 需要重新登录

示例:
  Catcher.exe refresh --platform chaoxing
  Catcher.exe list --show-submitted --keyword 实验
  Catcher.exe mark 123 --status submitted

PowerShell 直接调用 GUI 程序时不会等待，请改用 cmd /c 或 Start-Process -Wait
`

const refreshUsage = `用法:
  Catcher.exe refresh [--platform <平台[,平台]>] [--timeout <时长>] [--verbose] [--data-dir <路径>]

参数:
  --platform   要刷新的平台，逗号分隔或重复给出；默认使用「参与刷新」勾选的平台
  --timeout    整个命令的总时限，默认 10m，0 表示不限
  --verbose    日志同时写到 stderr

说明:
  从未登录的平台标 skipped，不计失败；登录态失效或网络失败退出 1/3
`

const listUsage = `用法:
  Catcher.exe list [--platform <平台[,平台]>] [--status <状态[,状态]>]
                   [--show-submitted] [--all-terms] [--keyword <文本>] [--data-dir <路径>]

参数:
  --platform        只看指定平台，默认全部
  --status          submitted、unsubmitted、unknown，按有效状态精确匹配
  --show-submitted  包含已提交作业
  --all-terms       不限制本学期
  --keyword         匹配标题或课程名

说明:
  默认只看本学期（9 月 1 日起）并隐藏已提交作业；截止时间未知的作业始终保留
`

const statusUsage = `用法:
  Catcher.exe status [--data-dir <路径>]

只读取数据库与配置，不发起网络请求
`

const markUsage = `用法:
  Catcher.exe mark <id> --status <submitted|unsubmitted|clear> [--data-dir <路径>]

参数:
  id       作业 id，来自 list 或 refresh 的输出
  --status submitted / unsubmitted 写入手动状态，clear 恢复平台值

说明:
  id 不存在或已下架返回 NOT_FOUND
`

const versionUsage = `用法:
  Catcher.exe version [--data-dir <路径>]

输出程序版本
`

// UsageError 把用法错误写到 stderr，退出 2
func UsageError(w io.Writer, message string) int {
	fmt.Fprintf(w, "用法错误: %s\n\n%s", message, generalUsage)
	return ExitUsage
}

func printUsage(w io.Writer, text string) int {
	fmt.Fprint(w, text)
	return ExitOK
}
