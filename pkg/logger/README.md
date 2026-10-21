# pkg/logger · 日志与打码

封装 `log/slog`：

```go
type Logger interface {
    Debug(msg string, args ...any)
    Info(msg string, args ...any)
    Warn(msg string, args ...any)
    Error(msg string, args ...any)
    With(args ...any) Logger
}
```

- 文件：`data/logs/catcher.log`，lumberjack 按大小/备份数/天数滚动；
- 开发模式同时输出 stderr；发布模式只写文件；
- 级别从 `catcher.yaml` 读取，默认 `info`；
- 不输出 Cookie / Set-Cookie / Authorization / 密码 / token / session / 二维码内容。

## 打码工具

- `IsSensitiveKey(key)`：判断字段名是否敏感（含 `_token` / `_secret` / `_session` 等后缀）；
- `Redact(key, value)`：敏感字段返回 `***`；
- `RedactHeader` / `RedactHeaders`：对 header 值打码，不修改原 header；
- `RedactURL`：对查询串中的敏感参数打码。

`New` 的 handler 还会对敏感 attr 做一次兜底打码。sender 只在 DEBUG 级别记录 method/url/status/header（打码）与 body 长度，不把响应正文写进日志。
