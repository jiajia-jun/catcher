# 贡献指南

欢迎提交 issue 和 PR。Catcher 是 Windows 桌面应用（Wails v2 + Go + Vue 3）。

## 环境准备

- Go 1.27.1
- Node 24 / npm 11
- Wails CLI v2.13.0

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
wails version
```

会话与密码加密依赖 Windows DPAPI，非 Windows 平台只能跑到 `ErrUnsupported`。

## 本地开发

```bash
wails dev                      # 前端热更新，数据写到仓库根目录 data/
wails build                    # 产物 build/bin/Catcher.exe
go test ./...                  # 全部单测
go vet ./...                   # 静态检查
cd frontend && npm run build   # vue-tsc 类型检查 + 打包
```

`go build ./...` 需要 `frontend/dist` 至少有占位文件（仓库里的 `.gitkeep` 已够用）。

## 分支与提交

- `dev` 是日常开发分支，`main` 只承载发布提交
- **从 `dev` 拉分支，PR 目标选 `dev`**，不要直接往 `main` 提
- 分支名自定，例如 `feat/学习通扫码`、`fix/扫码倒计时`
- 提交信息用中文 conventional 前缀：`feat(学习通): ...`、`fix(界面): ...`、`docs(接口): ...`、`chore(发布): ...`
- 提交说明以精简为主：只写标题，必要时补一两行关键原因
- 按功能拆分提交，不要把无关改动堆进一个提交；不要添加协作者署名

## 代码约定

分层依赖方向：

```text
main.go -> cmd/app -> internal/handle -> internal/service -> internal/domain
internal/infra -> internal/domain
所有层 -.-> pkg
```

各层职责与 `pkg` 的约束见 [docs/架构.md](docs/架构.md)。

- `domain` 是最内层，不依赖外层；`infra` 实现 `domain` 定义的端口，在 `cmd/app` 完成装配
- `pkg` 不得 import `internal/` 或 `cmd/` 下的任何包
- Wails 方法的入参出参放 `internal/dto`，不要把领域实体直接传出去
- 平台适配器放 `internal/infra/bridge/<platform>`，每个适配器持有自己的 `Sender`
- 平台 ID 一律用 `int64`，不要过 `float64`（学习通的课程 id 会因此对不上）
- `sender` 不持有 cookie、不使用 cookiejar、不跟随重定向、不自动重试。改 `Do` 的行为前先读 [internal/infra/sender/README.md](internal/infra/sender/README.md)
- 时间字段按对应平台文档处理，不要套一个统一规则

## 安全

- 不要提交真实凭据。`data/`、`catcher.json` 等已在 `.gitignore` 里
- 密码和会话只经 DPAPI 加密后落库，不得出现在日志、事件或 DTO 返回值里
- 日志里不要打印 `Cookie`、`Set-Cookie`、`Authorization` 的值

## 测试

- 单测和被测包放同一目录，文件名 `*_test.go`
- 平台适配器的测试用固定样本，不要连真实站点
- 提交 PR 前跑一遍 `go test ./...` 和 `go vet ./...`；动了前端再跑 `cd frontend && npm run build`

## 关于 CI

[.github/workflows/build.yml](.github/workflows/build.yml) 目前只在 **push 到 `main`** 和 **推 `v*` tag** 时触发，PR 上不会跑。所以上面那些检查请在本地自己跑完。

## 发版流程

对维护者：

1. 在 `dev` 上把 `cmd/app/app.go` 的 `Version` 改成目标版本，提交 `chore(发布): 版本号改为 X.Y.Z`
2. `git merge --no-ff dev` 到 `main` 并推送（会触发一次双架构构建，不建 Release）
3. 打 tag `vX.Y.Z` 并推送，CI 校验 tag 与 `Version` 一致后自动建 Release，附带 amd64 / arm64 两个 zip
4. 正式版本的 tag 不能带 `-`，否则会被自动标为 pre-release，`releases/latest` 查不到，客户端不会提示更新

## 文档

- [docs/架构.md](docs/架构.md)：分层结构、各层职责、依赖方向与 `pkg` 的约束
- [internal/infra/sender/README.md](internal/infra/sender/README.md)：sender 的设计契约，改 `Do` 的行为前必读
- 平台接口文档（学习通、雨课堂、学在重邮）不随仓库发布。要改对应适配器，先找维护者要文档，特别是其中标注「未证实」的部分不要当成事实改
- 注释和文档用中文
