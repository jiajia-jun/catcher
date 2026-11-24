<p align="center">
  <img src="logo/logo-wordmark-1024x256.png" width="440" alt="Catcher">
</p>

<p align="center"><a href="README.md">简体中文</a> | <a href="README.en.md">English</a></p>

<p align="center">一个把雨课堂、学习通、学在重邮、批改网、FiF 口语的作业抓取到一个本地窗口里的工具</p>

<p align="center">
  <img alt="version" src="https://img.shields.io/badge/version-1.5.1-4c6ef5">
  <img alt="platform" src="https://img.shields.io/badge/platform-Windows%2010%2F11%20x64%2Farm64-4c6ef5">
  <img alt="本地化" src="https://img.shields.io/badge/本地化-数据不出本机-4c6ef5">
  <img alt="无三方" src="https://img.shields.io/badge/无三方-无中转服务器-4c6ef5">
  <img alt="license" src="https://img.shields.io/badge/license-Apache--2.0-4c6ef5">
  <a href="https://github.com/jiajia-jun/catcher/releases/latest"><img alt="下载最新版" src="https://img.shields.io/badge/下载-最新版-4c6ef5"></a>
</p>

## 最新版本支持平台

| 平台 | 登录方式 | 状态 |
| --- | --- | --- |
| 雨课堂 | 扫描二维码 | 实验性（作业类型未覆盖全） |
| 学习通 | 扫描二维码 / 手机号 + 密码 | 已实机验证 |
| 学在重邮 | 导入浏览器 session | 已实机验证 |
| 批改网 | 微信扫码 | 实验性（分页未验证） |
| FiF 口语 | 微信扫码 | 已实机验证 |

## 功能

- 作业表统一展示平台、课程、状态、截止时间与时间来源
- 支持按课程搜索，点击作业标题跳到作业提交处（目前雨课堂部分作业可能出现无法跳转）
- 平台卡可筛选平台、勾选是否参与刷新；平台卡片单行排列，悬停滚轮左右切换
- 按平台刷新，带进度与取消，单个平台失败不影响其他
- 支持**手动标记**作业提交状态
- 启动时自动检查更新，升级后首次登录展示更新日志
- 所有数据只保存在本机，**不上传任何服务器**

## 安装

系统要求：Windows 10/11，x64 或 ARM64。需要 WebView2 Runtime：安装器会自动处理，便携版要求系统里已有（Windows 11 自带）。

到 [Releases](https://github.com/jiajia-jun/catcher/releases/latest) 页下载，两种包任选：

- **安装器**（仅 x64）：`Catcher-<版本>-amd64-installer.exe`，默认装到当前用户目录，**不需要管理员权限**，也可以在安装页改成任意当前用户可写的目录（包括 D:、E: 等非系统盘）
- **便携版**：`Catcher-windows-amd64.zip`（x64）或 `Catcher-windows-arm64.zip`（ARM64），解压到任意可写目录，运行 `Catcher.exe`

> 安装目录必须对当前用户可写。`Program Files` 这类目录普通用户无法写入，安装器会拦下来并提示换一个位置

### 卸载与更新

#### 卸载

卸载程序位于**安装目录下**：卸载时会询问是否一并删除数据。选择保留时，作业记录、登录态和日志会留在安装目录的 `data/` 里，**重新安装到同一目录即可继续使用**  
安装目录本身不会被删除：即使选择删除数据，也只移除程序文件，目录保留在原位

#### 更新

安装到旧版本的**相同目录**下即可

<details>
<summary>数据目录</summary>

默认是 exe 同级的 `data/`。也可以用以下方式指定：

1. 命令行 `--data-dir <path>`
2. 环境变量 `CATCHER_DATA_DIR`

目录不存在会自动创建；不可写时显式报错，**不会回退到 C 盘或临时目录**。

```text
data/
├── catcher.yaml
├── catcher.db
├── catcher.db-wal
├── catcher.db-shm
└── logs/
    └── catcher.log
```

建议安装在本地磁盘。网络盘、映射盘上 SQLite 的文件锁不可靠，可能导致数据损坏。

</details>

## 安全与隐私

- 程序**不监听任何本地端口**，也不在后台起服务，只有主动发出的出站请求
- 雨课堂、学习通、批改网与 FiF 口语走 HTTPS；学在重邮的校园站点只开放 HTTP，session在该站内为明文传输，**建议连接校园网或VPN使用**
- 登录态（含批改网与 FiF 口语 cookie）与学习通密码使用 Windows DPAPI 加密后存入本地 sqlite，只对当前 Windows 用户可解密
- 日志**不记录** Cookie、Set-Cookie、Authorization、密码、token 或二维码内容
- 更新检查只向 GitHub 查询最新版本号，请求只含客户端版本号与常规 HTTP 头，不携带账号、作业或日志数据
- 数据**不会上传到任何第三方服务器**

## 已知限制

- **设置仍是只读的**：日志级别等选项在 `data/catcher.yaml` 中，修改后需要重启；界面只支持修改平台刷新勾选
- **学在重邮只支持导入浏览器 session**：失效后需要重新导入，不支持 CAS 自动登录
- **学习通的作业标题会先打开课程「作业」列表**：需要在列表里再点一次进入作业
- **雨课堂与批改网仍标注实验性**：部分作业类型可能抓不到；批改网只抓列表第一页，作业较多时可能漏掉更早的记录
- **FiF 口语只能微信扫码**：应用关闭**几个小时**后需要重新扫码；作业详情**需要浏览器自己已登录 FiF**
- 安装包与程序**未做代码签名**，Windows SmartScreen 可能提示「未知发布者」，这一点是正常的

更详细的平台验证状态与实现限制见[贡献指南](CONTRIBUTING.md#已知限制与验证状态)。

<details>
<summary>从源码构建</summary>

需要 Go 1.27、Node 24、Wails v2.13。

```bash
wails build            # 产物 build/bin/Catcher.exe
wails build -nsis -installscope user   # 额外生成用户级安装包（需要 NSIS）
```

开发模式：

```bash
wails dev              # 前端热更新，数据写到仓库根目录 data/
```

</details>

## 免责声明

- 本项目是非官方的个人学习项目，与雨课堂、学习通、学在重邮、批改网、FiF 口语及其运营方没有任何关联
- 仓库内的接口文档由公开页面抓包整理而来，仅用于学习与技术研究；接口及相关内容归各平台所有，如有侵权请联系删除
- 使用者应自行遵守各平台的用户协议、服务条款和所在地法律法规，不得将本项目用于商业用途、批量抓取、干扰平台正常服务或其他不当用途
- 使用本项目产生的一切后果由使用者自行承担；作者不对数据准确性、服务可用性及由此造成的任何损失负责
- 项目按 Apache 2.0 许可证「按现状」提供，不附带任何明示或暗示的担保

## 许可证

本项目自 v1.5.0 起采用 [Apache License 2.0](LICENSE)，版权与归属声明见 [NOTICE](NOTICE)，随发布包分发的第三方声明见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。v1.4.0 及更早版本仍按 [MIT 许可证](https://github.com/jiajia-jun/catcher/blob/v1.4.0/LICENSE) 发布。

## 说明

这是个人学习项目，**请勿用作其他非法用途**；  
接口文档正在逐步补充，目前已公开：

- [学习通接口文档](docs/接口文档/学习通.md)

- [雨课堂接口文档](docs/接口文档/雨课堂.md)

<h3 align="center">此项目将永久开源</h3>
