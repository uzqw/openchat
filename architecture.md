# 统一 AI 聚合平台 — 架构总览

> 版本：v1.1（Gemini + Grok 双站点并存，会话级站点）
> OpenCLI 基线：`@jackwener/opencli` **v1.8.7**、Browser Bridge Extension **v1.0.23**

## 1. 目标

平台最终目标是通过用户已经登录的真实 Chrome，会聚多个 AI 网页版；**当前版本同时支持 gemini 与 grok 两个站点并存**：每个会话在创建时选定站点并把 `provider` 字段落库，恢复与追问都按会话自己的站点执行；`OPENCLI_SITE` 只决定新建会话的默认站点，不再是全局切换开关：

1. 提供一个受保护的 LAN Web UI。
2. 在当前活跃站点会话中连续追问。
3. 保存本地提问、完整结果、耗时和失败状态。
4. 展示 Bridge、站点登录和模型能力状态。

所有站点读写都经过 OpenCLI；平台不直接调用站点云 API，也不直接操作网站 DOM。

## 2. v1 边界

- 单机、单用户、单后端实例。
- 双站点单 provider 并存：gemini / grok **同时**可用（会话级选择，`OPENCLI_SITE` 仅作新会话默认值）；ChatGPT、Kimi、DeepSeek 后置。
- 站点差异收敛在 `internal/opencli/site.go` 的 Site 能力表（子命令名、--model/--thinking、models 命令、sentinel、会话 URL 形状）；新增站点 = 新增 Site 条目 + 合同实测，不在各层散落分支。
- 当前只有一个 active conversation（新建/恢复会归档旧的）。
- 当前活跃会话支持连续追问；旧会话可恢复续聊（每个会话保存其站点远端会话 id，恢复时按会话自己的站点 `detail` 导航后继续）。
- 无 `remote_id` 的旧会话保持只读（grok 为 UUID，`https://grok.com/c/<id>`）。
- 使用任务轮询，不做 token 级流式输出。
- 不导入网站历史，不支持图片或附件。
- 使用专用 OS 服务账号/HOME 和 `OPENCLI_PROFILE`，应用独占其 OpenCLI Adapter tab。
- 不把无鉴权的 OpenCLI daemon 19825 端口暴露到 LAN。

## 3. 部署方案

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

- 所有 OpenCLI 业务调用显式 `--format json`、使用参数数组，禁止 `sh -c`；错误按退出码分类，stderr 只作脱敏诊断。
- Gemini 全操作（ask/login/refresh/doctor/status/whoami/models）经一个 FIFO operation queue 串行；ask 有容量上限，满则 `429`。
- active conversation 有成功 turn 后只允许后续 ask，禁止 whoami/login/models/doctor 改动 shared tab。
- 新建本地会话归档旧 active；首轮 `--new true`，成功后捕获远端会话 id；恢复会话先 `gemini detail` 导航并校验 URL 再提问。
- 浏览器写操作不幂等：v1 不自动重试 ask；不确定结果标记 `unknown_outcome`、归档会话并隔离 Gemini，人工确认后解除。

详细状态机、恢复规则和 REST API 见 [`docs/domain-api.md`](docs/domain-api.md)。

## 5. 数据与组件

首版只保留三个业务 collection：`conversations` / `turns` / `tasks`；`tasks.result` 是回答的唯一事实来源。

组件职责：

- React：提交 turn、轮询 Gemini 状态、展示回答和只读历史。
- Go/PocketBase：业务 API、持久化、队列、OpenCLI 子进程和错误归一化。
- OpenCLI/Chrome：网页操作和真实登录会话。

详细模型和 REST API 见 [`docs/domain-api.md`](docs/domain-api.md)。

## 6. 安全与运维

- 应用始终启用 Basic Auth；另用 Tailscale/VPN 或 HTTPS 反向代理保护传输。
- 不公开 PocketBase 管理端和 collection 写接口。
- daemon 始终仅监听 loopback。
- 不记录 cookie、profile 内容或未脱敏 stderr。
- Backend、Bridge、Gemini auth 分层健康检查。
- 锁定 OpenCLI 与 Extension，并作为同一升级单元验证。

宿主机要求、健康检查、备份和升级流程见
[`docs/deployment-operations.md`](docs/deployment-operations.md)。

## 7. 实施状态

Stage 0–3 已完成（合同 PoC、最小产品、恢复与运维、旧会话续聊）；Stage 4（其他 provider、SSE、图片、附件、历史导入）按需进行。详见 [`docs/roadmap.md`](docs/roadmap.md)。

## 8. 已确认决策

| 决策 | 结论 |
|---|---|
| OpenCLI 部署 | 宿主机安装，使用用户可见 Chrome |
| Go 接入 | 同宿主机 `exec.CommandContext` |
| Provider 范围 | 第一版 Gemini；v1.1 起 gemini/grok 双站点并存（会话级），`OPENCLI_SITE` 仅作新会话默认站点 |
| 会话范围 | 当前 active conversation 连续追问；旧会话有 `remote_id` 可恢复续聊，无则只读 |
| 并发 | Gemini 全操作 FIFO 串行 |
| 任务结果 | `tasks.result` 为唯一事实来源 |
| 超时重试 | 不确定结果归档会话并隔离 Gemini，不自动重发 |
| 前端进度 | 先轮询 turn，SSE 后置 |
| 模型列表 | 以 OpenCLI 实际命令为准 |
| 安全 | 默认 HTTPS/VPN + Basic Auth |

## 9. 文档索引

| 文档 | 内容 |
|---|---|
| [`docs/opencli-contract.md`](docs/opencli-contract.md) | Gemini/Grok v1.8.7 命令、输出和错误合同 |
| [`docs/domain-api.md`](docs/domain-api.md) | 组件、数据模型、状态机、队列和 REST API |
| [`docs/deployment-operations.md`](docs/deployment-operations.md) | 宿主机部署、安全、健康检查、备份和升级 |
| [`docs/roadmap.md`](docs/roadmap.md) | Stage 0–4、验收标准和风险 |

## 参考

- OpenCLI v1.8.7：https://github.com/jackwener/OpenCLI/releases/tag/v1.8.7
- Browser Bridge：https://github.com/jackwener/opencli/blob/v1.8.7/docs/guide/browser-bridge.md
- Remote Orchestration：https://github.com/jackwener/opencli/blob/v1.8.7/docs/guide/remote-orchestration.md
- Exit Codes：https://github.com/jackwener/opencli/blob/v1.8.7/docs/guide/exit-codes.md
