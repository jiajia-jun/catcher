# 贡献指南

[简体中文](CONTRIBUTING.md) | [English](CONTRIBUTING.en.md)

欢迎提交 issue 和 PR。Catcher 是 Windows 桌面应用（Wails v2 + Go + Vue 3），把雨课堂、学习通、学在重邮的作业聚合到一个本地窗口里。

参与贡献前，请先确认你的改动符合项目的定位：**所有数据只保存在本机，不上传任何服务器，不绕过平台登录与风控**。

## 设计理念

Catcher 有**两条底线**，评审改动时会按这两条判断

### 本地化

- 数据只落在本机数据目录（`data/`），不进云、不做账号体系、不做遥测
- 不上传作业、日志、登录态或任何使用数据
- 除抓取平台数据和检查更新外，断网也能正常打开和查看
- 本地优先也意味着**失败要显式**：宁可报错，也不静默返回空列表
- 备份、迁移、卸载都只围绕这个目录，不依赖任何远端服务

### 无三方

- 不引入第三方后端、BaaS、云函数、统计 SDK、崩溃上报或 CDN 依赖
- 项目有 Go 后端的业务逻辑，但只跑在本机，**不部署服务器**，也不提供中转、存储或账号服务
- 不监听本地端口，不起后台服务，不要求安装浏览器扩展；程序只在用户操作时主动发出请求
- 出站请求只针对作业平台本身和 GitHub Releases
- 登录态与凭据只存本机，密码、授权码用 DPAPI 加密，不经过任何中转
- 更新检查只向 GitHub Releases 查询版本号，不下载、不执行内容，下载页交给系统浏览器

## 开始之前

- 先搜一遍 issue 和已有 PR，避免重复劳动
- 新功能或较大改动（新增平台、改数据模型、改 sender 行为）**先开 issue** 说明动机和方案，达成一致后再动手
- 小修复、**接口文档**和测试可以直接提 PR
- 讨论时保持友善，就事论事

## 环境准备

