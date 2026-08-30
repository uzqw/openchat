# 实现提示词：Gemini 单 Provider 第一版

你是一名资深全栈工程师。请在当前仓库中**直接完成可运行、可测试的 Gemini 第一版**，不要只输出设计、伪代码或实施建议。持续工作到自动化验收全部通过；只有遇到必须由用户完成的 Gemini 人工登录时才说明具体操作。

## 任务目标

基于以下文档实现完整闭环：

1. `architecture.md`
2. `docs/opencli-contract.md`
3. `docs/domain-api.md`
4. `docs/deployment-operations.md`
5. `docs/roadmap.md`

若旧描述与本提示词冲突，以本提示词的 **Gemini-only v1** 范围为准，并同步修正文档。

第一版只实现 Gemini。不要实现 ChatGPT、Kimi、DeepSeek、Grok，也不要提前建立通用 provider 插件系统、provider registry、分布式队列、SSE 或远程 Runner。

## 强制技术边界

- 后端：Go + PocketBase v0.39.9 + SQLite，`CGO_ENABLED=0` 可构建。
- 前端：React 19 + TypeScript + Vite + Tailwind，使用简洁的 shadcn 风格组件。
- 运行方式：Go 后端和 OpenCLI 在同一宿主机；用户使用安装了 Browser Bridge Extension v1.0.23 的可见 Chrome。
- OpenCLI：固定 `@jackwener/opencli@1.8.7` 合同；版本或 Extension 不匹配时写操作 fail closed。
- 使用专用 OS 服务账号的 HOME；启动时拒绝 `~/.opencli/clis/gemini` 本地 override 和任何已安装 OpenCLI plugin，避免同版本号下 adapter 被替换。子进程使用最小环境并移除 `NODE_OPTIONS/NODE_PATH`。
- 使用专用 Chrome/OpenCLI profile；`OPENCLI_PROFILE` 必填，并显式传给每个命令。该 profile 的 OpenCLI Adapter tab 由本应用独占，除登录和故障确认外不得人工导航或被其他 OpenCLI 客户端使用。
- 调用方式：`exec.CommandContext` + 参数数组，禁止 `sh -c`。
- OpenCLI daemon 19825 只能保持 loopback，不能由应用代理或暴露。
- 生产环境对 UI 和 API 应用全局 Basic Auth；仅 `/api/health` 可列入显式公共白名单。保护或禁用 PocketBase `/_/` 和系统管理路由。HTTPS/VPN 由部署文档说明。开发免鉴权只能通过显式环境变量启用，并限制在 loopback。

遵循 YAGNI：只写 Gemini 当前需要的代码。为了自动化测试，可以保留一个最小命令执行边界用于注入 fake OpenCLI；不要为未来五家创建空接口和实现。

## 必须实现的功能

### 1. Gemini OpenCLI 合同

先核对目标机器上的实际帮助和输出，不要仅凭文档猜参数：

```bash
opencli --version
opencli --profile "$OPENCLI_PROFILE" doctor
opencli --profile "$OPENCLI_PROFILE" gemini ask --help
opencli --profile "$OPENCLI_PROFILE" gemini models --format json
opencli --profile "$OPENCLI_PROFILE" gemini status --format json
opencli --profile "$OPENCLI_PROFILE" gemini whoami --format json
```

业务调用必须统一请求 JSON 输出，并支持：

- 新会话第一次提问。
- 当前活跃会话连续追问。
- `model` 可选值。
- `thinking=standard|extended`，空表示不改变网站当前值。v1.8.7 的 `gemini models` 不提供可靠的 per-model thinking 能力，因此只校验枚举并让 `gemini ask` 在发送前尝试选择；不得用 fake 能力矩阵冒充真实发现。
- 可配置 timeout。
- `login/status/whoami/models`。

实现健壮的 JSON 解析和流式大小限制。Gemini 结果中的展示前缀只能在确认是 OpenCLI 固定包装时移除，不能破坏 Markdown 正文。OpenCLI v1.8.7 可能以退出码 0 返回固定前缀 `💬 [NO RESPONSE] No Gemini response within`；只按该已知前缀识别 sentinel（不能误伤正文中的同名文本），并标记 `unknown_outcome`，绝不能保存为成功回答。

