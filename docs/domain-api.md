# 领域模型与 API

> 定义组件职责、持久化模型、任务状态机、恢复规则和首版 REST API。

## 1. 组件职责

### 1.1 前端

技术栈沿用 React 19、TypeScript、Vite、Tailwind、shadcn 风格。

首版页面：

- `/`：当前活跃会话，多 provider 并排回答。
- `/history`：只读历史会话。
- `/settings`：Bridge、provider 登录状态和可用模型。

前端通过轮询 turn 状态更新各列；不直接访问 PocketBase collection CRUD，不直接访问 OpenCLI daemon。

### 1.2 Go/PocketBase 后端

职责：

1. 托管前端静态资源和业务 REST API。
2. 持久化 conversation、turn、provider task。
3. 通过 `exec.CommandContext` 调本机 OpenCLI。
4. 维护每 provider operation queue。
5. 将 OpenCLI 输出归一化为平台任务状态。
6. 提供分层健康检查。

v1 不需要为五家复制五份完整类层级。共享 runner 负责进程、JSON 和错误处理，provider 配置只描述命令差异；确实存在差异的 Kimi/模型选择再写小型专用逻辑。

### 1.3 Provider operation queue

- 每个 provider 一个 FIFO operation queue。
- 不同 provider 可并行。
- 同 provider 的 `ask/new/model/login/status` 全部串行。
- 队列对齐 OpenCLI persistent site session，也避免后台状态探测改变正在生成的 tab。
- v1 单后端实例；不实现分布式锁。

---

## 2. 数据模型

使用 PocketBase/SQLite，沿用系统 `id`、`created`、`updated` 字段，不重复创建 `created_at/updated_at`。

### 2.1 `conversations`

| 字段 | 类型 | 说明 |
|---|---|---|
| `title` | text | 默认由首个 prompt 截断生成 |
| `status` | select | `active` / `archived` |

约束：v1 同一时间只有一个 active conversation。

### 2.2 `turns`

| 字段 | 类型 | 说明 |
|---|---|---|
| `conversation` | relation | 所属本地会话 |
| `prompt` | text | 本轮用户输入 |

一个 turn 表示一次用户提交，是并排展示和“全部重问”的最小分组。

### 2.3 `provider_tasks`

| 字段 | 类型 | 说明 |
|---|---|---|
| `turn` | relation | 所属提问轮次 |
| `provider` | select | 五家之一 |
| `requested_model` | text | 用户请求的模型；空表示沿用网站当前模型 |
| `resolved_model` | text | 实际确认的模型；无法确认时为空 |
| `status` | select | 见 3.2 |
| `result` | text | 完整回答；任务是唯一事实来源 |
| `error_code` | text | 稳定错误码 |
| `error_message` | text | 脱敏后的用户可见信息 |
| `attempts` | number | 安全执行尝试次数 |
| `latency_ms` | number | 端到端耗时 |

首版不另建 `messages` 表，避免 `tasks.result` 与 `messages.content` 双写不一致。需要附件、system message 或旧会话续聊时再扩展。

### 2.4 配置与 Provider 状态

- timeout、队列上限、最小调用间隔等先用环境变量。
- Bridge/登录/模型状态为运行时缓存，按需刷新，不建通用 key/value `config` 表。
- Stage 3 增加 `conversation_provider_threads`，保存远端会话 ID/URL 和恢复状态。

---

## 3. 任务、重试与恢复

### 3.1 创建流程

```text
POST turn
   │
   ├─ 原子创建 turn
   ├─ 为所选 provider 创建 task
   └─ 各 task 进入对应 provider queue
```

### 3.2 状态机

```text
pending → running → succeeded
                  ├→ failed
                  ├→ auth_required
                  ├→ unknown_outcome
                  └→ canceled
pending ─────────────→ canceled
```

- `pending`：等待 provider queue。
- `running`：OpenCLI 子进程已启动。
- `succeeded`：已获得并保存完整回答。
- `failed`：确定没有成功，或不可重试错误。
- `auth_required`：退出码 77，需要人工登录。
- `unknown_outcome`：可能已经提交，但无法确认结果，禁止自动重发。
- `canceled`：只保证取消未开始任务；运行中取消可能变为 `unknown_outcome`。

### 3.3 重试规则

写操作不是天然幂等。只允许自动重试“确定尚未提交”的错误，例如：

- 子进程启动失败。
- 执行前发现 Bridge 不可用。
- 明确的 session busy，且旧任务仍可确认。

以下情况不自动重发：

- 等待回复超时。
- 子进程被 kill 或上下文取消。
- daemon 返回结果未知。
- 已点击发送但没有拿到最终内容。

此类任务进入 `unknown_outcome`，由用户确认后手动创建新的 task。

### 3.4 重启恢复

后端启动时：

- 按 `created` 顺序重新入队 `pending`。
- 将遗留 `running` 改为 `unknown_outcome`，不自动重发。
- `auth_required` 保持原状态，等待登录后人工重试。
- active conversation 若无法验证浏览器上下文，归档为只读。

### 3.5 背压

- 每 provider 同时只执行一个 operation。
- 每 provider 设置有限队列长度；满时返回 `429`，不无限积压。
- 最小调用间隔由 Stage 0 实测后配置，不预设没有依据的“30 次/分钟”。
- prompt、stdout、stderr 和 result 均设置大小上限。

---

## 4. REST API

首版使用业务 API，不开放 collection 的公共写权限。

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/api/conversations` | 归档旧 active conversation，创建新会话 |
| `GET` | `/api/conversations` | 历史列表 |
| `GET` | `/api/conversations/{id}` | 会话、turn 和 task 结果；归档会话只读 |
| `POST` | `/api/conversations/{id}/turns` | 仅 active conversation 可提交；创建一轮多 provider 任务 |
| `GET` | `/api/turns/{id}` | 一次返回本轮所有 provider 状态，供前端轮询 |
| `POST` | `/api/tasks/{id}/retry` | 用户确认后的手动重试，生成新 task |
| `POST` | `/api/tasks/{id}/cancel` | 取消 pending task |
| `GET` | `/api/providers` | Bridge、登录、模型与能力状态 |
| `POST` | `/api/providers/{name}/login` | 入队异步登录 operation，在可见 Chrome 中人工完成 |
| `POST` | `/api/providers/{name}/model` | 对支持的 provider 切换模型 |
| `GET` | `/api/health` | 后端自身健康；不执行五家网站探测 |

`POST /turns` 请求示例：

```json
{
  "prompt": "比较这两个方案",
  "providers": ["chatgpt", "gemini", "deepseek"],
  "models": {
    "gemini": "2.5-flash",
    "deepseek": "expert"
  }
}
```

接口应在单个事务中创建 turn 和全部 task，避免只创建部分 provider 任务。
