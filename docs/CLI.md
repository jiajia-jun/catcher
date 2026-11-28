# 命令行调用

1.6.0 起 `Catcher.exe` 同时是命令行入口：`refresh`、`list`、`status`、`mark`、`version`、`help` 走命令行，无参数或只带 `--data-dir` 时打开桌面窗口

命令行与桌面版共用同一份数据目录、同一个数据库和同一套平台适配器；登录（扫码、密码、session 导入）仍然只在界面完成，命令行只报告哪个平台需要重新登录

## 入口与分发

| 第一个参数 | 行为 |
| --- | --- |
| `refresh` / `list` / `status` / `mark` / `version` / `help` / `--help` / `-h` / `--version` | 命令行 |
| 无参数 | 打开界面 |
| `--data-dir` 或 `--data-dir=...` | 打开界面 |
| 其他值（含其他 `-` 开头的参数） | 用法错误，退出 2 |

命令行的 `--data-dir` 写在命令之后：

```text
Catcher.exe list --data-dir D:\data
```

环境变量 `CATCHER_DATA_DIR` 对两种入口都生效

## Agent 接入

安装目录附带 `AGENTS.md`，内容是一份可以直接交给 Agent 的说明，覆盖：

- 先查 `status`，只在 `connected` 时刷新
- 检查 `refresh` 的 `platforms[].status` 与退出码
- 用 `list` 的 `id` 标记作业
- 不读取凭据、不尝试登录

便携包的 zip 根目录同样包含该文件

## 通用参数

| 参数 | 适用命令 | 说明 |
| --- | --- | --- |
| `--data-dir <路径>` | 全部 | 数据目录，等价于界面版 |
| `--help` | 全部 | 输出该命令用法 |
| `--verbose` | `refresh` | 日志同时写到 stderr；默认 stderr 只输出致命错误 |

stdout 只放 JSON，`help` 是唯一例外；stderr 只放诊断信息；命令行日志独立写入 `data/logs/catcher-cli.log`，滚动参数与桌面版一致

## 输出信封

成功：

```json
{ "ok": true, "data": {} }
```

失败：

```json
{ "ok": false, "error": { "code": "AUTH_EXPIRED", "message": "登录已失效" }, "data": {} }
```

所有 `message` 与 `issues[].message` 都经过与日志相同的 URL 查询串打码规则，凭据类字段不会出现在输出里

## 退出码

| 退出码 | 含义 |
| --- | --- |
| 0 | 成功，含空结果、`partial`、`skipped` |
| 1 | 失败：网络、解析、业务、超时、取消、存储错误 |
| 2 | 用法错误 |
| 3 | 有平台需要重新登录 |

优先级为 `3 > 1 > 0`；一次刷新里既有登录失效又有网络失败时退出 3，具体看 `data.platforms[]`

## refresh

```text
Catcher.exe refresh [--platform <平台[,平台]>] [--timeout <时长>] [--verbose] [--data-dir <路径>]
```

- 不传 `--platform` 时使用 `catcher.yaml` 的 `ui.enabled_platforms`
- 从未登录的平台标 `skipped`，不发起请求，不计失败
- 登录过但失效时该平台失败，整体退出 3
- 同一平台正被其他进程刷新时标 `busy`，按失败处理
- `--timeout` 是整个命令的总时限，默认 10 分钟，`0` 表示不限
- 收到 Ctrl+C 时取消在途请求、释放锁，按 `canceled` 输出并退出 1

各平台并发刷新，互不影响；成功输出：

```json
{
  "ok": true,
  "data": {
    "jobId": "refresh-1764300000000-1",
    "startedAt": "2026-11-28T19:00:00+08:00",
    "finishedAt": "2026-11-28T19:00:42+08:00",
    "platforms": [
      {
        "platform": "chaoxing",
        "displayName": "学习通",
        "status": "success",
        "runId": 42,
        "counts": {
          "coursesTotal": 13,
          "coursesOk": 13,
          "itemsSeen": 19,
          "itemsUpserted": 19,
          "warningCount": 0
        },
        "issues": []
      },
      {
        "platform": "pigai",
        "displayName": "批改网",
        "status": "skipped",
        "skipReason": "not_logged_in",
        "issues": []
      }
    ]
  }
}
```

失败时 `ok=false`，`error.code` 取首个失败平台的错误码，`data` 仍然完整返回；`status` 取值：`success`、`partial`、`failed`、`canceled`、`skipped`、`busy`

## list

```text
Catcher.exe list [--platform <平台[,平台]>] [--status <状态[,状态]>]
                 [--show-submitted] [--all-terms] [--keyword <文本>] [--data-dir <路径>]
```

- 默认只看本学期（9 月 1 日起，东八区），`--all-terms` 关闭
- 默认隐藏已提交作业，`--show-submitted` 包含
- `--status` 给出时按有效状态精确匹配，覆盖隐藏规则；可取 `submitted`、`unsubmitted`、`unknown`
- `--keyword` 匹配标题或课程名
- 不传 `--platform` 时查询全部平台
- 截止时间未知的作业始终保留，与界面一致

```json
{
  "ok": true,
  "data": {
    "total": 1,
    "items": [
      {
        "id": 123,
        "platform": "chaoxing",
        "courseName": "计算机组成原理A",
        "title": "实验三",
        "status": "unsubmitted",
        "platformStatus": "unsubmitted",
        "manualStatus": "",
        "phase": "open",
        "deadlineAt": "2026-12-01T15:59:00Z",
        "deadlineLocal": "2026-12-01 23:59",
        "detailUrl": "https://mooc1.chaoxing.com/...",
        "firstSeenAt": "2026-11-20T02:00:00Z",
        "lastSeenAt": "2026-11-28T11:00:00Z"
      }
    ]
  }
}
```

