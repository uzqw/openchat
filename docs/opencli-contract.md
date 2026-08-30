# OpenCLI v1.8.7 运行合同

> 记录平台依赖的命令、输出、错误和已知阻塞项。目标网站变化后需重新执行 Stage 0 PoC。

## 1. OpenCLI 已核对的运行合同

本节基于 v1.8.7 npm 包中的 `cli-manifest.json` 和适配器源码。Stage 0 必须在目标机器上再次实测，源码存在不等于目标网站当前仍可用。

### 1.1 Browser Bridge

```text
opencli CLI
    │ localhost:19825（私有、无业务鉴权）
    ▼
OpenCLI daemon ↔ Browser Bridge Extension ↔ 可见 Chrome ↔ provider 网站
```

硬约束：

- daemon 固定服务于本机 Bridge，不暴露到 LAN 或公网。
- Chrome 必须安装并启用 Browser Bridge Extension。
- 登录依赖用户在可见 Chrome 中人工完成。
- `opencli <provider> login` 默认打开 foreground 窗口并等待登录完成。
- 平台调用 OpenCLI 时统一显式指定 `--format json`。

### 1.2 v1.8.7 Provider 能力矩阵

| Provider | 提问 | 新会话 | 模型能力 | 登录/状态 | v1.8.7 注意事项 |
|---|---|---|---|---|---|
| ChatGPT | `chatgpt ask` | `ask --new` / `chatgpt new` | 独立命令 `chatgpt model <name>`，`ask` 没有 `--model` | `login/status/whoami` | `ask` 可返回 `conversationId`、`conversationUrl`、`response` |
| Gemini | `gemini ask` | `ask --new true` / `gemini new` | `ask --model`，可用 `gemini models` 发现 | `login/status/whoami` | `ask` 默认 plain，平台必须覆盖为 JSON；模型值必须是规范 ID |
| Kimi | `kimi ask` | `kimi new` | `kimi model --list/--set` | `login/status/whoami` | **当前 `ask` 只返回最多 300 字的 `ReplyPreview`，完整回答提取待 PoC 解决** |
| DeepSeek | `deepseek ask` | `ask --new` / `deepseek new` | `ask --model instant|expert|vision` | `login/status/whoami` | 现有会话中不能切模型，切模型时需新会话 |
| Grok | `grok ask` | `ask --new` / `grok new` | `ask` 无模型参数 | `login/status/whoami` | 只能沿用网站当前能力 |

不得在平台内再维护一份臆测的静态模型表：

- 优先使用 provider 自身的发现/选择命令。
- 无发现能力时，UI 只显示“沿用网站当前模型”。
- OpenCLI 返回的明确 choices 可以作为该锁定版本的合同。
- 发现失败时禁用模型切换，不回退到可能过期的自建列表。

### 1.3 输出与错误

所有业务调用使用参数数组，不经过 shell：

```text
opencli <provider> <command> ... --format json
```

错误分类优先使用进程退出码：

| 退出码 | 含义 | 平台行为 |
|---|---|---|
| `0` | 成功 | 解析 JSON |
| `2` | 参数错误 | 标记失败，不重试 |
| `66` | 空结果 | 标记失败或空结果，不重试 |
| `69` | Browser Bridge 不可用 | 未发送，可短暂重试或提示启动 Chrome |
| `75` | 临时失败、超时或 session busy | 必须结合执行阶段分类；不能盲目重发 |
| `77` | 需要登录 | `auth_required` |
| `78` | 配置错误 | 标记失败，不重试 |
| `130` | 被中断 | 若发送结果未知，标记 `unknown_outcome` |

stderr 只用于诊断和补充分类：

- 设置大小上限。
- 服务端保存脱敏摘要。
- 默认不把原始 stderr 返回前端。

### 1.4 Kimi 阻塞项

v1.8.7 的 `kimi ask` 只返回 300 字预览，不能直接满足完整回复展示。Stage 0 按顺序验证：

1. `kimi ask` 完成后调用 `kimi read --format json`。
2. 或调用 `kimi copy-message --format json` 获取末条 assistant 文本。
3. 若仍不完整，才评估 OpenCLI 官方的 adapter eject/本地 override。
4. 未解决前，Kimi 在产品中标记为 unavailable，不用截断内容冒充完整回答。
