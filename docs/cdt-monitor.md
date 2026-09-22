# CDT-Monitor 项目结构与文件说明

> 生成时间：2026-09-20
> 说明范围：当前仓库全部目录与源码文件，包含 Go 后端、React 前端、Android 小组件、Docker 与 CI。

## 1. 项目概览

CDT-Monitor 是面向阿里云 CDT 流量监控、ECS 实例自动化控制和费用观察的**轻量级单机控制台**。HTTP 服务、内置调度器、并发 Worker、SQLite、API 与前端静态资源全部集成在**单个 Go 进程**中，适合 Docker 或独立二进制部署。

- 目标平台：Linux / Windows / macOS，amd64 / arm64
- 数据库：SQLite（WAL 模式），无 MySQL / Redis 依赖
- 前端：React 18 + TypeScript + Vite + Recharts，构建产物通过 `go:embed` 内嵌到二进制
- 附加客户端：Android 桌面小组件原型（`android-widget/`）

## 2. 技术栈与依赖

| 分类 | 技术 / 库 |
| --- | --- |
| 后端语言 | Go 1.24 |
| HTTP | 标准库 `net/http`（`ServeMux` 方法路由） |
| 数据库 | `modernc.org/sqlite`（纯 Go，支持 `CGO_ENABLED=0`） |
| 密码哈希 | `golang.org/x/crypto/argon2`（Argon2id） |
| 加密 | 标准库 AES-GCM（信封加密） |
| Passkey / WebAuthn | `github.com/go-webauthn/webauthn` |
| 代理 | `golang.org/x/net/proxy`（Telegram SOCKS5） |
| 前端框架 | React 18、TypeScript 5.7、Vite 6 |
| 图表 / 图标 | Recharts 2、lucide-react |
| E2E 测试 | Playwright（Chrome） |
| Android | Kotlin DSL Gradle、Android SDK 35、JDK 17 |

## 3. 目录结构

```text
CDT-Monitor/
├── .github/workflows/          CI 与发布工作流
│   ├── android-widget.yml       Android 小组件构建（手动触发）
│   ├── auto-release.yml         自动计算版本并打 Tag
│   ├── ci.yml                   前端构建 + Go race test / vet / build
│   ├── docker-build-push.yml    多架构镜像发布（GHCR + Docker Hub）
│   └── release.yml              多平台二进制与 Release
├── cmd/
│   └── cdt-monitor/
│       └── main.go              程序入口与 CLI 命令分发
├── internal/                    非导出业务包
│   ├── aliyun/
│   │   ├── client.go            阿里云 RPC 客户端（CDT/ECS/BSS）
│   │   └── client_test.go
│   ├── domain/
│   │   └── models.go            领域模型与 JSON DTO
│   ├── engine/
│   │   ├── engine.go            调度器、Worker、策略执行、通知 Worker
│   │   ├── billing_test.go
│   │   ├── lease_test.go
│   │   └── policy_test.go
│   ├── httpapi/
│   │   ├── server.go            HTTP 路由、鉴权、限流、Passkey、静态资源
│   │   └── server_test.go
│   ├── notify/
│   │   ├── service.go           SMTP / Telegram / Webhook 通知发送
│   │   └── service_test.go
│   ├── security/
│   │   ├── security.go          AES-GCM 加密、Argon2id、Token 工具
│   │   └── security_test.go
│   ├── store/
│   │   ├── store.go             数据库打开、WAL 配置、事务、加密门面
│   │   ├── migrations.go        Schema 定义与迁移
│   │   ├── config.go            配置读取/保存、账号持久化
│   │   ├── auth.go              管理员密码、Session、API Key、Passkey
│   │   ├── runtime.go           日志、统计、任务队列、Outbox、租约
│   │   └── store_test.go
│   └── web/
│       ├── web.go               go:embed 嵌入前端构建产物
│       └── dist/                前端构建输出（assets/、index.html、icon.png）
├── web/                        前端源码工程
│   ├── src/
│   │   ├── main.tsx            React 挂载入口
│   │   ├── App.tsx             主应用（仪表盘/设置/登录/向导等页面）
│   │   ├── HistoryChart.tsx    流量历史折线/柱状图
│   │   ├── api.ts              API 封装、CSRF、Job 轮询
│   │   ├── types.ts            前端类型定义与默认配置
│   │   └── styles.css          全局样式
│   ├── tests/
│   │   ├── install.spec.ts     安装与登录 E2E
│   │   └── ui-regressions.spec.ts  UI 回归 E2E
│   ├── public/icon.png
│   ├── index.html
│   ├── package.json / package-lock.json
│   ├── playwright.config.ts
│   ├── tsconfig.json / tsconfig.app.json / tsconfig.node.json
│   └── vite.config.ts          构建输出到 ../internal/web/dist
├── android-widget/             Android 桌面小组件原型
│   ├── app/
│   │   ├── build.gradle.kts / proguard-rules.pro
│   │   └── src/main/
│   │       ├── AndroidManifest.xml
│   │       ├── java/com/wang4386/cdtmonitor/widget/
│   │       │   ├── AppPreferences.java        站点地址等偏好存储
│   │       │   ├── CdtApi.java                调用 /api/v1/widget/summary
│   │       │   ├── CdtWidgetProvider.java     小组件 AppWidgetProvider
│   │       │   ├── SecretStore.java           Android Keystore 加密 Key
│   │       │   ├── SettingsActivity.java      连接设置页
│   │       │   └── WidgetConfigureActivity.java 小组件配置页
│   │       └── res/                           layout / drawable / values / xml
│   ├── build.gradle.kts / settings.gradle.kts / gradle.properties
│   └── README.md
├── docs/
│   ├── architecture-analysis.md  架构分析与重构建议
│   └── cdt-monitor.md            本文件
├── Dockerfile                   多阶段构建 → scratch 运行镜像
├── docker-compose.yml
├── go.mod / go.sum
├── icon.png
├── LICENSE (MIT)
└── README.MD
```

