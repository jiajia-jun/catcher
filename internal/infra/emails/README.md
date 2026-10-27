# 功能

进行邮件发送。当前只支持「用发件人邮箱给自己发信」，端口固定 465（隐式 TLS）。

## 配置

配置文件由 `cmd/app` 注入数据目录下的 `email.json`，界面入口是顶栏「邮件通知」：

- 发件邮箱
- SMTP 服务器（只填主机名，例如 `smtp.qq.com`）
- 授权码（不是邮箱登录密码）

修改 SMTP 或发件地址时，授权码留空表示沿用已保存的值。授权码使用 Windows DPAPI
加密后写入 `email.json`，文件内不含明文；旧版明文配置会在下次读取时自动迁移。

## 测试

`init_test.go` 中的真实发送测试默认跳过。需要验证连通性时设置以下环境变量后运行：

```bash
CATCHER_TEST_EMAIL_ADDR=me@example.com \
CATCHER_TEST_EMAIL_SMTP=smtp.example.com \
CATCHER_TEST_EMAIL_PASSWORD=auth-code \
go test ./internal/infra/emails -run TestSendEmailLive -v
```