v1 不自动重试 Gemini ask。退出码和 stderr 单独不足以证明是否已经发送：

- `0` 且存在非 sentinel 的完整 response：成功。
- `77`：按锁定合同标记 `auth_required`。
- 请求校验必须在 task 创建前完成；执行中的 `failed` 只允许 OS 明确报告子进程未启动等本地证据。
- 子进程一旦启动，除 `77` 外的所有未成功 ask（包括 `2/66/69/75/78/130`、无效 JSON、输出超限和进程中断）均标记 `unknown_outcome`。

stdout/stderr 必须通过 pipe 流式有限捕获，不能先写入无限 `bytes.Buffer` 再检查。超过上限立即终止进程；ask 不得截断后标记成功。原始 stderr 不返回浏览器，也不能记录 cookie、profile 内容或敏感路径。

### 2. 会话语义

- 同一时间只有一个 `active` conversation，并用数据库 partial unique index 强制保证。
- 有 pending/running task 或 Gemini 处于隔离状态时，拒绝创建新 conversation。
- 新 conversation 的第一次 Gemini 提问显式开启新网页会话。
- 后续 turn 沿用当前 Gemini persistent session，实现连续追问。
- 同一 conversation 同时最多一个未完成 turn；只有上一轮当前 task 成功后才允许继续追问。失败轮次必须重试，或放弃该会话并创建新 conversation。
- active conversation 一旦有成功 turn，只允许后续 ask；禁止 `whoami/login/models/doctor` 导航或改动共享 tab。
- 后续 turn 若返回 `auth_required`，立即归档 conversation；登录后只能创建新 conversation，不能重试该轮。只有尚无成功 turn 的会话可登录并重试首轮。
- archived conversation 只读，API 和 UI 都禁止继续提交。
- v1 没有远端会话映射，因此后端启动时无条件归档遗留 active conversation，避免串会话。
- v1 不实现返回旧 conversation 后继续追问。

### 3. 数据模型

使用 PocketBase migrations 创建：

#### `conversations`

- `title`
- `status`: `active|archived`

#### `turns`

- `conversation` relation
- `prompt`
- `idempotency_key`，与 conversation 建 composite unique index

#### `tasks`

- `turn` relation
- `retry_of` optional self relation
- `requested_model`
- `resolved_model`
- `thinking`: empty/standard/extended
- `status`
- `result`
- `error_code`
- `error_message`
- `unknown_acknowledged_at`
- `latency_ms`

状态必须覆盖：

```text
pending | running | succeeded | failed |
auth_required | unknown_outcome | canceled
```

使用 PocketBase 系统 `id/created/updated`，不要重复增加 `created_at/updated_at`。`tasks.result` 是回答的唯一事实来源，不增加重复保存内容的 messages 表。

### 4. Gemini FIFO 队列

- 一个进程内 FIFO operation queue。
- `doctor` 和 `ask/new/models/login/status/whoami` 全部串行。
- active conversation 有成功 turn 后暂停所有非 ask OpenCLI operation；provider GET 只读缓存，不允许后台 `whoami/models/doctor` 破坏 shared tab 上下文。
- 入队前预留容量，再在单个事务中创建 turn + task；队列满返回 `429` 且数据库不得留下记录。
- 不实现跨 provider 并行或分布式锁。
- 启动时把遗留 pending 改为 `canceled`，running 改为 `unknown_outcome`，不得自动重发。
- provider 状态接口读取缓存，不能因 UI 轮询反复入队；后台只在队列空闲、缓存过期且 active 尚无成功 turn 时刷新。
- pending task 可取消；running 取消请求返回 `409`，不要尝试 kill 后伪装成已取消。
- 任一 ask 进入 `unknown_outcome` 后立即归档 active conversation，并将 Gemini 置为持久化隔离状态。用户在可见 Chrome 确认已停止生成后，通过明确的 acknowledge 操作解除隔离；解除前暂停所有 OpenCLI operation（包括后台状态刷新和登录），GET 只能返回缓存。