## 4. 后端模块与文件职责

### 4.1 `cmd/cdt-monitor/main.go`

程序入口，解析命令与参数并组装依赖。

- 命令分发：`serve`（默认，启动 HTTP + 调度器 + Worker）、`run-once`（单轮监控）、`migrate`（迁移后退出）、`version`、`healthcheck`。
- 参数与环境变量：`--data` / `CDT_DATA_DIR`（默认 `./data`）、`--listen` / `CDT_LISTEN`（默认 `:8080`）、`--workers` / `CDT_WORKERS`（默认 4）。
- `loadExtraRegions`：启动时读取数据目录 `regions.json`（可选，格式 `[{"value","label"}]`），校验去重后传给 `engine.New`（自定义地域名）与 `httpapi.New`（`GET /api/v1/regions`）。
- 使用 `slog` JSON 日志；`signal.NotifyContext` 处理 SIGINT/SIGTERM 优雅退出；导入 `time/tzdata` 内嵌时区。
- 通过 `-ldflags` 注入 `version` / `commit` / `builtAt`。

### 4.2 `internal/domain/models.go`

纯数据模型，无业务逻辑：

- 状态常量：`Unknown / Running / Stopped / Starting / Stopping`
- `Account`、`Config`、`NotificationConfig`（`EmailConfig`/`TelegramConfig`/`WebhookConfig`）
- `AccountSummary`（状态页脱敏摘要）、`Job`、`APIKey`、`Passkey`、`LogEntry`、`TrafficPoint`、`History`、`NotificationEvent`

### 4.3 `internal/security/security.go`

- `Cipher`：基于 AES-GCM 的加解密，密文前缀 `enc:v1:`；`LoadOrCreateCipher` 在数据目录生成/读取 32 字节 `master.key`（0600 权限）。
- 密码：`HashPassword`（Argon2id：m=64MB,t=3,p=2）、`VerifyPassword`、`HashLegacyPassword`。
- 工具：`NewToken`（随机 Token）、`TokenHash`（SHA-256）、`IsEncrypted`。

### 4.4 `internal/store/`

