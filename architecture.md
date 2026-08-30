# 统一 AI 聚合平台 — 架构总览

> 版本：v0.3
> OpenCLI 基线：`@jackwener/opencli` **v1.8.7**、Browser Bridge Extension **v1.0.23**
> 状态：第一版只做 Gemini；先完成 Stage 0 合同 PoC，再进入完整实现。

## 1. 目标

平台最终目标是通过用户已经登录的真实 Chrome，会聚多个 AI 网页版；**第一版只接入 Gemini**：

1. 提供一个受保护的 LAN Web UI。
2. 在当前活跃 Gemini 会话中连续追问。
3. 保存本地提问、完整结果、耗时和失败状态。
4. 展示 Bridge、Gemini 登录和模型能力状态。

所有 Gemini 网站读写都经过 OpenCLI；平台不直接调用 Gemini 云 API，也不直接操作网站 DOM。

## 2. v1 边界

- 单机、单用户、单后端实例。
- 只实现 Gemini；ChatGPT、Kimi、DeepSeek、Grok 后置。
- 当前只有一个 active conversation。
- 当前活跃会话支持连续追问；旧会话只读。
- v1 没有远端会话映射，因此后端启动时无条件归档遗留 active conversation。
- 使用任务轮询，不做 token 级流式输出。
- 不导入网站历史，不支持旧会话续聊、图片或附件。
- 使用专用 OS 服务账号/HOME 和 `OPENCLI_PROFILE`，应用独占其 OpenCLI Adapter tab。
- 不把无鉴权的 OpenCLI daemon 19825 端口暴露到 LAN。
- 不在没有实测故障前维护 OpenCLI patch。

旧会话续聊需要逐家验证远端会话 ID/URL 的保存和恢复，安排在 Stage 3。

## 3. 已确认部署方案

```text
LAN 浏览器 / React UI
          │ HTTPS / REST
          ▼
宿主机 Go + PocketBase（单进程）
  - 业务 API / 静态前端
  - conversation / turn / task
  - Gemini operation queue
          │ exec.CommandContext
          ▼
宿主机 @jackwener/opencli v1.8.7
          │ localhost:19825
          ▼
OpenCLI daemon ↔ Extension v1.0.23 ↔ 可见 Chrome
          ▼
Gemini Web
```

采用宿主机方案的原因：

- Go 可以真实执行同一宿主机上的 `opencli` 子进程。
- 用户能在可见 Chrome 中完成人工登录。
- 使用宿主机上的专用 Chrome/OpenCLI profile，无需维护容器显示服务器、扩展和 noVNC。
- daemon 保持 loopback，不新增无鉴权远程控制面。

远程 Node Runner 仅作为 v1 之后的备选方案。

## 4. 核心运行规则

### 4.1 OpenCLI 合同

- 所有支持结构化输出的业务调用显式使用 `--format json`，并使用参数数组，禁止 `sh -c`。
- 优先按 OpenCLI 退出码分类错误，stderr 只作脱敏诊断。
- Gemini 模型能力以锁定版本的 `gemini models` 和 `gemini ask` 为准，不维护臆测静态表。
- 禁止 Gemini 本地 adapter override 和 OpenCLI plugin，避免同版本号下实际执行代码被替换。
- 其他 provider 的合同只作后续研究依据，不进入第一版实现。

完整命令矩阵和错误合同见 [`docs/opencli-contract.md`](docs/opencli-contract.md)。

### 4.2 并发

- `doctor` 及 Gemini 的 `ask/new/models/login/status/whoami` 全部经过一个 FIFO operation queue。
- 同时只运行一个 Gemini operation。
- active conversation 有成功 turn 后只允许后续 ask；禁止 whoami/login/models/doctor 改动 shared tab。
- 队列长度有限；满时返回 `429`，不无限积压。
- 第一版不实现通用 provider registry 或跨 provider 调度。

### 4.3 会话

- 新建本地会话时归档旧 active conversation。
- Gemini 在新本地会话中的第一次提问显式开启新网页会话。
- 后续 turn 继续使用 Gemini persistent 网页会话。
- 旧本地会话在 v1 中只读，避免问题被发送到错误网页上下文。

### 4.4 重试

浏览器写操作不是天然幂等：

- v1 不自动重试 Gemini ask；task 执行后只有子进程明确未启动才标记确定失败。
- `[NO RESPONSE]`、超时、进程中断、输出超限或结果未知均标记 `unknown_outcome`。
- `unknown_outcome` 立即归档 active conversation 并隔离 Gemini，暂停所有 OpenCLI operation。
- 用户在可见 Chrome 确认已空闲后解除隔离，再创建新 conversation。
- 重启时 pending → canceled、running → unknown、active → archived。

