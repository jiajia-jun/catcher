# sender · HTTP 请求发送器

>前言： sender 包只负责「把请求发出去、把原始响应拿回来」，是无状态的
>
>实现状态：`Do`、错误模型、限速、超时、响应体上限与单测已在 M0 落地。

---

## 1. 定位与边界

### 是什么

模拟客户端向学习通 / 雨课堂发 HTTP 请求的**通用传输层**。

| sender 不含有的 | 归谁 |
| --- | --- |
| cookie 表 / 会话状态 | 持久化形态归 `domain/entity` + `infra/sqlite`；请求期形态归平台适配器 |
| 学习通 AES 加密、雨课堂扫码 | 平台适配器（`infra/bridge/chaoxing`、`infra/bridge/yuketang`） |
| 业务判据（登录态、作业状态） | 平台适配器 |
| 响应解析（JSON / HTML / 字符集转换） | 平台适配器 |

### 在分层中的位置

```text
infra/bridge/chaoxing ─┐
                       ├─-> infra/sender -> net/http
infra/bridge/yuketang ─┘                     └─→ pkg/logger
```

**本包不被 `domain` / `service` 引用，因此不定义接口。**
`domain` 只认识平台适配器实现的 `Fetcher`，全程看不见 HTTP。

---

## 2. 为什么是无状态的

cookie 在这个项目里有**三种形态**，把它们混成一个「cookie 存储」才是设计错误：

| 形态 | 存放位置 | 说明 |
| --- | --- | --- |
| **持久化登录态** | `domain/entity` 的 `Session` + `infra/sqlite` | 「某账号在某平台的登录态」是业务概念，且必须跨重启复用（雨课堂 `sessionid` 活数天，不必每次扫码） |
| **抓取期 cookie 表** | 平台适配器私有 | 学习通要 host 分区累积、策略是「无脑累积全部」；雨课堂只要一个 `sessionid` 字符串。两套策略不同，抬到通用层就会长出平台分支 |
| **sender 内** | **没有** | sender 只把响应里的 `Set-Cookie` 原始行原样交出去 |

**为什么不让 sender 持有 cookie 罐**：那它就必须能把自己的状态倒进 sqlite 再恢复——通用传输层被迫依赖存储；而且 jar 会按 host-only 规则过滤掉跨域的凭据（`passport2.xuexitong.com` 的 `JSESSIONID` 不会被带给 `mooc1-api.chaoxing.com`），而**手工累积是不过滤的**，这正是文档要的「无脑累积全部 `Set-Cookie`，不挑名字、不硬编码」。

> 代价：适配器每次发请求多一行 `Header: {"Cookie": tbl.HeaderFor(host)}`。
> 换到的是：sender 可并发、可单测、且能看见 302 链上每一跳的 `Set-Cookie`。

---

## 3. 关键决策

### 3.1 底座：`net/http`，不用 `req`

两份接口文档列出的全部硬要求，`net/http` 都原生满足，而且**默认更安全**：

| 文档要求 | `net/http` | `req/v3` |
| --- | --- | --- |
| cookie 罐必须关闭 | ✅ 零值即 `nil` | ❌ **默认开启**，必须显式 `SetCookieJar(nil)` |
| 关自动重定向、拿 3xx 原响应 | ✅ `CheckRedirect` + `ErrUseLastResponse` | ✅ `NoRedirectPolicy()`（同一签名） |
| 相对 `Location` 解析 | ✅ `resp.Location()` 自动解析 | ❌ **`Response` 上没有这个方法** |
| 解析 `Set-Cookie` | ✅ `resp.Cookies()` | ✅ `GetHeaderValues` |
| 原始响应留底 | ✅ `httputil.DumpResponse` | ✅ `resp.Dump()` |
| 每请求超时 | ✅ context | ⚠️ 同样无 per-request API |

`req` 独有的能力只有四项：**uTLS 指纹**、br/zstd 解压、内置重试、HTTP/3。
其中后三项本项目不需要（见 §6），而 **uTLS 指纹用 `net/http` 也能实现**——`http.Transport.DialTLSContext` 返回一个 `utls.UConn` 即可（`utls.Conn` 实现了 `net.Conn`）。

> 两份接口文档中 `指纹|风控|WAF|验证码|captcha|TLS|JA3|限流` **零命中**，
> 文档甚至记录「实测连 `Accept` 都不带也能拿到」。目前无任何证据需要指纹伪装。