| 文件 | 职责 |
| --- | --- |
| `store.go` | `Open` 打开 SQLite 并设置 `journal_mode=WAL`、`synchronous=NORMAL`、`busy_timeout=5000`、`foreign_keys=ON`；`SetMaxOpenConns(1)`；`WithTx` 事务封装；`Encrypt/Decrypt` 门面；启动时清理超时的 `running` 任务与 `sending` Outbox |
| `migrations.go` | 完整 `schema` 常量与 `Migrate`；补列（`remark`/`site_type`/`deleted_at`）；`migrateLegacyStats` 把旧 `traffic_hourly/daily` 从 `access_key_id` 迁移到稳定 `account_id`；`migratePlaintextSecrets` 把旧明文密码/Token/Secret 加密 |
| `config.go` | `GetConfig` 组装 `domain.Config` 并解密敏感设置；`Setup`/`SaveConfig` 校验并事务化写入；`saveAccountsTx` 以稳定 ID + 复合键匹配、软删除账号；`ListAccounts`/`GetAccount`/`AccountSecret`/`UpdateRuntime` |
| `auth.go` | 管理员密码校验（旧哈希自动升级）、登录失败计数、Session 创建/校验/删除、API Key 创建/列表/撤销/校验、Passkey 存取、改密后清理其它 Session |
| `runtime.go` | 日志读写与清理、`Prune` 定期清理、流量小时/日统计、`History`、`LastMonitorRun`、**任务队列**（`EnqueueJob`/`ClaimJob`/`CompleteJob`/`FailJob`）、**调度租约**（`AcquireLease`）、**动作幂等事件**（`RecordActionEvent`/`DeleteActionEvent`）、**通知 Outbox**（`AddOutbox`/`ClaimOutbox`/`CompleteOutbox`/`FailOutbox`）、账单缓存 |

### 4.5 `internal/engine/engine.go`

核心编排层，是 README 架构图中“调度器 + Worker + 策略”的实现：

- `Engine`：持有 `store`、`provider`、`notify`、Worker 数、唤醒通道、按账号互斥锁。
- `Start`：启动 `scheduler` + N 个 `worker` + `notificationWorker`。
- `scheduler`：每 15 秒尝试 `RunOnce`（需先获 `monitor` 租约，避免多进程重叠），每 30 分钟触发 `Prune`。
- Job 类型：`monitor_account`、`refresh_account`、`control_instance`、`test_notification`。
- `processAccount`：定时开关机（10 分钟补偿窗口）、并发查询流量与实例状态、阈值判定（`stop_and_notify` / `notify_only`，事件键去重）、保活启动（受时间窗口约束）、账单刷新、写 heartbeat 日志。
- `executeScheduledAction`：按 `schedule:<id>:<date>:<action>` 幂等执行并可选通知。
- `control`：手动 start/stop，拒绝过渡状态操作、保活时禁止手动关机。
- `Summary`：生成脱敏摘要与 `stale` 判定，供 `/status` 与 widget 接口使用。
- 纯函数：`dueWithin`、`inTimeRange`（跨午夜）、`transient`、`usagePercent`、`masked`、`RegionName`（地域中文名映射）；`Engine.regionName` 在内置表未命中时回退到 `regions.json` 加载的自定义地域名。

### 4.6 `internal/httpapi/server.go`

- 路由（`net/http` 方法路由）：
  - 健康：`GET /healthz`、`GET /readyz`
  - 初始化：`GET /api/v1/system/init-status`、`POST /api/v1/setup`、`GET /api/v1/system/info`
  - 地域：`GET /api/v1/regions`（公开，返回数据目录 `regions.json` 中加载的自定义地域，供前端与内置列表合并去重）
  - 认证：`POST /api/v1/auth/login`、`POST /api/v1/auth/passkeys/begin|complete`、`POST /api/v1/auth/logout`
  - 管理：`PUT /api/v1/admin/password`、`/admin/passkeys*`
  - 业务：`GET /api/v1/status`、`GET /api/v1/widget/summary`、`GET|PUT /api/v1/config`
  - 账号：`GET /api/v1/accounts/{id}/history`、`POST /api/v1/accounts/refresh`、`POST /api/v1/accounts/{id}/refresh`、`POST /api/v1/accounts/{id}/actions/{action}`
  - 任务：`GET /api/v1/jobs/{id}`
  - 日志：`GET|DELETE /api/v1/logs`
  - 通知测试：`POST /api/v1/notifications/test/{channel}`
  - API Key：`GET|POST /api/v1/api-keys`、`DELETE /api/v1/api-keys/{id}`
  - 兼容：`GET /monitor.php?key=...`（需 `cron:run` scope）
  - 其它：`/` 静态资源（SPA 回退到 `index.html`）
- 鉴权：`require(scope, handler)` 同时支持 Session Cookie（管理员）与 `Authorization: Bearer` / `X-API-Key`（scope 授权）；管理员非 GET 请求需 CSRF（`X-CDT-CSRF` 与 `cdt_csrf` Cookie）。
- 安全：安全响应头（CSP、X-Frame-Options 等）、panic 恢复、登录/初始化限流（内存窗口）、Session/CSRF Cookie（HttpOnly/SameSite=Strict）。
- Passkey：WebAuthn 注册与登录，挑战经内存 `passkeys` map 保存并 5 分钟过期，仅 HTTPS 可用。
- 辅助函数：`decodeJSON`（1MB 限制、拒绝未知字段）、`writeJSON`/`writeError`、`applyConfigDefaults`、`scrubConfig`（剥离所有敏感字段）、`latestRelease`（GitHub 版本检查）。