状态机、恢复规则和 API 见 [`docs/domain-api.md`](docs/domain-api.md)。

## 5. 数据与组件

首版只保留三个业务 collection：

| Collection | 职责 |
|---|---|
| `conversations` | 本地会话及 active/archived 状态 |
| `turns` | 一次用户提交及其 Gemini 任务分组 |
| `tasks` | Gemini 任务、手动重试关系、完整结果、错误和耗时 |

`tasks.result` 是回答的唯一事实来源；首版不再增加会双写内容的 `messages` 表。

组件职责：

- React：提交 turn、轮询 Gemini 状态、展示回答和只读历史。
- Go/PocketBase：业务 API、持久化、队列、OpenCLI 子进程和错误归一化。
- OpenCLI/Chrome：网页操作和真实登录会话。

详细模型和 REST API 见 [`docs/domain-api.md`](docs/domain-api.md)。

## 6. 安全与运维

平台能操作用户的真实 Gemini 账号，安全入口是 Stage 1 验收项：

- 应用始终启用 Basic Auth；另用 Tailscale/VPN 或 HTTPS 反向代理保护传输。
- 不公开 PocketBase 管理端和 collection 写接口。
- daemon 始终仅监听 loopback。
- 不记录 cookie、profile 内容或未脱敏 stderr。
- Backend、Bridge、Gemini auth 分层健康检查。
- 锁定 OpenCLI 与 Extension，并作为同一升级单元验证。

宿主机要求、健康检查、备份和升级流程见
[`docs/deployment-operations.md`](docs/deployment-operations.md)。

## 7. 实施顺序

1. **Stage 0**：在目标宿主机验证 Gemini 命令、JSON、模型、串行和多轮上下文；超时/kill 用 fake OpenCLI 验证。
2. **Stage 1**：实现 Gemini 最小产品、当前会话连续追问和只读历史。
3. **Stage 2**：补齐恢复、背压、取消、人工重试和运维。
4. **Stage 3**：实现 Gemini 旧会话恢复与续聊。
5. **Stage 4**：按需增加其他 provider、SSE、图片、附件和历史导入。

验收清单和风险表见 [`docs/roadmap.md`](docs/roadmap.md)。

## 8. 已确认决策

| 决策 | 结论 |
|---|---|
| OpenCLI 部署 | 宿主机安装，使用用户可见 Chrome |
| Go 接入 | 同宿主机 `exec.CommandContext` |
| Provider 范围 | 第一版只做 Gemini |
| 开工顺序 | 先完成 Gemini OpenCLI v1.8.7 合同 PoC |
| 会话范围 | 当前 active conversation 连续追问，旧会话只读 |
| 并发 | Gemini 全操作 FIFO 串行 |
| 任务结果 | `tasks.result` 为唯一事实来源 |
| 超时重试 | 不确定结果归档会话并隔离 Gemini，不自动重发 |
| 前端进度 | 先轮询 turn，SSE 后置 |
| 模型列表 | 以 OpenCLI 实际命令为准 |
| 安全 | Stage 1 默认 HTTPS/VPN + Basic Auth |

## 9. 文档索引

| 文档 | 内容 |
|---|---|
| [`docs/opencli-contract.md`](docs/opencli-contract.md) | Gemini v1 命令、输出和错误合同 |
| [`docs/research/future-providers.md`](docs/research/future-providers.md) | 其他 provider 的后续调研快照 |
| [`docs/domain-api.md`](docs/domain-api.md) | 组件、数据模型、状态机、队列和 REST API |
| [`docs/deployment-operations.md`](docs/deployment-operations.md) | 宿主机部署、安全、健康检查、备份和升级 |
| [`docs/roadmap.md`](docs/roadmap.md) | Stage 0–4、验收标准和风险 |
| [`prompts/implement-gemini-v1.md`](prompts/implement-gemini-v1.md) | 完成 Gemini 第一版的实施与测试提示词 |

## 参考

- OpenCLI v1.8.7：https://github.com/jackwener/OpenCLI/releases/tag/v1.8.7
- Browser Bridge：https://github.com/jackwener/opencli/blob/v1.8.7/docs/guide/browser-bridge.md
- Remote Orchestration：https://github.com/jackwener/opencli/blob/v1.8.7/docs/guide/remote-orchestration.md
- Exit Codes：https://github.com/jackwener/opencli/blob/v1.8.7/docs/guide/exit-codes.md
