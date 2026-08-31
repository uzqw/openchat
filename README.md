# OpenChat — 多站点问答（Gemini / Grok）

基于宿主机 OpenCLI（`@jackwener/opencli@1.8.7`）+ 可见 Chrome 的问答服务：
Go + PocketBase（SQLite）后端提供 FIFO 队列、会话/任务模型与 REST API，
React 19 + TypeScript + Vite + Tailwind 单列前端。**会话级站点**：每个会话在
创建时选定站点 provider（Gemini / Grok）并固定，恢复与追问都按会话自己的
站点执行；站点差异（子命令、能力、会话 URL）收敛在 `internal/opencli/site.go`。
环境变量 `OPENCLI_SITE` 只决定新建会话的默认站点（生产 = `grok`），历史
会话保留各自站点。

设计文档：`architecture.md`；接口与数据模型：`docs/opencli-contract.md`、
`docs/domain-api.md`；部署运维：`docs/deployment-operations.md`；路线：
`docs/roadmap.md`。

## 前置条件

- Go `>= 1.25`（本仓库 `go.mod` 声明 `go 1.25.0`）
- Node.js `>= 20`（构建 `web/`）
- 宿主机安装 `@jackwener/opencli@1.8.7`，且与 Browser Bridge Extension `v1.0.23` 匹配
- 可见 Chrome 已运行、扩展已连接；使用**专用 OpenCLI profile**（应用独占其 Adapter tab）
- 已在该专用 profile 的 Gemini 与 Grok Web 都人工登录一次（每账号首次需一次人工登录；
  两站点共用同一 profile 的不同 site tab）
- 后端建议使用**专用 OS 服务账号/HOME**，不与日常 OpenCLI 配置混用

> ⚠️ 平台能操作用户的真实 Gemini/Grok 账号：使用前请阅读「安全与注意事项」。

## 安装

```bash
# 后端
go build -o openchat-server ./cmd/server     # CGO_ENABLED=0 亦可
go build -o mcpserver ./cmd/mcpserver        # 只读 MCP 桥（见下）

# 前端（产物 web/dist 由后端静态托管，必须先构建）
cd web && npm ci && npm run build && cd ..
```

## 配置

后端完全由环境变量配置（样例见 `.env.example`，可复制为 `.env` 后
`set -a; . ./.env; set +a`）。缺失关键配置时**拒绝启动（fail closed）**。
必填项：`PB_DATA_DIR`（数据目录）、`OPENCLI_PROFILE`（专用 profile）；
非 loopback 监听还必须提供 `BASIC_AUTH_USER` / `BASIC_AUTH_PASS`、
`OPENCLI_TRUSTED_HOST`、`OPENCLI_TRUSTED_ORIGIN`。完整变量表见
[`docs/deployment-operations.md`](docs/deployment-operations.md) §4。

fail-closed 规则：非 loopback 监听缺少 Basic Auth 凭据、可信 Host 或可信
Origin 时拒绝启动；`OPENCLI_DEV_NO_AUTH` 只能用于 loopback；本地
`~/.opencli/clis/<site>` override、已安装 OpenCLI plugin 或版本 ≠ `1.8.7`
时写操作（ask/retry/login）一律拒绝。

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

## 编译与重新部署（生产）

### 编译

```bash
# 前端必须先行：后端静态托管 web/dist，不先 build 会托管旧产物
cd web && npm run build && cd ..

# 后端二进制输出到生产路径（CGO_ENABLED=0 亦可）
go build -o openchat-server ./cmd/server
```

### 部署到 grokbot

生产跑在 grokbot（10.0.0.2，WireGuard 内网）；公网入口 `chat.gostapi.com`
经本机 caddy → Authelia 前置 → WireGuard → grokbot `socat` →
`127.0.0.1:18090`。grokbot 沙箱无 systemd，用 setsid 常驻 +
watchdog 自愈，完整流程见 `.pi/skills/openchat-deploy/SKILL.md`。

场景 A：grokbot 本地改（当前主路径）

```bash
cd /workspace/wp/openchat
cd web && npm run build && cd ..     # 前端有改动时

# 旧二进制先备份（回滚用），再构建、重启
go build -o /opt/openchat-server.new ./cmd/server
mv /opt/openchat-server /opt/openchat-server.bak.$(date +%s)
mv /opt/openchat-server.new /opt/openchat-server
/opt/start-openchat.sh               # pkill 旧进程 → setsid nohup 常驻
```

场景 B：本机改好推过去

```bash
cd /home/ubuntu/wp/openchat
tar czf - --exclude=.git --exclude=node_modules --exclude=dist . \
  | ssh grokbot 'tar xzf - -C /workspace/wp/openchat'
ssh grokbot 'cd /workspace/wp/openchat && cd web && npm run build && cd .. \
  && go build -o /opt/openchat-server ./cmd/server && /opt/start-openchat.sh'
```

验证（grokbot 上）：

```bash
curl -s http://127.0.0.1:18090/api/health          # 后端与 SQLite
curl -s http://127.0.0.1:18090/api/providers       # 双站点快照；看 site / logged_in / models
curl -sI https://chat.gostapi.com/                 # 未登录应 302 → /authelia/
```

### 站点：双站点并存，会话级归属，不是全局切换

不再有「部署切换站点」。生产同时服务 Gemini 与 Grok：新建会话时由前端
选择站点（默认取 `OPENCLI_SITE`，生产 = `grok`）；每个会话带 `provider`
字段，恢复/追问在会话自己的站点上执行。`/api/providers` 返回双站点快照；
`POST /api/providers/{site}/login` 与 `/{site}/refresh` 按站点操作。

```bash
# 检查某站点登录态（两站点各自确认）
opencli --profile openchat-gemini gemini status --format json
opencli --profile openchat-gemini grok status --format json    # 应见 "Login": "Yes"
```

回滚：`mv /opt/openchat-server.bak.<时间戳> /opt/openchat-server && /opt/start-openchat.sh`。

## 外部 AI 接入（MCP，只读）

`cmd/mcpserver` 把会话历史暴露给其他 AI 智能体（Claude Code / Cursor /
Desktop）：stdio 传输的**只读**桥接，每个工具对应一个现有 GET 端点，
把转写渲染成 AI 友好的 Q/A 文本；刻意不暴露任何写操作。

```bash
go build -o mcpserver ./cmd/mcpserver
OPENCHAT_API_URL=http://127.0.0.1:8090 ./mcpserver   # 同机即可，无监听端口
```

可选环境变量：`OPENCHAT_API_USER`/`OPENCHAT_API_PASS`（API 若启用了
Basic Auth）、`OPENCHAT_API_TIMEOUT_SECONDS`（默认 120）。工具：
`list_conversations`（分页清单）、`get_conversation`（完整转写，用于
总结）、`get_turn`（单轮详情）。AI 客户端配置：

```json
{"mcpServers": {"openchat-history": {"command": "/opt/openchat/bin/mcpserver", "args": []}}}
```

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
