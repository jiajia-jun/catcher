# cmd/app · 装配层

根 `main.go` 负责 `//go:embed all:frontend/dist`，然后把 `embed.FS` 交给 `app.Run`。

`app.Run` 按顺序完成：

1. 解析数据目录（`pkg/config.ResolveDataDir`）；
2. 读取或创建 `data/catcher.yaml`；
3. 初始化 `pkg/logger`；
4. 打开 sqlite、执行迁移、确保每个平台有一行账号；
5. 构造 repository、service、Wails 绑定对象；
6. `wails.Run`，把 `handle.App` 绑定给前端；
7. 退出时关闭数据库和日志文件。

这里只做依赖注入，不写业务规则。平台适配器、DPAPI、刷新任务在后续里程碑接入。
