# OpenChat — Gemini 单 Provider v1

基于宿主机 OpenCLI（`@jackwener/opencli@1.8.7`）+ 可见 Chrome 的 Gemini 问答服务：
Go + PocketBase（SQLite）后端提供 FIFO 队列、会话/任务模型与 REST API，
React 19 + TypeScript + Vite + Tailwind 单列前端。第一版仅实现 Gemini，
不引入多 provider 抽象。

设计文档：`architecture.md`；接口与数据模型：`docs/opencli-contract.md`、
`docs/domain-api.md`；部署运维：`docs/deployment-operations.md`；路线：
`docs/roadmap.md`。

## 前置条件

- Go `>= 1.25`（本仓库 `go.mod` 声明 `go 1.25.0`）
- Node.js `>= 20`（构建 `web/`）
- 宿主机安装 `@jackwener/opencli@1.8.7`，且与 Browser Bridge Extension `v1.0.23` 匹配
- 可见 Chrome 已运行、扩展已连接；使用**专用 OpenCLI profile**（应用独占其 Adapter tab）
- 已在该专用 profile 的 Gemini Web 人工登录（每账号首次需一次人工登录）
- 后端建议使用**专用 OS 服务账号/HOME**，不与日常 OpenCLI 配置混用

> ⚠️ 平台能操作用户的真实 Gemini 账号：使用前请阅读「安全与注意事项」。

## 安装

```bash
# 后端
go build -o openchat-server ./cmd/server     # CGO_ENABLED=0 亦可

# 前端（产物 web/dist 由后端静态托管，必须先构建）
cd web && npm ci && npm run build && cd ..
```

## 配置

后端完全由环境变量配置（样例见 `.env.example`，可复制为 `.env` 后
`set -a; . ./.env; set +a`）。缺失关键配置时**拒绝启动（fail closed）**：

| 变量 | 默认 | 必填 | 说明 |
|---|---|---|---|
| `PB_DATA_DIR` | — | ✅ | PocketBase 数据目录（显式路径；启动以 0700 权限创建；测试须用临时目录） |
| `OPENCLI_PROFILE` | — | ✅ | 专用 OpenCLI profile，每次调用显式传入 |
| `OPENCLI_PATH` | `opencli` | | opencli 可执行文件路径 |
| `OPENCLI_LISTEN_ADDR` | `127.0.0.1:8090` | | 监听地址 |
| `BASIC_AUTH_USER` / `BASIC_AUTH_PASS` | — | 非 loopback | 全局 Basic Auth（常量时间比较） |
| `OPENCLI_TRUSTED_HOST` | — | 非 loopback | 可信 Host（忽略端口/大小写） |
| `OPENCLI_TRUSTED_ORIGIN` | — | 非 loopback | 可信 Origin，逗号分隔（写请求校验） |
| `OPENCLI_QUEUE_CAPACITY` | `1` | | FIFO 队列容量，满则 `429` 且事务内不留记录 |
| `OPENCLI_TIMEOUT_SECONDS` | `300` | | Gemini ask 超时（kill 上限，同时传给 `opencli --timeout`） |
| `OPENCLI_MAX_STDOUT_BYTES` / `OPENCLI_MAX_STDERR_BYTES` | `4MiB` / `1MiB` | | stdout/stderr 有限捕获上限，超限立即终止进程 |
| `OPENCLI_PROBE_TIMEOUT_SECONDS` | `120` | | 单条探针命令的 kill 上限 |
| `OPENCLI_CACHE_TTL_SECONDS` | `120` | | provider 缓存过期时间 |
| `OPENCLI_REFRESH_INTERVAL_SECONDS` | `60` | | 后台刷新循环周期 |
| `OPENCLI_WEB_DIR` | `web/dist` | | 前端构建产物目录（后端静态托管；`""` 表示不托管） |
| `OPENCLI_DEV_NO_AUTH` | 关 | | 开发免鉴权：仅 loopback + 显式 `1`，禁止 wildcard 绕过 |

