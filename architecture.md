# 统一 AI 聚合平台 — 架构总览

> 版本：v0.2
> OpenCLI 基线：`@jackwener/opencli` **v1.8.7**、Browser Bridge Extension **v1.0.23**
> 状态：先完成 Stage 0 合同 PoC，再进入完整实现。

## 1. 目标

平台通过用户已经登录的真实 Chrome，会聚 ChatGPT、Gemini、Kimi、DeepSeek、Grok 五家网页版能力：

1. 提供一个受保护的 LAN Web UI。
2. 将同一问题并发发送给多个 provider，并排展示结果。
3. 在当前活跃会话中连续追问。
4. 保存本地提问、完整结果、耗时和失败状态。
5. 展示 Bridge、provider 登录和模型能力状态。

所有网站读写都经过 OpenCLI；平台不直接调用五家云 API，也不直接操作网站 DOM。

## 2. v1 边界

- 单机、单用户、单后端实例。
- 当前只有一个 active conversation。
- 当前活跃会话支持连续追问；旧会话只读。
- 浏览器或服务重启后，无法证明上下文正确的会话降级为只读。
- 使用任务轮询，不做 token 级流式输出。
- 不导入网站历史，不支持旧会话续聊、图片或附件。
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
  - conversation / turn / provider task
  - 每 provider operation queue
          │ exec.CommandContext
          ▼
宿主机 @jackwener/opencli v1.8.7
          │ localhost:19825
          ▼
OpenCLI daemon ↔ Extension v1.0.23 ↔ 可见 Chrome
          ▼
ChatGPT / Gemini / Kimi / DeepSeek / Grok
```

采用宿主机方案的原因：

- Go 可以真实执行同一宿主机上的 `opencli` 子进程。
- 用户能在可见 Chrome 中完成人工登录。
- 复用正常 Chrome profile，无需维护容器显示服务器、扩展和 noVNC。
- daemon 保持 loopback，不新增无鉴权远程控制面。

远程 Node Runner 仅作为 v1 之后的备选方案。

## 4. 核心运行规则

### 4.1 OpenCLI 合同

- 所有调用固定 `--format json`，并使用参数数组，禁止 `sh -c`。
- 优先按 OpenCLI 退出码分类错误，stderr 只作脱敏诊断。
- 模型能力以锁定版本的实际命令为准，不维护臆测静态表。
- Kimi v1.8.7 的 `ask` 只返回最多 300 字预览；Stage 0 必须验证完整回答提取，否则禁用 Kimi。

完整命令矩阵和错误合同见 [`docs/opencli-contract.md`](docs/opencli-contract.md)。

### 4.2 并发

- 不同 provider 并行。
- 同 provider 的 `ask/new/model/login/status` 全部经过一个 FIFO operation queue。
- 每 provider 同时只运行一个 operation。
- 队列长度有限；满时返回 `429`，不无限积压。

### 4.3 会话

- 新建本地会话时归档旧 active conversation。
- provider 在新本地会话中的第一次提问显式开启新网页会话。
- 后续 turn 继续使用该 provider 的 persistent 网页会话。
- 旧本地会话在 v1 中只读，避免问题被发送到错误网页上下文。

### 4.4 重试

浏览器写操作不是天然幂等：

- 只有能够确定“尚未提交”的错误可以自动重试。
- 超时、进程中断或 daemon 返回结果未知时标记 `unknown_outcome`。
- `unknown_outcome` 禁止自动重发，由用户确认后手动重试。
- 重启时重新入队 `pending`，遗留 `running` 改为 `unknown_outcome`。

状态机、恢复规则和 API 见 [`docs/domain-api.md`](docs/domain-api.md)。

## 5. 数据与组件

首版只保留三个业务 collection：

| Collection | 职责 |
|---|---|
| `conversations` | 本地会话及 active/archived 状态 |
| `turns` | 一次用户提交，是多 provider 结果的分组 |
| `provider_tasks` | 每个 provider 的状态、完整结果、错误和耗时 |

`provider_tasks.result` 是回答的唯一事实来源；首版不再增加会双写内容的 `messages` 表。

组件职责：

- React：提交 turn、轮询整轮状态、并排展示、只读历史。
- Go/PocketBase：业务 API、持久化、队列、OpenCLI 子进程和错误归一化。
- OpenCLI/Chrome：网页操作和真实登录会话。

详细模型和 REST API 见 [`docs/domain-api.md`](docs/domain-api.md)。

## 6. 安全与运维

平台能操作五个真实账号，安全入口是 Stage 1 验收项：

- 使用 Tailscale/VPN，或 HTTPS 反向代理 + Basic Auth。
- 不公开 PocketBase 管理端和 collection 写接口。
- daemon 始终仅监听 loopback。
- 不记录 cookie、profile 内容或未脱敏 stderr。
- Backend、Bridge、provider auth 分层健康检查。
- 锁定 OpenCLI 与 Extension，并作为同一升级单元验证。

宿主机要求、健康检查、备份和升级流程见
[`docs/deployment-operations.md`](docs/deployment-operations.md)。

## 7. 实施顺序

1. **Stage 0**：在目标宿主机验证五家命令、JSON、模型、并发、超时和多轮上下文。
2. **Stage 1**：实现最小产品、当前会话连续追问和只读历史。
3. **Stage 2**：补齐恢复、背压、取消、人工重试和运维。
4. **Stage 3**：逐家实现旧会话恢复与续聊。
5. **Stage 4**：按需增加 SSE、图片、附件和历史导入。

验收清单和风险表见 [`docs/roadmap.md`](docs/roadmap.md)。

## 8. 已确认决策

| 决策 | 结论 |
|---|---|
| OpenCLI 部署 | 宿主机安装，使用用户可见 Chrome |
| Go 接入 | 同宿主机 `exec.CommandContext` |
| 开工顺序 | 先完成五家 OpenCLI v1.8.7 合同 PoC |
| 会话范围 | 当前 active conversation 连续追问，旧会话只读 |
| 并发 | 跨 provider 并行，同 provider 全操作串行 |
| 任务结果 | `provider_tasks.result` 为唯一事实来源 |
| 超时重试 | 不确定结果不自动重发 |
| 前端进度 | 先轮询 turn，SSE 后置 |
| 模型列表 | 以 OpenCLI 实际命令为准 |
| 安全 | Stage 1 默认 HTTPS/VPN + Basic Auth |

## 9. 文档索引

| 文档 | 内容 |
|---|---|
| [`docs/opencli-contract.md`](docs/opencli-contract.md) | Provider 命令、输出、退出码、Kimi 阻塞项 |
| [`docs/domain-api.md`](docs/domain-api.md) | 组件、数据模型、状态机、队列和 REST API |
| [`docs/deployment-operations.md`](docs/deployment-operations.md) | 宿主机部署、安全、健康检查、备份和升级 |
| [`docs/roadmap.md`](docs/roadmap.md) | Stage 0–4、验收标准和风险 |

## 参考

- OpenCLI v1.8.7：https://github.com/jackwener/OpenCLI/releases/tag/v1.8.7
- Browser Bridge：https://github.com/jackwener/opencli/blob/v1.8.7/docs/guide/browser-bridge.md
- Remote Orchestration：https://github.com/jackwener/opencli/blob/v1.8.7/docs/guide/remote-orchestration.md
- Exit Codes：https://github.com/jackwener/opencli/blob/v1.8.7/docs/guide/exit-codes.md