### 5. REST API

实现并测试：

| 方法 | 路径 | 成功 | 行为 |
|---|---|---:|---|
| `POST` | `/api/conversations` | `201` | 无 pending/running 且未隔离时归档旧 active，创建新会话 |
| `GET` | `/api/conversations?page=&perPage=` | `200` | 按 `created desc` 返回历史列表 |
| `GET` | `/api/conversations/{id}` | `200` | turn 按 `created asc`，task 按 `created asc` |
| `POST` | `/api/conversations/{id}/turns` | `202` | 仅 active 且上一轮成功时创建；要求 `Idempotency-Key` |
| `GET` | `/api/turns/{id}` | `200` | 返回全部 task 和最新的 `current_task` |
| `POST` | `/api/tasks/{id}/retry` | `202` | 仅 failed/auth_required/canceled 且会话仍 active |
| `POST` | `/api/tasks/{id}/cancel` | `200` | 仅 pending；其他状态返回 `409` |
| `GET` | `/api/providers/gemini` | `200` | 返回缓存的版本、Bridge、登录、模型、login operation 和隔离状态 |
| `POST` | `/api/providers/gemini/login` | `202` | 未隔离且 active 尚无成功 turn 时入队；queued/running 或上下文冲突时 `409` |
| `POST` | `/api/tasks/{id}/acknowledge-unknown` | `204` | 确认该 unknown task；没有其他未确认 unknown 时解除隔离 |
| `GET` | `/api/health` | `200` | 只检查后端和 SQLite，不执行 OpenCLI 命令 |

创建 turn 的请求只接受：

```json
{
  "prompt": "问题",
  "model": "可选规范模型 ID",
  "thinking": "standard 或 extended，可选"
}
```

客户端不能提交 provider 名称、任意命令、任意 OpenCLI flags 或 shell 内容。拒绝未知 JSON 字段，并对 prompt、model、thinking 和 body 大小做边界验证。

统一错误 envelope：`{"error":{"code":"stable_code","message":"safe message"}}`。校验失败 `400`、未认证 `401`、不存在 `404`、状态冲突 `409`、队列满 `429`。

认证和 body 校验后，必须先按 `(conversation, Idempotency-Key)` 查重，再检查会话状态或预留队列容量。相同 key 且请求内容一致时返回原 turn；内容不一致返回 `409`。数据库用 composite unique index 兜底，绝不能创建第二个 Gemini 写任务。

重试创建带 `retry_of` 的新 task，复制 model/thinking；首轮重试仍使用 `--new true`。已有成功 turn 后的 auth_required 会归档 conversation，不允许登录后继续该网页上下文。`GET turn` 返回按创建时间排序的全部 task，并明确标出最新 `current_task`。`succeeded` 和 `unknown_outcome` 不允许重试。

### 6. Web UI

实现可用、响应式、具备基本无障碍能力的页面：

- `/`：当前会话、消息历史、Gemini 状态、模型/思考选择、输入框、pending/running/error/result 展示。
- `/history`：历史会话列表和只读详情。
- `/settings`：Backend、Bridge、Gemini 登录状态，模型列表和“去登录”操作。

行为要求：

- 提交后轮询整个 turn，而不是建立一个永久定时器。
- 到达终态或组件卸载时停止轮询。
- thinking 提供“不改变网站当前值”、standard、extended；不要显示伪造的 per-model 支持状态，真实选择失败按后端 task 状态展示。
- archived conversation 禁用输入并明确显示“只读历史”。
- `auth_required` 显示登录引导；若会话已有成功 turn，明确说明该会话已归档，登录后需新建会话。
- active 已有成功 turn 时禁用登录/刷新模型等会改动 shared tab 的操作。
- `unknown_outcome` 明确说明可能已经提交，归档当前会话并显示“确认 Chrome 已空闲”操作；不能直接重试。
- 保留 Gemini Markdown；渲染时防止 XSS。
- 提供 loading、空状态、键盘提交、可见 label 和错误提示。
- 第一版使用单列 Gemini UI，不显示 provider 多选或对比布局。