**底座是纯实现细节**：本包对外只暴露自己的 `Request`/`Response`，将来若真需要 TLS 指纹，在 `sender` 内部换 `Transport` 即可，平台适配器一行都不用改。

### 3.2 重定向：恒关闭

```go
CheckRedirect: func(req *http.Request, via []*http.Request) error {
    return http.ErrUseLastResponse // 3xx 原样返回，不跟随
}
```

`net/http` 文档：返回 `ErrUseLastResponse` 时，「the most recent response is returned with its body unclosed, along with a nil error」。即 3xx 的 header（含 `Set-Cookie`）完整保留。

**为什么不做成可配置**：req 的 `RedirectPolicy` 签名是 `func(*http.Request, []*http.Request) error`——`via` 是**请求**不是响应，所以**自动重定向的钩子里拿不到中间跳的 `Set-Cookie`**。也就是说自动跟随和学习通「逐跳收 cookie」天然互斥，留两个模式只会让人选错。

**为什么不实现 `DoChain`**：实测（ApiPost，关闭自动重定向 + 清空 cookie 罐）表明**一次登录请求的 `Set-Cookie` 就足以访问后续接口**，cookie 应该是在第一跳就全下发的。而万一真出现 302，它会以 `StatusError{302}` **显式报错**、Response 仍可读——**不会静默漏 cookie**。需要时再加那 30 行即可。

### 3.3 错误模型

雨课堂 ② 是逼出这套模型的地方：

```text
还没扫码   → nginx 504 + HTML      → 「这不是错误，继续轮询」
token 作废 → HTTP 400 + code 50400 → 「这是错误，回①换一张码」
扫码成功   → HTTP 200 + code 0     → 正常
```

同一个循环里 **504 是正常、400 是终止**，而两者都非 2xx。所以 sender **不替调用方判断，但必须让调用方能分辨**。

```go
type StatusError    struct{ Code int }        // 非 2xx —— 此时 Response 同时非 nil，body 可读
type TimeoutError   struct{ Op string }       // 我们等够了（context deadline 或 socket 超时）
type CanceledError  struct{ }                 // 上层主动取消 —— 一律放弃，别再重试
type TransportError struct{ Err error }       // 连接 / DNS / TLS
type TooLargeError  struct{ Limit, Got int64 }// 响应体超限
```

调用方这样用：

```go
var se *sender.StatusError
if errors.As(err, &se) && se.Code == 504 { /* 继续轮询 */ }

var te *sender.TimeoutError
if errors.As(err, &te) { /* 继续轮询 */ }

var ce *sender.CanceledError
if errors.As(err, &ce) { /* 放弃，且不要再重试 */ }
```

**非 2xx 也报错**（而不是只给 `resp.Status` 让调用方自己看）是刻意的：`req` 的原生行为是非 2xx **不报错**，那意味着调用方忘了检查 `Status` 就是静默失败。让它走 error 路径，**强制调用方表态**。

### 3.4 超时模型

**`http.Client.Timeout` 恒为 0**，超时全部由 per-request context 表达。

原因是它和 context deadline **谁先到谁生效**：若设了 30s 的 `Client.Timeout`，雨课堂那个 70s 的 context 会在第 30 秒被腰斩——而且现象看起来是「平台挂了」，实际是我们自己砍的，属于诊断灾难。

```go
func (s *Sender) Do(ctx context.Context, r *Request) (*Response, error) {
    d := r.Timeout
    if d == 0 {
        d = s.defaultTimeout // 兜底，防挂死
    }
    ctx, cancel := context.WithTimeout(ctx, d)
    defer cancel()
    // ...
}
```

| 场景 | 写法 | 实际超时 |
| --- | --- | --- |
| 普通请求 | `Timeout` 留 0 | `DefaultTimeout`（默认 30s） |
| 雨课堂 ② 轮询 | `Timeout: 70 * time.Second` | 70s（服务端挂住约 60s 才由 nginx 切 504，客户端必须等更久） |
| 逐课/逐条循环 | `Timeout` 留 0 | 每次调用各自计时 |

**超时的保证来自 `Do` 而不是 `http.Client`**——每次调用都无条件包一层 `context.WithTimeout`，所以不存在「调用方忘了传就永久挂死」。

### 3.5 限速