### 4.7 `internal/aliyun/client.go`

`Provider` 接口 + 自实现阿里云 RPC 客户端（HMAC-SHA1 签名，不依赖官方 SDK）：

- `GetTraffic`：`ListCdtInternetTraffic`（host `cdt.aliyuncs.com`），按 `trafficClass` 区分国内（`cn-*` 且非 `cn-hongkong`）与海外，聚合 `TrafficDetails` 并换算 GB；45 秒内存缓存。
- `GetInstanceStatus`：`DescribeInstanceStatus`；`ControlInstance`：`StartInstance` / `StopInstance`（`StoppedMode` 支持 `KeepCharging` / `StopCharging`）。
- 账单：`GetAccountBalance`（`QueryAccountBalance`）、`GetInstanceBill`（`DescribeInstanceBill`）；`bssEndpoint` 区分中国站 / 国际站 host。
- `call`：最多 3 次重试，指数退避 + 抖动；5xx / 429 / 限流可重试，4xx 鉴权错误不重试。

### 4.8 `internal/notify/service.go`

- `EnabledChannels`：根据配置返回启用通道。
- `Send` 分发到：`sendEmail`（SMTP，支持 SSL / STARTTLS、HTML 邮件模板）、`sendTelegram`（自定义反代 URL 或 SOCKS5 代理）、`sendWebhook`（GET/POST、JSON/Form、自定义 Headers、钉钉签名、模板变量替换）。
- 模板变量：`#TITLE#`、`#MSG#`、`#ACCOUNT#`、`#ACCOUNT_ID#`、`#TRAFFIC#`、`#MAX_TRAFFIC#`、`#INSTANCE#`、`#STATUS#`、`#TYPE#`、`#CREATED_AT#` 等。

### 4.9 `internal/web/web.go`

通过 `//go:embed dist/*` 嵌入前端构建产物，`FS()` 返回 `dist` 子文件系统供 HTTP 服务与 SPA 回退使用。

## 5. 前端 `web/`

| 文件 | 职责 |
| --- | --- |
| `src/main.tsx` | React 18 `createRoot` 挂载，StrictMode |
| `src/App.tsx` | 主应用。包含 `App`、`SetupWizard`（安装向导）、`Login`、`Dashboard`、`AccountCard`、各类设置面板（`GeneralSettings`/`AccountSettings`/`NotificationSettings`/`APIKeySettings`/`LogSettings`/`AdminSettingsPanel`/`AboutSettings`）、`HistoryModal` 及大量 UI 组件（`Metric`、`ElegantSelect`、`Segmented`、`ToastStack` 等） |
| `src/HistoryChart.tsx` | 基于 Recharts 的流量图表：`hourly` 用折线、`daily` 用柱状；按时间槽补全空点，响应式刻度 |
| `src/api.ts` | `api<T>()` 统一请求封装（自动带 CSRF、同源 Cookie、`APIError`）、`waitForJob` 轮询、`fetchLatestReleaseFromGitHub` |
| `src/types.ts` | `Account`/`Config`/`AccountSummary`/`History`/`APIKeyRecord` 等类型，`emptyAccount()` 与 `defaultConfig()` |
| `src/styles.css` | 全局样式（自适应桌面与移动端） |
| `tests/*.spec.ts` | Playwright E2E：安装/登录、品牌位置、多视口布局、刷新全部、更新检查、账单与历史精度 |
| `vite.config.ts` | React 插件；构建输出到 `../internal/web/dist`，`target: es2022`，关闭 sourcemap |
| `index.html` | 入口 HTML（`zh-CN`、主题色、icon、`#root`） |

## 6. 数据模型（SQLite 表）