### 7. 安全和配置

至少支持以下配置，名称可按项目惯例微调，但必须记录在 README：

- OpenCLI 可执行文件路径，默认 `opencli`。
- 专用 OS 服务账号/HOME 和 `OPENCLI_PROFILE`，不得与日常 OpenCLI 环境混用。
- 监听地址、可信 Host 和可信 Origin。
- PocketBase 数据目录。
- Basic Auth 用户名和密码。
- Gemini timeout。
- 队列容量。
- stdout/stderr/result 最大字节数。
- 显式 loopback-only 开发免鉴权开关。

要求：

- 用户名和密码都使用常量时间比较。
- 非 loopback 监听时，Basic Auth 凭据、可信 Host 和可信 Origin 缺失或为空必须拒绝启动。
- 开发免鉴权只有在显式开启且监听地址为 loopback 时有效；禁止 wildcard 绕过。
- 对 UI/API 应用全局认证，仅显式放行 `/api/health`；保护或禁用 PocketBase `/_/` 和系统管理路由。
- 写 API 校验可信 Origin/Host 并只接受 JSON；不要盲目信任转发头。
- PocketBase collection 不开放公共写规则。
- 数据目录和备份使用 owner-only 权限；测试路径必须证明不解析到生产 `pb_data`。
- 不把凭证写入仓库、前端 bundle 或日志。
- 提供 `.env.example`，只包含安全占位符。

## 自动化测试要求

默认测试绝不能调用真实 Gemini 账号。创建可执行的 fake OpenCLI，覆盖成功、等待、退出码、超时、无效 JSON、超大 stdout/stderr 和取消场景。

### Go 测试至少覆盖

- Gemini 命令参数构造：不经过 shell、每次显式 profile、格式和版本合同正确；子进程环境不继承 `NODE_OPTIONS/NODE_PATH`。
- JSON 成功解析、`[NO RESPONSE]` sentinel、无效 JSON 和 thinking 枚举/参数传递；不得用 fake 测试声称 v1.8.7 可发现 per-model thinking 能力。
- stdout/stderr 流式限长；超限终止且 ask 不会产生截断成功。
- ask 子进程启动失败是 failed；子进程启动后的非 0/77 退出、坏 JSON 等均为 unknown。
- timeout/kill → unknown + Gemini 隔离，且不自动重发。
- FIFO 顺序、原子容量预留，以及 `429` 时数据库零新增。
- pending 可取消；running 取消返回 `409`。
- 启动恢复：pending → canceled、running → unknown、active → archived，并恢复隔离。
- partial unique index 和并发 API 测试保证单 active conversation。
- pending/running 时拒绝新 conversation；上一轮未成功时拒绝下一 turn。
- active 有成功 turn 后不会运行 whoami/login/models/doctor；后续 auth_required 会归档会话且不可重试。
- archived conversation 拒绝新 turn。
- composite `(conversation, Idempotency-Key)`：重放先于状态/容量检查、跨 conversation 可复用、body 冲突返回 `409`。
- 重试资格、`retry_of`、参数复制、首轮 `--new true` 和 current_task 选择。
- login operation 的 `idle|queued|running|succeeded|failed` 缓存状态和脱敏错误；仅 queued/running 重复提交 `409`，后续登录可替换 terminal 状态。
- acknowledge unknown 解除隔离；解除前所有写操作被拒绝。
- 全局 Basic Auth、PocketBase 管理路由保护、health 路由无冲突、Origin/Host、JSON content type 和输入边界。
- 非 loopback 缺少安全配置时 fail closed；开发免鉴权不能用于非 loopback。
- Gemini 本地 adapter override、任意 OpenCLI plugin 或版本不匹配时写操作 fail closed。
- 全部 REST API 的成功状态码、错误 envelope、排序和失败路径。

使用临时 PocketBase 数据目录，测试不能接触真实 `pb_data`。