```go
func (s *Sender) Do(...) {
    s.mu.Lock()
    if wait := s.minInterval - time.Since(s.lastSent); wait > 0 {
        // 实现里用 timer + ctx 等待，取消时立即返回 CanceledError。
        waitWithContext(ctx, wait)
    }
    s.lastSent = time.Now()
    s.mu.Unlock()
    // ...
}
```

**限速放在 sender 里，是为了防「写了循环忘了 sleep」**——而那正好是静默失败（被风控但不报错）。调用方一个 `MinInterval` 就开好了：

| 平台 | 间隔 | 出处 |
| --- | --- | --- |
| 学习通 作业详情 | 250ms | 接口文档实现要点 7 |
| 雨课堂 逐课 | 300ms | 接口文档实现要点 16 |

**副作用**：这个锁让**并发调用被自然摊平**——「并发安全」和「限速生效」是同一个机制，不需要额外设计。代价是并发不会提升吞吐（34 门课 × 300ms ≈ 10s 串行），这是接口文档要求的节奏。

### 3.6 实例粒度

**每个平台适配器持有一个 `Sender`，两者共享同一个 `http.Transport`。**

- 分开：限速是**按实例**生效的。共用一个实例会让学习通的 250ms 拖慢雨课堂，两个平台的节奏互相干扰，也没法单独调某一个。
- 共享 `Transport`：复用连接池。

### 3.7 原始响应留底

两份接口文档都要求「**原始响应留底，便于排查平台改版**」。本包是唯一能看见原始字节的地方。

```go
logger.Debug("http",
    "method",       r.Method,
    "url",          logger.RedactURL(r.URL),
    "status",       statusOf(resp),
    "duration_ms",  elapsed.Milliseconds(),
    "body_bytes",   len(body),
    "resp_headers", logger.RedactHeaders(resp.Header),
    "error",        errText,
)
```

**四个要点**：

1. **走 DEBUG 级别**，不走 INFO。响应体是 HTML，一条作业列表页就好几 KB，按 INFO 记会把日志撑爆。
2. **成功与失败都记**。超时和 504 的现场**最需要**留底；若只在成功路径记录，出事时正好是空白。
3. **必须打码**。`Cookie` / `Set-Cookie` / `Authorization` 的值在交给 logger 之前换成 `***`；URL 查询串里的 `sessionid` / `auth` 等也要打码。
   学习通的 cookie 集合**等于完整登录态**，拿到即可冒充用户；而「留底」的场景天然意味着这些内容会被翻出来看、甚至贴出去。
4. **不把响应正文写进日志**。sender 只能可靠地打码 HTTP 头；正文里的凭据由适配器负责。
   失败响应的原始字节由 raw store 落盘（M4），成功响应只在 DEBUG 记录元数据。

sender 只能打码 **HTTP 头与 URL**；**响应正文里的凭据由适配器负责**（只有适配器知道哪个字段敏感）。

> 若将来需要把留底写到日志以外的地方（按天存文件、存 sqlite、只存失败的），
> 再在 `Sender` 上加一个函数字段即可——**是加字段，不是改签名**，调用方无感。

### 3.8 响应体上限

`net/http` 读响应体**无上限**。而本项目里「返回一大坨意料之外的东西」是**常态**——学习通有两条链路会返回整页 HTML，且**会话失效时也返回 200 + 一整页登录页 HTML**。

不加限制的后果是 OOM kill：**无日志、无堆栈，看起来像程序自己消失了**。

```go
body, err := io.ReadAll(io.LimitReader(resp.Body, s.maxBody+1))
if int64(len(body)) > s.maxBody {
    return nil, &TooLargeError{Limit: s.maxBody, Got: int64(len(body))}
}
```

**超限报错，不截断**——截断会让下游把半截 HTML 当完整内容解析，正是接口文档最怕的静默错误（「静默读空」）。

### 3.9 零解析

sender **一个字节都不动**：

- 不自动 JSON 解析
- 不做字符集转换（学习通作业列表是 UTF-8 HTML，文档特意写了「别按 GBK 解码」；`net/http` 不做任何转换，原样给字节——这正是我们要的）
- 不判 `Content-Type`

---

## 4. API