- Windows 10/11（会话与密码加密依赖 DPAPI，非 Windows 平台只能跑到 `ErrUnsupported`）
- Go 1.27.1（与 `go.mod`、CI 保持一致）
- Node 24 / npm 11
- Wails CLI v2.13.0
- WebView2 Runtime（安装器会自动处理；便携版要求系统里已有，Windows 11 自带）

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
wails version
```

> **Go 版本不要随意升级**：Wails v2.13.0 在 Go 1.27.3 下生成绑定时会报错，CI 固定用 1.27.1。

## 本地开发

```bash
wails dev                      # 前后端一起跑，前端热更新，数据写到仓库根目录 data/
wails build                    # 产物 build/bin/Catcher.exe
go build ./...                 # 只编译 Go（需要 frontend/dist 至少有占位文件）
go test ./...                  # 全部单测
go vet ./...                   # 静态检查
go fmt ./...                   # 格式化
cd frontend && npm run build   # vue-tsc 类型检查 + 打包
```

- 仓库里没有 Makefile，命令直接跑
- 开发模式的数据目录是仓库根目录的 `data/`，发布版在 exe 同级的 `data/`
- `frontend/wailsjs/` 由 Wails 生成，**不要手工修改**；改了 Go 侧绑定后重新跑一次 `wails dev` 或 `wails build`

## 分支模型

**只有 `main` 一条长期分支**，始终保持可发布状态。

1. 从最新 `main` 切短分支，命名 `<type>/<英文短横线描述>`，`type` 与提交前缀一致
   - `feat/homework-filter`
   - `fix/qr-countdown`
   - `docs/sender-contract`
2. 在分支上提交，按功能拆成多个小提交
3. 推送并发 PR，目标分支选 `main`
4. 合并前先把分支 rebase 到最新 `main`
5. PR 固定用 **Rebase and merge**，合并后立即删除远程分支，本地也删掉

**不要直接在 `main` 上提交**，也不要使用 merge commit（会破坏线性历史）。

## 提交信息

使用中文 conventional 前缀，scope 用中文模块名：

```text
feat(首页): 增加按课程搜索
fix(学习通): 详情页 302 不再误判为登录失效
docs(接口): 补充扫码状态表
refactor(service): 拆分刷新任务
test(config): 补数据目录解析用例
chore(发布): 版本号改为 1.2.0
```

- 以精简为主：**只写标题**，必要时补一两行关键原因
- **一个提交只做一件事**，不要混入无关格式化或顺手重构

## 代码约定

分层依赖方向：

```text
main.go -> cmd/app -> internal/handle -> internal/service -> internal/domain
internal/infra -> internal/domain
所有层 -.-> pkg
```

各层职责与 `pkg` 的约束见 [docs/架构.md](docs/架构.md)。几条硬约束：

- `domain` 是最内层，不依赖外层；`infra` 实现 `domain` 定义的端口，装配统一在 `cmd/app` 完成
- `pkg` **不得** import `internal/` 或 `cmd/` 下的任何包
- `handle` 是 Wails 绑定层，不接收 `http.Request`；项目没有本地 HTTP 服务、端口或 CORS
- Wails 方法的入参出参放 `internal/dto`，不要把领域实体直接传出去
- 平台适配器放 `internal/infra/bridge/<platform>`，每个适配器持有自己的 `Sender`，限速互不干扰
- 平台 ID **一律用 `int64`**，不要过 `float64`（学习通的课程 id 会静默对不上）
- `sender` 不持有 cookie、不使用 cookiejar、不自动跟随重定向、不自动重试、不解析业务响应。改 `Do` 的行为前先读 [internal/infra/sender/README.md](internal/infra/sender/README.md)
- 时间字段按对应平台文档处理，不要套统一规则；**失败必须显式**，登录失效不能表现成「今天没作业」
- 注释和文档用中文

新增依赖前先说明理由，能用标准库就不要引第三方包。

## 数据与安全

- **不要提交真实凭据**。`data/`、`catcher.json`、`draft/`、`CLAUDE.md` 已在 `.gitignore` 里
- 密码和会话只经 DPAPI 加密后落库，**不得出现在日志、事件或 DTO 返回值里**
- 日志里不要打印 `Cookie`、`Set-Cookie`、`Authorization` 的值；`pkg/logger` 已提供打码工具
- 抓包导出、原始响应留底默认打码，**不要把未脱敏的导出结果贴进 issue、PR 或提交**
- 接口文档已随仓库发布在 `docs/接口文档/`。新增或修改文档时，必须把真实手机号、账号、cookie、token、课程 ID、课程名、活动 ID、作业名替换成占位值，参考现有文档的写法
- 平台行为没验证过的部分要标注「未证实」，不要把推测写成事实，也不要删掉已有的警告
- **不要提交绕过登录、绕过风控、批量注册、共享账号一类的改动**

## 测试

- 单测和被测包放同一目录，文件名 `*_test.go`
- 平台适配器的测试用固定样本或本地 HTML/JSON，**不要连真实站点**；涉及凭据的用例用假数据
- 改动 `sender`、时间解析、去重键、状态映射时，**必须补对应的边界用例**
- 提 PR 前至少跑一遍：

```bash
go test ./...
go vet ./...
```

- 改了前端再跑 `cd frontend && npm run build`（包含 `vue-tsc` 类型检查）
- 改了用户可见行为，在 PR 描述里写清楚复现步骤和验证方式；界面改动附截图更好

## CI

[.github/workflows/build.yml](.github/workflows/build.yml) 在以下情况触发：

- push 到 `main`
- 推送 `v*` tag
- 手动 `workflow_dispatch`

CI 会构建 Windows amd64 / arm64 两个架构，但**单元测试只在 amd64 上跑**。**PR 上不会触发 CI**，所以上面的检查请在本地自己跑完再提 PR。

## 文档

- [docs/架构.md](docs/架构.md)：分层结构、各层职责、依赖方向与 `pkg` 的约束
- [internal/infra/sender/README.md](internal/infra/sender/README.md)：sender 的设计契约，改 `Do` 的行为前必读
- [docs/接口文档/](docs/接口文档/)：学习通、雨课堂的接口说明，改对应适配器前先读；每份文档末尾的「实现要点」和「未证实」部分**不要当成已确认事实**
- 学在重邮的接口文档尚未公开，需要改对应适配器时找维护者
- README 面向使用者：新增功能写进「功能」，没验证完的写进「已知限制」，别把实验性能力写成稳定能力
- Markdown 遵循仓库根目录的 `.markdownlint.json`

## 贡献接口文档

接口文档和代码一样欢迎贡献，尤其是补全「未证实」的部分。已有文档在 [docs/接口文档/](docs/接口文档/)。

提交前请确认：

- **以真实的抓包或页面源码为依据**，不要凭猜测补字段
- 按现有文档的结构写：调用流程、请求与响应、状态表、实现要点，末尾保留「未证实」清单
- 真实手机号、账号、cookie、token、课程 ID、课程名、活动 ID、作业名一律换成占位值；导出前先打码
- 平台改版导致文档失效时，直接更新对应文档，并在 PR 里说明验证方式和日期
- 新平台的文档先开 issue 讨论，确认要接入再写

提交信息用 `docs(接口): ...`。

## 发版流程

仅维护者操作。正式版本按下面的顺序走：

1. 从最新 `main` 开 `chore/release-X.Y.Z` 分支
2. 同时更新四处版本号：
   - `cmd/app/app.go` 的 `const Version`
   - `README.md` 顶部的 version 徽章
   - `README.en.md` 顶部的 version 徽章
   - `build/windows/installer/project.nsi` 的 `INFO_PRODUCTVERSION` 与 `INSTALLER_FILE_VERSION`（后者必须是四段 `X.Y.Z.0`）
3. 提交 `chore(发布): 版本号改为 X.Y.Z`，发 PR，Rebase 合并后删除分支
4. 在 `main` 顶端打 tag 并推送：

   ```bash
   git tag -a vX.Y.Z -m "Catcher X.Y.Z"
   git push origin vX.Y.Z
   ```

5. CI 会校验 tag 与 `app.go` 的 `Version` 一致，然后自动构建并创建 Release，附带 `Catcher-windows-amd64.zip` 与 `Catcher-windows-arm64.zip`
6. CI **不构建 NSIS 安装包**。需要安装器时在本地构建并手动上传：

   ```bash
   wails build -nsis -installscope user
   mkdir -p build/release
   mv build/bin/Catcher-amd64-installer.exe build/release/Catcher-X.Y.Z-amd64-installer.exe
   gh release upload vX.Y.Z build/release/Catcher-X.Y.Z-amd64-installer.exe
   ```

注意事项：

- 正式版本的 tag **不能带 `-`**（写 `v1.2.0`，不要写 `v1.2.0-final`），否则 CI 会自动标成 pre-release，`releases/latest` 查不到，客户端不会提示更新
- tag 与 `cmd/app/app.go` 的 `Version` 不一致时 **CI 直接失败**
- 发版前用 `git grep` 搜一遍旧版本号，确认四处都改到了
- hotfix 同样从 `main` 拉 `fix/<描述>`，按正常流程合入后打补丁版本 tag，不要跳过 PR

## 报告安全问题

**不要公开提 issue**。请通过仓库的 **Security → Report a vulnerability** 私下报告；如果入口不可用，先开一个不含敏感细节的 issue 说明需要私下联系。

## 许可

项目使用 [MIT 许可证](LICENSE)。提交的代码、文档和测试默认按同一许可证授权，不要求签署额外的 CLA。