### 前端测试至少覆盖

- 当前会话加载与提交。
- pending/running/succeeded 状态显示。
- auth_required 登录引导。
- unknown_outcome 归档会话、阻止直接重试，并提供“确认 Chrome 已空闲”操作。
- archived conversation 只读。
- 模型与 thinking 选择，且不伪造 per-model thinking 能力。
- 轮询在终态和卸载时停止。
- Markdown XSS 被清理。

### 集成检查

至少提供一个使用 fake OpenCLI 的后端集成测试，验证：

```text
创建 conversation
→ 创建 turn
→ task pending/running
→ fake Gemini 返回
→ task succeeded
→ API 返回持久化结果
```

### 必须执行的命令

根据最终目录调整命令，但交付前至少执行等价检查：

```bash
go test ./...
go vet ./...
go build ./...
npm ci
npm run lint
npm test -- --run
npm run build
```

所有检查必须通过。不要通过删除测试、降低断言、跳过错误处理或提交生成缓存来“修绿”。

## 真实 Gemini Smoke

提供一个不会泄露敏感信息的 `scripts/smoke-gemini.sh` 或等价命令，验证：

1. `opencli doctor`。
2. Gemini status/whoami。
3. models JSON。
4. 新会话第一轮。
5. 同一会话第二轮追问，确认上下文生效。

真实 smoke 必须显式设置 `LIVE_GEMINI_SMOKE=1`；即使自动发现 Chrome 和登录态也不得默认发送真实 prompt。脚本不得自动退出登录、制造 timeout 或改变账号安全状态。`auth_required` 只允许在另一个专用未登录 profile 下以 `LIVE_GEMINI_AUTH_SMOKE=1` 单独人工验证；timeout/kill 使用 fake OpenCLI 测试。

如果用户明确 opt-in 且当前环境有可见 Chrome 和登录态，实际运行并记录脱敏结果。如果环境不具备条件：

- 自动化测试仍必须全部通过。
- README 写清人工步骤。
- 最终报告明确写 `live Gemini smoke: not run` 及原因。
- 绝不能伪造 fixture 或声称真实测试通过。

## 实施顺序

1. 阅读仓库指令和上述设计文档。
2. 检查当前文件和依赖，复用已有骨架；不要重复造轮子。
3. 先完成 Gemini 合同探针和 fake OpenCLI 测试边界。
4. 实现 migrations、runner、队列、恢复和 API。
5. 实现单列 Gemini UI。
6. 补齐安全、README、`.env.example` 和 smoke 脚本。
7. 运行全部测试、lint、build，修复所有失败。
8. 对照本提示词逐项验收，删除死代码、占位实现和不需要的抽象。

不要在每个阶段等待确认；在不破坏安全和数据的前提下选择最简单的正确实现继续推进。只有不可逆产品决策或真实登录阻塞时才提问。

## 完成定义

只有同时满足以下条件才算完成：

- Gemini-only 端到端闭环可运行。
- 当前 active conversation 可以连续追问。
- 新 conversation 不继承旧上下文，旧 conversation 只读。
- 一个 conversation 不会并发或越过失败轮次发送后续 turn。
- unknown outcome 会归档会话并隔离 Gemini，不会继续发送。
- 任务状态、恢复、pending 取消和人工重试符合文档。
- UI 能处理所有终态和登录引导。
- 全局 Basic Auth、管理路由保护、fail-closed 配置和输入边界生效。
- 自动化测试不依赖真实账号且全部通过。
- 前后端 lint、test、build 全部通过。
- README 能让新环境完成安装、启动、测试和人工 Gemini smoke。
- 没有未解释的 TODO、空实现、其他 provider 脚手架或敏感信息。

## 最终回复格式

完成后只报告：

1. 实现了什么。
2. 关键文件。
3. 实际执行的测试命令及结果。
4. live Gemini smoke 是否运行；未运行则说明唯一阻塞原因。
5. 仍存在的真实限制，不写泛泛的“未来可优化”。

除非用户明确要求，否则不要创建 Git commit。
