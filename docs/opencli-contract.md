# Gemini OpenCLI v1.8.7 运行合同

> 第一版只实现 Gemini。其他 provider 的历史调研移至 [`research/future-providers.md`](research/future-providers.md)。目标网站或 OpenCLI 版本变化后必须重新执行合同 PoC。

## 1. 运行边界

本合同基于 v1.8.7 npm 包中的 `cli-manifest.json` 和 Gemini adapter 源码；源码存在不等于目标网站当前仍可用，Stage 0 必须在目标宿主机实测。

```text
opencli CLI
    │ localhost:19825（私有、无业务鉴权）
    ▼
OpenCLI daemon ↔ Browser Bridge Extension ↔ 可见 Chrome ↔ Gemini Web
```

硬约束：

- daemon 不暴露到 LAN 或公网。
- 使用与 OpenCLI v1.8.7 匹配的 Extension v1.0.23。
- 使用专用 OS 服务账号/HOME；`~/.opencli/clis/gemini` 必须不存在，且不允许安装 OpenCLI plugin，防止本地代码覆盖内置 Gemini adapter。
- 子进程使用最小环境并移除 `NODE_OPTIONS/NODE_PATH`。
- 配置专用 `OPENCLI_PROFILE`，每个命令显式指定；应用独占该 profile 的 OpenCLI Adapter tab。
- 登录由用户在可见 Chrome 中人工完成。
- `gemini login` 默认使用 foreground 窗口并等待登录完成。
- `gemini whoami` 和 `login` 会导航 shared tab；`models` 会操作当前页面。active conversation 有成功 turn 后，只允许后续 ask，禁止这些维护命令和 doctor。
- 业务输出统一显式请求 `--format json`。
- 启动和健康探针校验 OpenCLI 与 Extension 版本；不匹配时禁止 Gemini 写操作。

## 2. Gemini 命令

| 能力 | 命令 | 说明 |
|---|---|---|
| 提问 | `gemini ask <prompt>` | persistent site session，写操作 |
| 新会话 | `gemini ask <prompt> --new true` / `gemini new` | 每个本地新会话的首轮显式使用 |
| 模型发现 | `gemini models` | 返回模型；v1.8.7 的 `thinkingValues` 为空，不能当能力矩阵 |
| 模型选择 | `gemini ask ... --model <canonical-id>` | 必须是形如 `2.5-flash` 的规范 ID |
| 思考级别 | `gemini ask ... --thinking standard|extended` | adapter 在发送前尝试操作当前 UI；不传表示保持网站值 |
| 登录 | `gemini login` | foreground 人工操作，异步入队 |
| 状态 | `gemini status` / `gemini whoami` | 通过同一 Gemini operation queue 串行执行 |

平台不得维护臆测静态模型表。模型发现失败时禁用模型选择。v1.8.7 不提供可靠的 per-model thinking 能力，UI 只能提供“不改变”、`standard`、`extended`，不能显示 fake 能力支持；实际选择失败按 ask 错误规则处理。

## 3. 输出合同

所有调用使用参数数组，不经过 shell。具体 profile 全局参数位置以目标机器 `opencli --help` 为准，Stage 0 固化为测试 fixture。

Gemini `ask` 成功 JSON 应包含完整 `response`。已知特殊行为：adapter 内部等待超时时可能退出码仍为 0，但 response 是：

```text
💬 [NO RESPONSE] No Gemini response within ...
```

该 sentinel **不是成功结果**。只对该已知前缀做精确识别，避免把正常正文中的 `[NO RESPONSE]` 误判；命中后必须映射为 `unknown_outcome`，归档 active conversation 并隔离 Gemini。

仅在确认 `💬 ` 是 OpenCLI 固定展示包装时移除该前缀；不得修改 Gemini Markdown 正文。

stdout/stderr 使用 pipe 流式有限捕获：

- 超过上限立即终止子进程。
- Gemini ask 输出超限映射为 `unknown_outcome`，不能截断后标记成功。
- 原始 stderr 只用于受限诊断，不返回浏览器，不记录 cookie、profile 内容或敏感路径。

## 4. 错误合同

OpenCLI 退出码：

| 退出码 | 上游含义 |
|---|---|
| `0` | 命令正常结束；仍需排除 `[NO RESPONSE]` sentinel |
| `2` | 参数错误 |
| `66` | 空结果 |
| `69` | Browser Bridge 不可用 |
| `75` | 临时失败、超时或 session busy |
| `77` | 需要登录 |
| `78` | 配置错误 |
| `130` | 被中断 |

Gemini ask 不是幂等写操作，v1 不自动重试：

- `77` 映射为 `auth_required`。
- 后端请求校验在创建 task 前完成；执行中的 `failed` 只允许 OS 明确报告子进程未启动等本地证据。
- 子进程一旦启动，除合同明确的 `77 → auth_required` 外，未成功 ask（包括 `2/66/69/75/78/130`、无效 JSON、输出超限和进程 kill）均保守映射为 `unknown_outcome`。
- 退出码或 stderr 文本本身不足以证明 prompt 未发送。
- `unknown_outcome` 后暂停所有 OpenCLI operation（包括只读刷新和登录），直到用户在可见 Chrome 中确认已空闲并显式解除隔离。

非 ask 的只读命令可按退出码明确失败，但仍不得绕过 operation queue。