| 表 | 用途 |
| --- | --- |
| `schema_migrations` | 迁移版本记录 |
| `settings` | 全局设置键值对（含加密后的敏感项） |
| `accounts` | 阿里云账号 / 地域 / 实例 / 流量上限 / 定时计划 / 站点类型；软删除 `deleted_at` |
| `logs` | info / warning / error / audit / heartbeat 日志，含 `(type, created_at)` 索引 |
| `login_attempts` | 登录失败计数（IP + 时间索引） |
| `traffic_hourly` / `traffic_daily` | 小时 / 日流量统计，唯一键 `(account_id, recorded_at)` |
| `billing_cache` | 余额、实例账单等 BSS 缓存，唯一键 `(account_id, cache_type, billing_cycle)` |
| `sessions` | 管理员登录会话（存 Token 哈希） |
| `api_keys` | API Key（存 SHA-256 哈希、scopes、过期、撤销） |
| `passkeys` | WebAuthn 凭据 |
| `jobs` | 任务队列（含 `unique_key` 去重、重试次数、`locked_at`） |
| `action_events` | 动作幂等事件（`event_key` 主键） |
| `scheduler_leases` | 调度租约（防多进程重复执行） |
| `notification_outbox` | 通知发件箱（唯一键 `event_id + channel`） |

## 7. 运行时行为要点

- 状态查询只读 SQLite 快照，不在页面请求内等待阿里云 API。
- 刷新与手动控制返回 `202 + job`，由后台 Worker 执行；前端通过 `waitForJob` 轮询 `/api/v1/jobs/{id}`。
- 幂等与去重：调度租约、`unique_key`、`action_events` 事件键确保不重复开关机与重复阈值通知。
- 通知异步化：策略只写 Outbox，`notificationWorker` 负责发送、重试（指数退避）与去重。
- 数据清理：`Prune` 定期删除过期日志 / 统计 / 缓存 / Session / 已完成任务。
- 优雅退出：收到 SIGTERM 后关闭 HTTP 服务并停止后台任务。

## 8. CLI 与环境变量

| 命令 | 用途 |
| --- | --- |
| `serve` | 启动 Web、调度器和 Worker（默认） |
| `run-once` | 创建一轮监控任务并等待执行结束 |
| `migrate` | 执行数据库迁移并退出 |
| `version` | 输出版本、Commit 和平台 |
| `healthcheck` | 请求本机 `/healthz`，供容器健康检查 |

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `CDT_DATA_DIR` | `./data` | SQLite 与 `master.key` 目录 |
| `CDT_LISTEN` | `:8080` | HTTP 监听地址 |
| `CDT_WORKERS` | `4` | 后台任务并发数 |
| `TZ` | 系统时区 | 容器环境时区（业务时区以控制台设置为准） |

## 9. 构建与部署

- **Dockerfile**：多阶段构建（Node 构建前端 → Go `CGO_ENABLED=0` 静态编译 → `scratch` 运行），只复制二进制、前端产物、CA 证书与空 `/data`，以 `65532:65532` 非 root 运行，`EXPOSE 8080`。
- **docker-compose.yml**：单服务、端口 `43210:8080`、`cdt-data` 卷、`healthcheck` 调用 `cdt-monitor healthcheck`。
- **本地开发**：`cd web && npm ci && npm run build`，随后 `go test ./...` 与 `go run ./cmd/cdt-monitor serve --data ./tmp-data`。
- **生产构建**：前端 `npm run build` 后 `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o cdt-monitor ./cmd/cdt-monitor`。

## 10. 测试

- **Go 单元测试**（`go test ./...`）：
  - `security`：Argon2id 哈希校验、旧短密码升级、master key 持久化
  - `store`：旧密钥/密码迁移、账号 ID 稳定、API Interval 下限、API Key scope 与撤销、Session 过期、中断任务恢复、旧流量表迁移
  - `engine`：调度延迟补偿、跨午夜时间窗、阈值百分比、地域名、租约忙、账单缓存补齐
  - `aliyun`：流量分类、BSS 端点、响应聚合、单条账单项、Business Code 200
  - `notify`：Webhook 变量替换、钉钉签名、JSON/Form 模板
  - `httpapi`：安全头、Passkey 凭据列表、刷新全部入队
- **前端 E2E**：`web/tests/` 下的 Playwright 用例（需先启动服务，默认 `http://127.0.0.1:43212`）。

## 11. Android 小组件（`android-widget/`）

基于只读接口 `GET /api/v1/widget/summary`（`Authorization: Bearer <widget:read Key>`）的实验性原生小组件：

- `CdtApi` 负责网络请求；`CdtWidgetProvider` 实现桌面小组件刷新；`SecretStore` 用 Android Keystore 加密保存 API Key；`AppPreferences` 保存站点地址与选中实例；`SettingsActivity` / `WidgetConfigureActivity` 为配置界面。
- 通过 `.github/workflows/android-widget.yml` 手动触发构建，产出 debug/release APK、ABI 分包与 AAB（配置 keystore Secrets 后可签名）。
