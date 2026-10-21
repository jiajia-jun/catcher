# pkg/config · 配置与数据目录

## 数据目录

`ResolveDataDir` 的解析顺序：

1. `--data-dir <path>` 或 `--data-dir=<path>`
2. 环境变量 `CATCHER_DATA_DIR`
3. `wails dev`（检测 `devserver` 环境变量）时的当前工作目录 `data/`
4. `os.Executable()` 同级 `data/`

解析后创建目录并做一次写入探测。失败时显式报错，不回退 C 盘或临时目录。

## 配置文件

`data/catcher.yaml`，不存在时用默认值创建：

```yaml
log:
  level: info
  max_size_mb: 20
  max_backups: 5
  max_age_days: 14

raw:
  enabled: true
  retention_days: 7
  max_total_mb: 100

fetch:
  include_history: false

ui:
  theme: auto
```

- `LoadOrCreate(dir)` 读取或创建；
- `New(path, name, ext)` 保留手动读取能力；
- 非法/缺失项由 `Normalize` 补默认值，不阻止应用启动；
- 配置文件里禁止放凭据。