`lastSeenAt` 即最近一次抓取时间；作业标识用 `id`，刷新不会改变该值；实际输出还可能包含 `courseExternalId`、`deadlineSource`、`deadlineRaw`、`manualStatusAt`

## status

```text
Catcher.exe status [--data-dir <路径>]
```

只读数据库与配置，不发网络请求；登录态取值：`connected`、`needs_login`、`disconnected`、`error`

```json
{
  "ok": true,
  "data": {
    "version": "1.6.0",
    "dataDir": "D:\\Apps\\Catcher\\data",
    "generatedAt": "2026-11-28T19:05:00+08:00",
    "platforms": [
      {
        "platform": "chaoxing",
        "displayName": "学习通",
        "loginState": "connected",
        "experimental": false,
        "counts": { "openUnsubmitted": 2, "overdueUnsubmitted": 0, "unknown": 1, "submitted": 5 },
        "lastRun": {
          "id": 42,
          "status": "success",
          "startedAt": "2026-11-28T19:00:00+08:00",
          "finishedAt": "2026-11-28T19:00:42+08:00"
        }
      }
    ]
  }
}
```

## mark

```text
Catcher.exe mark <id> --status <submitted|unsubmitted|clear> [--data-dir <路径>]
```

- `submitted` / `unsubmitted` 写入手动状态；`clear` 清除手动值，恢复平台值
- `id` 来自 `list` 或 `refresh` 的输出
- 未知或已下架的 id 返回 `NOT_FOUND`，退出 1
- 缺少 id、状态非法返回 `INVALID_ARGUMENT`，退出 1

```json
{ "ok": true, "data": { "item": { "id": 123, "status": "submitted", "manualStatus": "submitted" } } }
```

输出结构同 `list` 的 `items[]`

## version / help

```json
{ "ok": true, "data": { "version": "1.6.0" } }
```

`help` 把纯文本用法写到 stdout，退出 0；未知命令的提示写到 stderr，退出 2

## 错误码

沿用界面的错误码，并补充命令行专用项：

| 错误码 | 场景 |
| --- | --- |
| `AUTH_REQUIRED` | 尚未登录 |
| `AUTH_EXPIRED` / `AUTH_FAILED` | 登录态失效 |
| `NETWORK` | 网络不可达、超时 |
| `BUSINESS` | 平台返回业务错误 |
| `PLATFORM_CHANGED` | 页面结构变化、解析失败 |
| `CANCELED` | 用户取消 |
| `TIMEOUT` | 达到 `--timeout` |
| `PLATFORM_BUSY` | 该平台正被其他进程刷新 |
| `REFRESH_RUNNING` | 进程内已有同名平台任务 |
| `NOT_FOUND` | 作业 id 不存在或已下架 |
| `INVALID_ARGUMENT` | 参数非法 |
| `STORAGE` | 数据库或数据目录读写失败 |
| `INTERNAL` | 未分类错误 |

## 并发与锁

- 每个平台一把跨进程文件锁，位于 `data/locks/<platform>.lock`
- 桌面版与命令行同时刷新同一平台时，后到的一方该平台标 `busy`
- 刷新不同平台互不影响
- 数据库迁移单独使用 `data/locks/migrate.lock`，另一进程迁移最多等 30 秒
- 进程退出（含崩溃）后操作系统自动释放锁，不会留下死锁

## 调用示例

### cmd

```bat
Catcher.exe refresh --platform chaoxing,yuketang
if errorlevel 3 (
  echo 需要重新登录，请打开界面
)
for /f "delims=" %%i in ('Catcher.exe list --status unsubmitted') do set RESULT=%%i
```

### PowerShell

PowerShell 直接调用 GUI 子系统程序时不会等待，输出会与提示符交错；请改用 `cmd /c` 或 `Start-Process -Wait`：

```powershell
$json = cmd /c "Catcher.exe list --status unsubmitted"
$result = $json | ConvertFrom-Json
if (-not $result.ok) { Write-Error $result.error.message }

# 或者等待进程并读取退出码
$p = Start-Process -FilePath .\Catcher.exe -ArgumentList 'refresh','--platform','chaoxing' -Wait -PassThru
$p.ExitCode
```

### Node.js

```js
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'

const run = promisify(execFile)
const { stdout } = await run('Catcher.exe', ['list', '--status', 'unsubmitted'])
const { ok, data, error } = JSON.parse(stdout)
if (!ok) throw new Error(error.message)
console.log(data.items)
```

### Python

```python
import json
import subprocess

proc = subprocess.run(
    ["Catcher.exe", "refresh", "--platform", "chaoxing"],
    capture_output=True,
    text=True,
    encoding="utf-8",
)
payload = json.loads(proc.stdout)
if proc.returncode == 3:
    print("需要重新登录")
print(payload["data"]["platforms"])
```

## 已知限制

- **命令行不处理登录**：登录态维护回到界面，这是设计选择
- **PowerShell 不等待**：见上文，改用 `cmd /c` 或 `Start-Process -Wait`
- **界面回读只在窗口获得焦点时发生**：CLI 刷新后切回窗口会重新拉取；窗口长时间保持焦点时仍可能显示旧数据，可手动刷新
- **写锁等待**：大快照写入若超过 5 秒，另一进程写入可能报 `STORAGE`
- **定时刷新由外部调度**：程序内不内置定时器，可用系统任务计划或调度器触发 `refresh`
