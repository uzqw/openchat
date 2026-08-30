# 部署与运维

> 主方案为宿主机 OpenCLI + 可见 Chrome；本文同时定义安全和运维边界。

## 1. 部署拓扑

### 1.1 已确认主方案：宿主机 OpenCLI + 可见 Chrome

```text
┌────────────────────────────┐
│ LAN 浏览器 / React Web UI   │
└─────────────┬──────────────┘
              │ HTTPS / REST
              ▼
┌─────────────────────────────────────────────────────┐
│ 宿主机 Go + PocketBase（单进程、单实例）             │
│  - API / 静态前端                                    │
│  - conversations / turns / tasks                    │
│  - Gemini operation queue                           │
│  - os/exec.CommandContext                           │
└─────────────┬───────────────────────────────────────┘
              │ 启动同一宿主机上的 opencli 子进程
              ▼
┌─────────────────────────────────────────────────────┐
│ @jackwener/opencli v1.8.7                           │
│  localhost:19825 daemon ↔ Extension v1.0.23         │
└─────────────┬───────────────────────────────────────┘
              ▼
       可见 Chrome（专用 OpenCLI profile）
              ▼
          Gemini Web
```

此方案成立的原因：

- `exec.Command` 启动的是同一宿主机上的 OpenCLI，可执行边界真实存在。
- 用户能直接看到并操作 provider 登录页面。
- 使用宿主机上的专用 Chrome/OpenCLI profile，无需在容器中维护扩展、显示服务器和登录入口。
- OpenCLI daemon 继续只绑定 localhost。

### 1.2 部署前置条件

- Node.js `>=20`。
- 后端使用专用 OS 服务账号/HOME，不与日常 OpenCLI 配置混用。
- `@jackwener/opencli@1.8.7`，启动探针必须校验版本。
- `~/.opencli/clis/gemini` 不存在且没有已安装 OpenCLI plugin；否则拒绝 Gemini 写操作。
- 与该 release 匹配的 Browser Bridge Extension v1.0.23，Bridge 探针必须校验版本。
- Chrome/Chromium 处于运行状态且扩展已连接。
- 配置专用 `OPENCLI_PROFILE`，每次调用显式指定；应用独占其 OpenCLI Adapter tab。
- 用户已在该专用 profile 的 Gemini Web 人工登录。
- Go 后端服务账号有权限执行 `opencli`，但不读取 Chrome profile 文件。

### 1.3 后置方案：远程 Runner

只有宿主机方案无法满足部署要求时，才增加 Node Runner 服务：

```text
Go backend → 私有 HTTP/Unix Socket → Node Runner → spawn opencli
```

Runner 必须命令白名单化，且 19825 仍不得暴露。该方案不属于 v1。

---

## 2. 安全边界

平台能操作用户的真实 Gemini 账号，不能沿用“无鉴权 LAN”作为默认值。

v1 上线条件：

- 应用始终启用 Basic Auth；另通过 Tailscale/VPN 或 HTTPS 反向代理保护传输。
- Basic Auth 不允许运行在明文公网 HTTP 上。
- 非 loopback 监听时，认证凭据、可信 Host 和可信 Origin 缺失必须拒绝启动。
- Go 后端只绑定受控接口；OpenCLI daemon 始终只在 loopback。
- 对 UI/API 应用全局认证，仅显式放行 health；保护或禁用 PocketBase 管理端和系统路由。
- 对写 API 检查 Origin/Host，并只接受 JSON。
- 不记录完整 cookie、profile 路径或未脱敏 stderr。
- OpenCLI 命令使用 `exec.CommandContext(name, args...)`，禁止 `sh -c`；子进程移除 `NODE_OPTIONS/NODE_PATH`。
- README 明示网站 ToS、额度消耗和封号风险。

---

## 3. 健康检查与运维

健康状态分层，避免一个笼统的 healthy（实现见 `internal/provider`）：

1. **Backend**：`GET /api/health`，只检查进程和 SQLite，不执行 OpenCLI 命令。
2. **Bridge/版本/登录**：启动期做一次带专用 profile 的 version/doctor/status/whoami/models 探针（`opencli --version` 必须为锁定的 v1.8.7，Extension 为 v1.0.23）；此后 `GET /api/providers/gemini` 一律返回缓存，**不能因 UI 轮询反复入队**。
3. **按需刷新**：探针只在启动时与用户点击「检测在线」（`POST /api/providers/gemini/refresh`）时运行，**无后台定时轮询**；active 有成功 turn 后暂停所有非 ask 的 OpenCLI operation，只能读缓存，避免导航 shared tab。
4. **Gemini 隔离**：任一 ask 进入 `unknown_outcome` 后立即归档 active conversation 并将 Gemini 置为持久化隔离；隔离期间所有 OpenCLI operation（含按需刷新与 login）暂停，GET 只返回缓存，直到用户在可见 Chrome 确认已停止生成并通过 `POST /api/tasks/{id}/acknowledge-unknown` 解除。
5. **Adapter functional**：仅显式 opt-in 的人工 smoke（`LIVE_GEMINI_SMOKE=1 scripts/smoke-gemini.sh`，含 doctor/status/whoami/models/首轮/追问），不能拿真实写操作做定时 healthcheck。

