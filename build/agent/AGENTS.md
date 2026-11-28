# Catcher Agent 说明

本文件与 `Catcher.exe` 在同一个目录
把本文件交给支持本地命令的 Agent，或让 Agent 读取本文件

## 目标

通过 `Catcher.exe` 查看、刷新与标记本机作业
所有数据只在本机读写，不经过任何网络接口

## 开始前

1. 运行 `Catcher.exe status`
2. 读取 `data.platforms[].loginState`
3. 只有 `connected` 的平台可以刷新
4. `needs_login` 或 `disconnected` 时提示用户打开 Catcher 界面完成登录
5. 不要尝试登录、导入 session 或绕过平台风控

## 命令

### 状态

```text
Catcher.exe status
```

只读数据库与配置，不发网络请求

### 刷新

```text
Catcher.exe refresh [--platform chaoxing,yuketang] [--timeout 10m]
```

- 不传 `--platform` 时刷新「参与刷新」勾选的平台
- 同步等待，默认总时限 10 分钟，子进程超时要大于它
- 从未登录的平台返回 `skipped`，不是失败
- 退出码 3 表示有平台登录失效

### 查询

```text
Catcher.exe list [--status unsubmitted] [--platform chaoxing] [--keyword 文本]
                 [--show-submitted] [--all-terms]
```

- 默认只看本学期并隐藏已提交
- 用 `items[].id` 标识作业，刷新不会改变 id
- `deadlineAt` 是 UTC，`deadlineLocal` 是东八区
- `detailUrl` 可以为空

### 标记

```text
Catcher.exe mark <id> --status submitted|unsubmitted|clear
```

- `submitted` / `unsubmitted` 写入用户手动状态
- `clear` 恢复平台值
- 未知或已下架的 id 返回 `NOT_FOUND`

### 版本

```text
Catcher.exe version
```

## 输出

stdout 只输出 JSON

成功：

```json
{ "ok": true, "data": {} }
```

失败：

```json
{ "ok": false, "error": { "code": "AUTH_EXPIRED", "message": "登录已失效" }, "data": {} }
```

stderr 只放诊断日志，不要解析

## 退出码

| 退出码 | 含义 |
| --- | --- |
| 0 | 成功，含空结果、partial、skipped |
| 1 | 失败 |
| 2 | 用法错误 |
| 3 | 需要重新登录 |

## 数据目录

默认是 `Catcher.exe` 同级的 `data/`
用户用 `--data-dir` 或 `CATCHER_DATA_DIR` 指定过目录时，所有命令都要带同一个目录

## 安全

- 不要读取、输出或拼接 Cookie、token、密码
- 不要执行数据库备份、迁移或删除操作
- 不要在用户未确认时批量 `mark`
- 不要猜测作业 id，id 必须来自 `list` 或 `refresh`

## 常见任务

用户说「看看还有什么作业没交」：

1. `Catcher.exe list --status unsubmitted`
2. 按 `deadlineAt` 排序并汇总课程与标题

用户说「刷新一下」：

1. `Catcher.exe status`
2. 有 connected 平台时执行 `Catcher.exe refresh`
3. 用 `list` 读取新结果

用户说「这条作业我做完了」：

1. 用 `list` 找到对应 id
2. 与用户确认后执行 `Catcher.exe mark <id> --status submitted`
