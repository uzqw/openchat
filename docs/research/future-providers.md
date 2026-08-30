# 后续 Provider 调研记录

> 本文不属于 Gemini-only v1 实现范围。接入第二个 provider 时先重新核对目标 OpenCLI 版本和网站现状，再提取最小通用抽象。

## v1.8.7 能力快照

| Provider | 提问 | 新会话 | 模型能力 | 登录/状态 | 注意事项 |
|---|---|---|---|---|---|
| ChatGPT | `chatgpt ask` | `ask --new` / `chatgpt new` | 独立命令 `chatgpt model <name>`，`ask` 没有 `--model` | `login/status/whoami` | `ask` 可返回 `conversationId`、`conversationUrl`、`response` |
| Kimi | `kimi ask` | `kimi new` | `kimi model --list/--set` | `login/status/whoami` | `ask` 只返回最多 300 字的 `ReplyPreview` |
| DeepSeek | `deepseek ask` | `ask --new` / `deepseek new` | `ask --model instant|expert|vision` | `login/status/whoami` | 现有会话中不能切模型 |
| Grok | `grok ask` | `ask --new` / `grok new` | `ask` 无模型参数 | `login/status/whoami` | 只能沿用网站当前能力 |

## Kimi 完整回答阻塞项

未来接入 Kimi 时按顺序验证：

1. `kimi ask` 完成后调用 `kimi read --format json`。
2. 或调用 `kimi copy-message --format json` 获取末条 assistant 文本。
3. 若仍不完整，才评估 OpenCLI 官方的 adapter eject/本地 override。
4. 未解决前不接入 Kimi，不用截断内容冒充完整回答。