版本策略：

- OpenCLI 固定 v1.8.7。
- Extension 固定 v1.0.23，并作为同一升级单元记录。
- baseline 不打 patch。
- 升级时先在 Stage 0 合同测试中验证 Gemini 命令、输出字段、退出码、模型和登录。
- 网站改版可能让锁定版本失效；锁版本只防依赖漂移，不能防 DOM 漂移。

备份：

- PocketBase 数据目录和备份使用 owner-only 权限（`cmd/server` 启动即以 `0700` 创建/收紧）。
- Chrome 登录态由用户的专用 OpenCLI profile 管理，平台不复制、不导出 cookie。
- 恢复数据库不会自动恢复远端网页上下文，旧会话按只读处理。

## 4. 配置与启动

后端完全由环境变量配置（样例 `.env.example` 仅含安全占位符）。缺失关键配置时**拒绝启动（fail closed）**，加载与校验见 `internal/api/config.go`：

| 变量 | 默认 | 必填 | 说明 |
|---|---|---|---|
| `PB_DATA_DIR` | — | ✅ | PocketBase 数据目录（显式路径；启动以 0700 权限创建；测试必须用临时目录，绝不指向生产 `pb_data`） |
| `OPENCLI_PROFILE` | — | ✅ | 专用 OpenCLI profile，每次调用显式传入 |
| `OPENCLI_PATH` | `opencli` | | opencli 可执行文件路径 |
| `OPENCLI_LISTEN_ADDR` | `127.0.0.1:8090` | | 监听地址 |
| `BASIC_AUTH_USER` / `BASIC_AUTH_PASS` | — | 非 loopback | 全局 Basic Auth（常量时间比较） |
| `OPENCLI_TRUSTED_HOST` | — | 非 loopback | 可信 Host（比较忽略端口/大小写） |
| `OPENCLI_TRUSTED_ORIGIN` | — | 非 loopback | 可信 Origin，逗号分隔（写请求校验） |
| `OPENCLI_QUEUE_CAPACITY` | `1` | | FIFO 队列容量，满则 `429` 且事务内不留记录 |
| `OPENCLI_TIMEOUT_SECONDS` | `300` | | Gemini ask 超时（kill 上限，同时传给 `opencli --timeout`） |
| `OPENCLI_MAX_STDOUT_BYTES` / `OPENCLI_MAX_STDERR_BYTES` | `4MiB` / `1MiB` | | stdout/stderr 有限捕获上限，超限立即终止进程，ask 不做截断成功 |
| `OPENCLI_PROBE_TIMEOUT_SECONDS` | `120` | | 单条探针命令的 kill 上限 |
| `OPENCLI_CACHE_TTL_SECONDS` | `120` | | provider 缓存过期时间 |
| `OPENCLI_WEB_DIR` | `web/dist` | | 前端构建产物目录（后端静态托管；`""` 表示不托管） |
| `OPENCLI_DEV_NO_AUTH` | 关 | | 开发免鉴权：仅 loopback + 显式 `1`，禁止 wildcard 绕过 |

fail-closed 规则：非 loopback 监听缺少 Basic Auth 凭据、可信 Host 或可信
Origin 时拒绝启动；`OPENCLI_DEV_NO_AUTH` 只能用于 loopback；本地
`~/.opencli/clis/gemini` override、已安装 OpenCLI plugin 或版本 ≠ `1.8.7`
时 Gemini 写操作（ask/retry/login）一律拒绝（`internal/provider/guard.go`）。

### 构建与启动

```bash
# 后端（CGO_ENABLED=0 亦可）
go build -o openchat-server ./cmd/server

# 前端：web/dist 需先构建，后端才会托管界面
cd web && npm ci && npm run build && cd ..

export PB_DATA_DIR=/var/lib/openchat/pb_data
# …其余环境变量按上表（非 loopback 必须带 BASIC_AUTH_* / TRUSTED_HOST / TRUSTED_ORIGIN）
./openchat-server            # 或 go run ./cmd/server
```

启动顺序：数据目录 0700 → 启动恢复（遗留 pending→canceled、running→
unknown 并隔离、active→archived；不自动重发）→ 版本/doctor 等探针 →
`GET /api/health` 可用。健康检查只查后端与 SQLite：

```bash
curl -fsS http://127.0.0.1:8090/api/health
```