fail-closed 规则：非 loopback 监听缺少 Basic Auth 凭据、可信 Host 或可信
Origin 时拒绝启动；`OPENCLI_DEV_NO_AUTH` 只能用于 loopback；本地
`~/.opencli/clis/gemini` override、已安装 OpenCLI plugin 或版本 ≠ `1.8.7`
时 Gemini 写操作（ask/retry/login）一律拒绝。

## 启动

```bash
export PB_DATA_DIR=/var/lib/openchat/pb_data
export OPENCLI_PROFILE=openchat-gemini
export BASIC_AUTH_USER=... BASIC_AUTH_PASS=...
# （非 loopback 还需 OPENCLI_TRUSTED_HOST / OPENCLI_TRUSTED_ORIGIN）
./openchat-server            # 或 go run ./cmd/server
```

启动时自动完成：数据目录权限收紧 → 启动恢复（遗留 pending→canceled、
running→unknown 并隔离、active→archived）→ 版本/doctor 等探针 → 监听。
健康检查（只查后端与 SQLite，不跑 OpenCLI）：

```bash
curl -fsS http://127.0.0.1:8090/api/health
```

浏览器访问 `http://127.0.0.1:8090/`（受 Basic Auth 保护）。API 概览见
`docs/domain-api.md`；错误统一为
`{"error":{"code":"stable_code","message":"safe message"}}`。

## 测试

```bash
# 后端
go test ./...
go vet ./...
go build ./...

# 前端（在 web/ 下）
npm ci
npm run lint
npm test -- --run
npm run build
```

所有自动化测试绝不触碰真实 Gemini 账号：使用可执行 fake OpenCLI 注入
（`internal/opencli/fakeopencli`），覆盖成功、等待、退出码、超时、无效
JSON、超大 stdout/stderr、取消与恢复等场景；数据一律使用临时目录，不碰
真实 `pb_data`。

## 人工 Gemini smoke

发送真实 prompt 到真实 Gemini 账号的冒烟由 `scripts/smoke-gemini.sh`
执行，**必须显式 opt-in**：

```bash
LIVE_GEMINI_SMOKE=1 OPENCLI_PROFILE=openchat-gemini scripts/smoke-gemini.sh
```

脚本依次验证：`opencli --version`（固定 1.8.7）→ `doctor` → `gemini
status/whoami` → `models` → 新会话第一轮 `ask --new true` → 同一会话第二
轮追问（确认上下文生效）。纪律与后端一致：

- 未设 `LIVE_GEMINI_SMOKE=1` 时拒绝运行；即使自动发现 Chrome/登录态也不
  会默认发送真实 prompt。
- 不自动退出登录、不制造 timeout、不改变账号安全状态。
- `auth_required` 只允许在另一专用**未登录** profile 下以
  `LIVE_GEMINI_AUTH_SMOKE=1` 单独人工验证，且只检查 `whoami` 退出码 77。
- timeout/kill 场景由 fake OpenCLI 测试覆盖（`internal/runner`），不在此脚本内制造。
- 若环境不具备条件（未 opt-in、无可见 Chrome 或无登录态），请勿运行脚本，
  如实记录 `live Gemini smoke: not run` 及原因；绝不伪造结果。
- 脚本不打印 cookie / profile 内容 / 敏感路径。

## 安全与注意事项

- UI/API 全局 Basic Auth，仅 `/api/health` 显式放行；PocketBase 管理端与
  系统路由被保护/禁用；生产传输层用 HTTPS/VPN（Basic Auth 不得裸露在公网明文 HTTP）。
- 专用 profile 的 OpenCLI Adapter tab 由本应用独占；除登录和故障确认外不
  人工导航，也不被其他 OpenCLI 客户端使用。
- 平台不复制、不导出 Chrome cookie；数据目录与备份 owner-only。
- 使用真实 Gemini 前请知悉：消耗 Gemini 官方额度，且违反网站 ToS 的自动
  化操作存在封号风险；锁定的 OpenCLI/Extension 版本只能防依赖漂移，不能
  防网站 DOM 漂移。
