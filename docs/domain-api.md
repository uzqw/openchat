# 领域模型与 API

> 定义组件职责、持久化模型、任务状态机、恢复规则和首版 REST API。

## 1. 组件职责

### 1.1 前端

技术栈沿用 React 19、TypeScript、Vite、Tailwind、shadcn 风格。

首版页面：

- `/`：当前活跃 Gemini 会话及回答。
- `/history`：只读历史会话。
- `/settings`：Bridge、Gemini 登录状态和可用模型。

前端通过轮询 turn 状态更新 Gemini 任务；不直接访问 PocketBase collection CRUD，不直接访问 OpenCLI daemon。

### 1.2 Go/PocketBase 后端

职责：

1. 托管前端静态资源和业务 REST API。
2. 持久化 conversation、turn、task。
3. 通过 `exec.CommandContext` 调本机 OpenCLI。
4. 维护 Gemini operation queue。
5. 将 OpenCLI 输出归一化为平台任务状态。
6. 提供分层健康检查。

v1 直接实现 Gemini runner，不建立通用 provider registry、插件系统或五家适配器骨架。仅保留一个便于测试替换子进程的最小命令执行边界。

### 1.3 Provider operation queue

- Gemini 使用一个 FIFO operation queue。
- `doctor` 和 `ask/new/models/login/status/whoami` 全部串行。
- active conversation 一旦有成功 turn，除后续 `ask` 外不再执行任何 OpenCLI operation；`whoami/login/models/doctor` 会导航或改动共享 tab，只允许在启动期或该阶段之前执行。
- 队列对齐 OpenCLI persistent site session，避免后台探测改变多轮上下文。
- v1 单后端实例；不实现通用 provider 调度或分布式锁。

---

## 2. 数据模型

使用 PocketBase/SQLite，沿用系统 `id`、`created`、`updated` 字段，不重复创建 `created_at/updated_at`。

### 2.1 `conversations`

| 字段 | 类型 | 说明 |
|---|---|---|
| `title` | text | 默认由首个 prompt 截断生成 |
| `status` | select | `active` / `archived` |
| `remote_id` | text | Gemini 远端会话 id（首轮成功后从 `gemini status` 的 URL 捕获）；空 = 不可续聊 |

数据库使用 partial unique index 保证最多一条 `active`，不能只靠进程内检查。

### 2.2 `turns`

| 字段 | 类型 | 说明 |
|---|---|---|
| `conversation` | relation | 所属本地会话 |
| `prompt` | text | 本轮用户输入 |
| `idempotency_key` | text | 与 conversation 组成 composite unique index，防止客户端重试重复创建 Gemini 写任务 |

一个 turn 表示一次用户提交及其 Gemini 执行结果。同一 conversation 最多有一个未进入终态的 turn；只有上一轮当前 task 成功后才能继续追问。

### 2.3 `tasks`

| 字段 | 类型 | 说明 |
|---|---|---|
| `turn` | relation | 所属提问轮次 |
| `retry_of` | relation→tasks | 手动重试来源；首次执行为空 |
| `requested_model` | text | 用户请求的模型；空表示沿用网站当前模型 |
| `resolved_model` | text | 实际确认的模型；无法确认时为空 |
| `thinking` | select | 空 / `standard` / `extended`；空表示不改变网站当前值 |
| `status` | select | 见 3.2 |
| `result` | text | 完整回答；任务是唯一事实来源 |
| `error_code` | text | 稳定错误码 |
| `error_message` | text | 脱敏后的用户可见信息 |
| `unknown_acknowledged_at` | date | 用户确认 Chrome 已空闲后解除隔离 |
| `latency_ms` | number | 端到端耗时 |

首版不保存恒定为 `gemini` 的 provider 字段，也不增加 `messages` 表。`GET turn` 按 `created` 返回全部 task，并把最新 task 标为 `current_task`。

### 2.4 配置与 Gemini 状态