```go
package sender

// ---------- 构造 ----------

type Config struct {
    Transport      http.RoundTripper // 多个 Sender 可共享同一个，复用连接池
    DefaultTimeout time.Duration     // 0 → 30s
    MaxBodyBytes   int64             // 0 → 8MB
    MinInterval    time.Duration     // 0 → 不限速
    Logger         logger.Logger     // nil → 不输出日志
}

func New(cfg Config) *Sender

// ---------- 输入契约 ----------

type Request struct {
    Method  string
    URL     string
    Header  map[string]string // 含 Cookie —— 由调用方拼
    Body    []byte
    Timeout time.Duration     // 0 → 用 DefaultTimeout
}

// ---------- 输出契约 ----------

type Response struct {
    Status     int
    Header     http.Header
    Raw        []byte   // 原样字节，不解码、不解析
    SetCookies []string // 原始 Set-Cookie 行，不解析、不按名字过滤
    Location   string   // 已 resolve 为绝对 URL
    ReceivedAt time.Time
}

// ---------- 发送 ----------

func (s *Sender) Do(ctx context.Context, r *Request) (*Response, error)
```

**只有 `Do` 一个方法。** 没有 `Get`/`Post` 便捷方法——本包的所有调用都是「构造好一个请求再发」，便捷方法只会增加表面积。

---

## 5. 行为约定速查

| 项 | 约定 |
| --- | --- |
| 重定向 | **恒关闭**。302 → `StatusError{302}`，Response 仍可读（Location + Set-Cookie 都在） |
| 超时 | `Client.Timeout` 恒为 0；每次 `Do` 无条件包 `context.WithTimeout` |
| 限速 | 实例级最小间隔，并发调用被锁摊平 |
| 失败 | 非 2xx **报错**且 Response 非 nil；超时 / 取消 / 传输失败 / 超限各有独立错误类型 |
| 留底 | `logger.Debug` 记录元数据与 header（打码）；正文不写日志，失败响应由 raw store 落盘 |
| 解析 | 零 |
| Cookie | **不碰**。只把 `Set-Cookie` 原始行交出去 |
| 并发 | 安全（`mu` 保护限速状态）；`Sender` 本身无其它可变状态 |

---

## 6. 明确不做

| 不做 | 理由 |
| --- | --- |
| cookie 存储 / 注入 | 见 §2。jar 还会过滤掉跨域所需的 host-only 凭据 |
| 自动跟随重定向 / `DoChain` | 见 §3.2。万一出现 302 会显式报错，不会静默 |
| **自动重试** | 登录 POST 不该盲目重试；且失败必须显式。需要时由适配器自己决定 |
| 定义 `domain` 接口 | 没有内层调用方（见 §1） |
| 引入 `req` 库 | 见 §3.1。底座是纯实现细节，需要时再换 |
| 字符集转换 / JSON 解析 | 见 §3.9 |

---

## 7. 未决 / 待验证

| # | 项 | 现状 |
| --- | --- | --- |
| 1 | 学习通登录是**单跳**还是**多跳** | 实测（ApiPost）支持单跳，接口文档坚持多跳。设计已用「302 显式报错」兜住，**两条真相都吃得下**，不阻塞 |
| 2 | 雨课堂的**提交状态** | 接口文档标注「未找到；且在出现未交作业之前无法验证」。影响 `domain` 模型，**不影响本包** |
| 3 | 留底默认是否常开 | 已定：失败响应常开落盘（M4 接入 raw store），成功响应仅 DEBUG 记元数据；日志不写正文 |

---

## 附录：接口文档要求 → 本包对应

| 文档要求 | 落在哪 |
| --- | --- |
| 学习通 1 / 雨课堂 3「手工维护 cookie 表，不用 cookiejar」 | §2 —— cookie 全在适配器，sender 不碰 |
| 学习通 3 / 雨课堂 12「失败不许静默」 | §3.3 —— 非 2xx 走 error 路径，强制调用方表态 |
| 学习通 7「详情页逐条间隔 250ms」 | §3.5 —— `MinInterval` |
| 学习通 8 / 雨课堂 17「原始响应留底」 | §3.7 —— `logger.Debug` + 打码 |
| 雨课堂 5「轮询超时设在 60s 以上，且把 504 和超时都当继续等」 | §3.3 + §3.4 —— `TimeoutError` 与 `StatusError{504}` 分开表达 |
| 雨课堂 16「逐课间隔 300ms」 | §3.5 —— `MinInterval` |