- timeout、队列上限、输出上限和专用 `OPENCLI_PROFILE` 使用环境变量。
- Bridge、登录、模型和 login operation 状态为运行时缓存；UI 轮询不能反复触发 OpenCLI 命令。
- login operation 状态为 `idle|queued|running|succeeded|failed`；只有 queued/running 阻止重复提交，后续登录可用新 queued 状态替换 terminal 状态。
- v1.8.7 的 `gemini models` 不提供可靠的 per-model thinking 能力；后端只校验枚举，由 `gemini ask` 在发送前尝试选择，不能用 fake 数据声称能力已发现。
- 存在未确认的 `unknown_outcome` 时 Gemini 为 `quarantined`，暂停所有 OpenCLI operation（包括后台状态刷新和登录）；GET 只返回缓存。
- Stage 3 再增加 Gemini 远端会话 ID/URL 和恢复状态。

---

## 3. 任务、重试与恢复

### 3.1 创建与队列准入

```text
预留 Gemini 队列容量
   │
   ├─ 单事务创建 turn 和 task
   ├─ 提交后入队
   └─ 任一步失败则释放容量
```

队列满时返回 `429` 且不得创建 turn/task。每个已提交的 `pending` task 必须有对应的容量预留。并发创建 conversation、创建 turn 和归档操作必须由数据库约束及事务保证不产生双 active 或写入刚归档的会话。

### 3.2 状态机

```text
pending → running → succeeded
   │              ├→ failed
   │              ├→ auth_required
   │              └→ unknown_outcome
   └→ canceled
```

- `pending`：等待 Gemini queue，可取消。
- `running`：OpenCLI 子进程已启动，不接受取消请求。
- `succeeded`：已获得并保存完整回答。
- `failed`：有结构化证据证明未提交或确定失败。
- `auth_required`：退出码 77，需要人工登录；若该会话已有成功 turn，则同时归档会话，登录后只能新建 conversation。
- `unknown_outcome`：可能已经提交，立即归档 active conversation 并隔离 Gemini。
- `canceled`：仅来自尚未开始的 pending task。

OpenCLI `gemini ask` 即使退出码为 0，也可能返回固定前缀 `💬 [NO RESPONSE] No Gemini response within`；只按该已知前缀识别 sentinel，并映射为 `unknown_outcome`，不能保存成成功回答。

### 3.3 重试与隔离

v1 不自动重试任何 Gemini ask。请求校验应在创建 task 前完成；task 进入执行后，只有 OS 明确报告子进程未启动等本地证据才能进入 `failed`，退出码和 stderr 不算 pre-dispatch 证明。子进程一旦启动，除合同明确的 `77 → auth_required` 外，未成功 ask 均进入 `unknown_outcome`。

- `failed`、`auth_required`、`canceled` 可由用户手动重试。
- 重试只允许 active conversation；因此已有成功 turn 后发生的 auth_required 不可重试。新 task 复制原 task 的 model/thinking，并带 `retry_of`。
- 首轮重试仍显式 `--new true`。
- `succeeded` 和 `unknown_outcome` 不可重试。
- 用户在可见 Chrome 确认没有生成/占用后，写入 `unknown_acknowledged_at` 解除 Gemini 隔离；task 状态仍保留 `unknown_outcome`，之后只能创建新 conversation。

### 3.4 重启恢复

v1 没有远端会话映射，因此启动时采取安全降级：

- 遗留 `running` 改为 `unknown_outcome` 并保持 Gemini 隔离。
- 遗留 `pending` 改为 `canceled`，不再入队。
- 遗留 active conversation 无条件归档。
- 未确认的 unknown 从数据库恢复隔离状态。

### 3.5 背压与进程边界

- 同时只执行一个 Gemini operation。
- Gemini 队列长度有限；满时返回 `429`，不无限积压。
- stdout/stderr 使用流式有限捕获；超过上限立即终止子进程，ask 进入 `unknown_outcome`，绝不截断后标记成功。
- 运行超时或进程被 kill 后保持 Gemini 隔离，直到用户显式确认 Chrome 已空闲。
- 最小调用间隔由 Stage 0 实测后配置，不预设没有依据的“30 次/分钟”。

---

## 4. REST API

首版使用业务 API，不开放 collection 的公共写权限。统一错误格式：

```json
{"error":{"code":"stable_code","message":"safe message"}}
```

| 方法 | 路径 | 成功 | 说明 |
|---|---|---:|---|
| `POST` | `/api/conversations` | `201` | 无 pending/running 且未隔离时归档旧 active，创建新会话 |
| `POST` | `/api/conversations/{id}/resume` | `200` | 归档当前 active 并重新激活目标会话；目标无 `remote_id` 返回 `409 conversation_not_resumable` |
| `GET` | `/api/conversations?page=&perPage=` | `200` | 按 `created desc` 返回历史列表 |
| `GET` | `/api/conversations/{id}` | `200` | turn 按 `created asc`，每轮 task 按 `created asc`；归档会话只读 |
| `POST` | `/api/conversations/{id}/turns` | `202` | 仅 active 且上一轮成功时创建 Gemini task；要求 `Idempotency-Key` |
| `GET` | `/api/turns/{id}` | `200` | 返回全部 task 和确定的 `current_task`，供前端轮询 |
| `POST` | `/api/tasks/{id}/retry` | `202` | 仅 failed/auth_required/canceled 且 conversation 仍 active |
| `POST` | `/api/tasks/{id}/cancel` | `200` | 仅取消 pending；其他状态返回 `409` |
| `GET` | `/api/providers/gemini` | `200` | 返回缓存的版本、Bridge、登录、模型、login operation 和隔离状态 |
| `POST` | `/api/providers/gemini/login` | `202` | 未隔离且 active 尚无成功 turn 时入队；仅 queued/running 重复操作返回 `409` |
| `POST` | `/api/tasks/{id}/acknowledge-unknown` | `204` | 确认该 unknown task 后；没有其他未确认 unknown 时解除隔离 |
| `GET` | `/api/health` | `200` | 只检查后端和 SQLite，不执行 OpenCLI 命令 |

`POST /turns` 请求示例：

```json
{
  "prompt": "比较这两个方案",
  "model": "2.5-flash",
  "thinking": "standard"
}
```

接口拒绝未知 JSON 字段。客户端不能提交 provider 名称、任意命令或 OpenCLI flags。校验失败返回 `400`，未认证返回 `401`，不存在返回 `404`，状态冲突返回 `409`，队列满返回 `429`。

`(conversation, Idempotency-Key)` 使用 composite unique index。认证、body 校验后必须先查重，再检查会话状态和队列容量：相同 key 且请求内容一致时返回原 turn，不创建 task；内容不一致返回 `409`。

## 5. 旧会话续聊（Stage 3）

### 5.1 远端会话捕获

新会话首轮 `gemini ask --new true` 成功后，runner 在同一队列操作内追加一次只读 `gemini status`，从返回的 `Url` 解析 `/app/<id>` 存入 `conversations.remote_id`。捕获是 best-effort：失败只意味着该会话保持只读。

### 5.2 恢复流程

`POST /api/conversations/{id}/resume` 归档当前 active 并重新激活目标会话（事务内完成，partial unique index 兜底）。恢复后该会话的首轮执行：

```text
gemini detail <remote_id>   # 导航 persistent tab 到该会话
  → gemini status           # 校验 URL 含目标 id，不匹配则中止（failed）
  → gemini ask <prompt>     # 不带 --new，在当前会话内发送
```

首轮三态：`首轮 && 无 remote_id → --new true`（新会话）；`首轮 && 有 remote_id → detail+ask`（恢复）；`非首轮 → 普通 ask`。

安全边界：

- 恢复前必须校验 `status` URL 与目标 id 一致，绝不盲发（防串线）。
- 无 `remote_id` 的旧会话拒绝续聊（`409 conversation_not_resumable`），因为无法定位 Gemini 远端会话。
- `detail` 会导航 shared tab，但它是用户主动发起的恢复操作，与「后台探测禁止导航」的规则不冲突。
- 恢复失败（detail 非零退出或 URL 不匹配）标记 `failed`（pre-dispatch，未提交任何内容），会话保持 active，重试会重跑完整恢复序列。
